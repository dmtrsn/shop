package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"sweetshop/internal/domain"
)

func TestCollectionScheduleAppliesToListingAndCatalogFiltering(t *testing.T) {
	store := NewDemo()
	now := time.Now()
	store.collections = []domain.Collection{
		{ID: "open", Slug: "open", ProductIDs: []string{"p01"}, ProductCount: 1},
		{ID: "started", Slug: "started", StartsAt: now.Add(-time.Hour), ProductIDs: []string{"p02"}, ProductCount: 1},
		{ID: "future", Slug: "future", StartsAt: now.Add(time.Hour), ProductIDs: []string{"p03"}, ProductCount: 1},
		{ID: "expired", Slug: "expired", EndsAt: now.Add(-time.Hour), ProductIDs: []string{"p04"}, ProductCount: 1},
	}

	collections, err := store.ListCollections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(collections) != 2 || collections[0].ID != "open" || collections[1].ID != "started" {
		t.Fatalf("visible collections=%+v", collections)
	}

	for _, test := range []struct {
		identity string
		wantID   string
	}{
		{identity: "open", wantID: "p01"},
		{identity: "started", wantID: "p02"},
		{identity: "future"},
		{identity: "expired"},
	} {
		page, err := store.ListProducts(context.Background(), domain.ProductFilter{Collection: test.identity})
		if err != nil {
			t.Fatalf("collection %s: %v", test.identity, err)
		}
		if test.wantID == "" {
			if page.Total != 0 || len(page.Items) != 0 {
				t.Fatalf("inactive collection %s exposed %+v", test.identity, page.Items)
			}
			continue
		}
		if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != test.wantID {
			t.Fatalf("active collection %s page=%+v", test.identity, page)
		}
	}
}

// The storefront shows productCount next to links into /catalog?category=…,
// so both must use the same category semantics.
func TestCategoryProductCountMatchesUnfilteredCatalogTotal(t *testing.T) {
	store := NewDemo()
	ctx := context.Background()
	categories, err := store.ListCategories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(categories) == 0 {
		t.Fatal("demo categories are missing")
	}
	for _, category := range categories {
		for _, identity := range []string{category.ID, category.Slug} {
			page, err := store.ListProducts(ctx, domain.ProductFilter{Category: identity})
			if err != nil {
				t.Fatalf("category %s: %v", identity, err)
			}
			if page.Total != category.ProductCount {
				t.Fatalf("category %s: productCount=%d catalog total=%d", identity, category.ProductCount, page.Total)
			}
		}
	}
}

func sessionExists(t *testing.T, store *Store, hash string, transport domain.SessionTransport, at time.Time) bool {
	t.Helper()
	_, err := store.SessionByTokenHash(context.Background(), hash, transport, at)
	return err == nil
}

func TestCreateSessionIsAtomic(t *testing.T) {
	ctx := context.Background()
	store := NewDemo()
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	create := func(ns newSessionArgs) error {
		return store.CreateSession(ctx, domain.NewSession{UserID: domain.DemoUserID, TokenHash: ns.hash, Transport: ns.transport, ExpiresAt: ns.expires, ReplacesTokenHash: ns.replaces}, ns.now)
	}
	for _, seed := range []newSessionArgs{
		{hash: "old-cookie", transport: domain.SessionCookie, expires: future, now: past.Add(-time.Hour)},
		{hash: "expired", transport: domain.SessionCookie, expires: past, now: past.Add(-time.Hour)},
		{hash: "bearer", transport: domain.SessionBearer, expires: future, now: past.Add(-time.Hour)},
	} {
		if err := create(seed); err != nil {
			t.Fatal(err)
		}
	}
	unchanged := func() {
		t.Helper()
		if !sessionExists(t, store, "old-cookie", domain.SessionCookie, now) || !sessionExists(t, store, "expired", domain.SessionCookie, past.Add(-time.Minute)) || !sessionExists(t, store, "bearer", domain.SessionBearer, now) {
			t.Fatal("failed CreateSession changed existing sessions")
		}
	}
	if err := create(newSessionArgs{hash: "bearer", transport: domain.SessionCookie, expires: future, replaces: "old-cookie", now: now}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate hash: %v", err)
	}
	unchanged()
	if err := store.CreateSession(ctx, domain.NewSession{UserID: "missing", TokenHash: "new", Transport: domain.SessionCookie, ExpiresAt: future, ReplacesTokenHash: "old-cookie"}, now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	unchanged()
	if err := create(newSessionArgs{hash: "new", transport: domain.SessionBearer, expires: future, replaces: "old-cookie", now: now}); err == nil {
		t.Fatal("bearer sessions must not replace sessions")
	}
	unchanged()
	if err := create(newSessionArgs{hash: "new", transport: domain.SessionCookie, expires: future, replaces: "bearer", now: now}); err != nil {
		t.Fatal(err)
	}
	if !sessionExists(t, store, "bearer", domain.SessionBearer, now) {
		t.Fatal("cookie replacement deleted a bearer session")
	}
	if sessionExists(t, store, "expired", domain.SessionCookie, past.Add(-time.Minute)) {
		t.Fatal("expired session was not purged")
	}
	if err := create(newSessionArgs{hash: "newer", transport: domain.SessionCookie, expires: future, replaces: "old-cookie", now: now}); err != nil {
		t.Fatal(err)
	}
	if sessionExists(t, store, "old-cookie", domain.SessionCookie, now) || !sessionExists(t, store, "newer", domain.SessionCookie, now) {
		t.Fatal("replacement did not swap sessions")
	}
	if err := store.DeleteSession(ctx, "newer", domain.SessionBearer); err != nil || !sessionExists(t, store, "newer", domain.SessionCookie, now) {
		t.Fatal("delete must be transport-bound")
	}
	if err := store.DeleteSession(ctx, "newer", domain.SessionCookie); err != nil || sessionExists(t, store, "newer", domain.SessionCookie, now) {
		t.Fatal("delete")
	}
	if err := store.DeleteSession(ctx, "newer", domain.SessionCookie); err != nil {
		t.Fatal("delete must be idempotent")
	}
}

type newSessionArgs struct {
	hash, replaces string
	transport      domain.SessionTransport
	expires, now   time.Time
}

func TestConcurrentSessionReplacement(t *testing.T) {
	ctx := context.Background()
	store := NewDemo()
	now := time.Now()
	if err := store.CreateSession(ctx, domain.NewSession{UserID: domain.DemoUserID, TokenHash: "shared-old", Transport: domain.SessionCookie, ExpiresAt: now.Add(time.Hour)}, now); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- store.CreateSession(ctx, domain.NewSession{UserID: domain.DemoUserID, TokenHash: fmt.Sprintf("new-%d", i), Transport: domain.SessionCookie, ExpiresAt: now.Add(time.Hour), ReplacesTokenHash: "shared-old"}, now)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if sessionExists(t, store, "shared-old", domain.SessionCookie, now) {
		t.Fatal("replaced session survived")
	}
	for i := 0; i < 16; i++ {
		if !sessionExists(t, store, fmt.Sprintf("new-%d", i), domain.SessionCookie, now) {
			t.Fatal("concurrent logins must each create a session")
		}
	}
}
