//go:build integration

package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	postgres "sweetshop/internal/repository/postgres"
)

// These tests stand in for the first B1 mutation with the demo fixture:
// restocking a product of the isolated test database is the business change,
// and it must commit exactly when its audit row does.

// auditStaff inserts an enabled staff account directly; the audit path never
// verifies a password, so no bcrypt work is needed.
func (d *database) auditStaff(t *testing.T, address string) domain.StaffUser {
	t.Helper()
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		t.Fatal(err)
	}
	staff := domain.StaffUser{ID: "stf_" + hex.EncodeToString(id), Email: address, DisplayName: "Ops"}
	d.exec(t, "INSERT INTO staff_users(id,email,display_name,password_hash) VALUES ($1,$2,$3,'unused')", staff.ID, staff.Email, staff.DisplayName)
	return staff
}

func mutationFor(t *testing.T, staff domain.StaffUser, requestID string) domain.StaffMutation {
	t.Helper()
	mutation, err := application.NewStaffAuth(nil).AuthorizeMutation(staff, "product.update", requestID)
	if err != nil {
		t.Fatal(err)
	}
	return mutation
}

func restock(product string, delta int) func(context.Context, postgres.StaffTx) (domain.AuditTarget, error) {
	return func(ctx context.Context, tx postgres.StaffTx) (domain.AuditTarget, error) {
		tag, err := tx.Exec(ctx, "UPDATE products SET stock=stock+$2 WHERE id=$1", product, delta)
		if err != nil {
			return domain.AuditTarget{}, err
		}
		if tag.RowsAffected() != 1 {
			return domain.AuditTarget{}, domain.ErrNotFound
		}
		return domain.AuditTarget{Type: "product", ID: product}, nil
	}
}

func (d *database) stock(t *testing.T, product string) int64 {
	return d.number(t, "SELECT stock FROM products WHERE id=$1", product)
}

func (d *database) auditRows(t *testing.T) int64 {
	return d.number(t, "SELECT count(*) FROM staff_audit_events")
}

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestStaffMutationCommitsWithItsAuditEvent(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	before := d.stock(t, "p01")
	event, err := d.store.InStaffMutation(d.ctx, mutationFor(t, staff, "req-42"), restock("p01", 5))
	if err != nil {
		t.Fatal(err)
	}
	if d.stock(t, "p01") != before+5 || d.auditRows(t) != 1 {
		t.Fatal("mutation and audit event must both exist")
	}
	var stored domain.StaffAuditEvent
	var requestID *string
	var action string
	if err := d.pool.QueryRow(d.ctx, `SELECT id,staff_user_id,action,entity_type,entity_id,request_id,created_at FROM staff_audit_events`).Scan(
		&stored.ID, &stored.StaffUserID, &action, &stored.EntityType, &stored.EntityID, &requestID, &stored.CreatedAt); err != nil {
		t.Fatal(err)
	}
	stored.Action, stored.RequestID, stored.CreatedAt = domain.AuditAction(action), *requestID, stored.CreatedAt.UTC()
	if stored != event || !strings.HasPrefix(event.ID, "aud_") || event.StaffUserID != staff.ID || event.EntityID != "p01" || event.RequestID != "req-42" {
		t.Fatalf("stored %+v, returned %+v", stored, event)
	}
	if time.Since(event.CreatedAt) > time.Minute || time.Until(event.CreatedAt) > time.Minute {
		t.Fatalf("created_at is not the database clock: %v", event.CreatedAt)
	}
	// Outside HTTP there is no request ID; it is stored as NULL, not "".
	if _, err := d.store.InStaffMutation(d.ctx, mutationFor(t, staff, ""), restock("p01", 1)); err != nil {
		t.Fatal(err)
	}
	if d.number(t, "SELECT count(*) FROM staff_audit_events WHERE request_id IS NULL") != 1 {
		t.Fatal("missing request ID not stored as NULL")
	}
}

// A failing commit, here a deferred check installed only in this disposable
// database, rolls back the appended event together with the mutation.
func TestFailedCommitRollsBackTheAuditEvent(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	d.exec(t, `CREATE FUNCTION fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure' USING ERRCODE='P0001'; END $$`)
	d.exec(t, `CREATE CONSTRAINT TRIGGER fail_audit_at_commit AFTER INSERT ON staff_audit_events DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fail_audit()`)
	before := d.stock(t, "p01")
	_, err := d.store.InStaffMutation(d.ctx, mutationFor(t, staff, "req-1"), restock("p01", 5))
	if pgCode(err) != "P0001" || !strings.Contains(err.Error(), "commit staff mutation") {
		t.Fatalf("commit must fail: %v", err)
	}
	if d.stock(t, "p01") != before || d.auditRows(t) != 0 {
		t.Fatal("a failed commit left the mutation or its audit event")
	}
}

// The business statement succeeds and the audit INSERT itself fails: the
// mutation rolls back. The failing trigger exists only in this database.
func TestFailedAuditInsertRollsBackTheMutation(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	d.exec(t, `CREATE FUNCTION fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure' USING ERRCODE='P0001'; END $$`)
	d.exec(t, `CREATE TRIGGER fail_audit_for_p02 BEFORE INSERT ON staff_audit_events FOR EACH ROW WHEN (NEW.entity_id='p02') EXECUTE FUNCTION fail_audit()`)
	p01, p02 := d.stock(t, "p01"), d.stock(t, "p02")
	_, err := d.store.InStaffMutation(d.ctx, mutationFor(t, staff, ""), restock("p02", 7))
	if pgCode(err) != "P0001" || !strings.Contains(err.Error(), "append staff audit event") {
		t.Fatalf("audit insert must fail: %v", err)
	}
	if d.stock(t, "p02") != p02 || d.auditRows(t) != 0 {
		t.Fatal("business change committed without its audit event")
	}
	// The same path commits when the insert succeeds.
	if _, err := d.store.InStaffMutation(d.ctx, mutationFor(t, staff, ""), restock("p01", 7)); err != nil || d.stock(t, "p01") != p01+7 || d.auditRows(t) != 1 {
		t.Fatalf("control mutation: %v", err)
	}
}

func TestFailedMutationLeavesNoAuditEvent(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	before := d.stock(t, "p01")
	injected := errors.New("injected business failure")
	for name, work := range map[string]func(context.Context, postgres.StaffTx) (domain.AuditTarget, error){
		"error after a write": func(ctx context.Context, tx postgres.StaffTx) (domain.AuditTarget, error) {
			if _, err := restock("p01", 3)(ctx, tx); err != nil {
				return domain.AuditTarget{}, err
			}
			return domain.AuditTarget{}, injected
		},
		"constraint violation": restock("p01", -1_000_000),
		"missing resource":     restock("missing", 1),
		// A swallowed SQL error leaves the transaction aborted, so the audit
		// insert fails and nothing commits.
		"swallowed SQL error": func(ctx context.Context, tx postgres.StaffTx) (domain.AuditTarget, error) {
			if _, err := restock("p01", 3)(ctx, tx); err != nil {
				return domain.AuditTarget{}, err
			}
			_, _ = tx.Exec(ctx, "UPDATE products SET stock=-1 WHERE id='p01'")
			return domain.AuditTarget{Type: "product", ID: "p01"}, nil
		},
		"invalid target": func(ctx context.Context, tx postgres.StaffTx) (domain.AuditTarget, error) {
			if _, err := restock("p01", 3)(ctx, tx); err != nil {
				return domain.AuditTarget{}, err
			}
			return domain.AuditTarget{Type: "Product", ID: "p01"}, nil
		},
		"panic": func(ctx context.Context, tx postgres.StaffTx) (domain.AuditTarget, error) {
			if _, err := restock("p01", 3)(ctx, tx); err != nil {
				return domain.AuditTarget{}, err
			}
			panic("injected panic")
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := func() (event domain.StaffAuditEvent, err error) {
				defer func() {
					if recovered := recover(); recovered != nil {
						err = errors.New("panicked")
					}
				}()
				return d.store.InStaffMutation(d.ctx, mutationFor(t, staff, ""), work)
			}()
			if err == nil {
				t.Fatal("mutation succeeded")
			}
			if name == "error after a write" && !errors.Is(err, injected) {
				t.Fatalf("work error not returned: %v", err)
			}
			if d.stock(t, "p01") != before || d.auditRows(t) != 0 {
				t.Fatal("failed mutation left state or an audit event")
			}
		})
	}
	if d.number(t, "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state LIKE 'idle in transaction%'") != 0 {
		t.Fatal("a failed mutation left its transaction open")
	}
}

func TestConcurrentStaffMutationsKeepEveryEvent(t *testing.T) {
	d := newDatabase(t, true)
	first, second := d.auditStaff(t, "ops@example.com"), d.auditStaff(t, "ops2@example.com")
	before := d.stock(t, "p01")
	const n = 16
	ids := make([]string, n)
	errs := make([]error, n)
	mutations := make([]domain.StaffMutation, n)
	for i := range mutations {
		staff := first
		if i%2 == 1 {
			staff = second
		}
		mutations[i] = mutationFor(t, staff, "req-"+string(rune('a'+i)))
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			event, err := d.store.InStaffMutation(d.ctx, mutations[i], restock("p01", 1))
			ids[i], errs[i] = event.ID, err
		}(i)
	}
	close(start)
	wg.Wait()
	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil || seen[ids[i]] {
			t.Fatalf("mutation %d: %q %v", i, ids[i], err)
		}
		seen[ids[i]] = true
	}
	if d.stock(t, "p01") != before+n || d.auditRows(t) != n || d.number(t, "SELECT count(DISTINCT id) FROM staff_audit_events") != n ||
		d.number(t, "SELECT count(*) FROM staff_audit_events WHERE staff_user_id=$1", first.ID) != n/2 {
		t.Fatal("lost mutation or audit event")
	}
}

func TestOnlyActiveStaffCanBeAuditActors(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	before := d.stock(t, "p01")
	unknown := domain.StaffUser{ID: "stf_" + strings.Repeat("0", 32)}
	if _, err := d.store.InStaffMutation(d.ctx, mutationFor(t, unknown, ""), restock("p01", 1)); !errors.Is(err, domain.ErrAuditActorInactive) || !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("unknown actor: %v", err)
	}
	if _, err := d.store.InStaffMutation(d.ctx, domain.StaffMutation{}, restock("p01", 1)); err == nil {
		t.Fatal("unauthorized zero mutation ran")
	}
	if _, err := d.store.SetStaffDisabled(d.ctx, staff.Email, true); err != nil {
		t.Fatal(err)
	}
	if _, err := d.store.InStaffMutation(d.ctx, mutationFor(t, staff, ""), restock("p01", 1)); !errors.Is(err, domain.ErrAuditActorInactive) {
		t.Fatalf("disabled actor: %v", err)
	}
	if d.stock(t, "p01") != before || d.auditRows(t) != 0 {
		t.Fatal("an inactive actor changed state")
	}
	// The table itself refuses actors that are not staff, customers included.
	for _, actor := range []string{"usr-demo", unknown.ID} {
		_, err := d.pool.Exec(d.ctx, `INSERT INTO staff_audit_events(id,staff_user_id,action,entity_type,entity_id) VALUES ($1,$2,'product.update','product','p01')`, "aud_"+strings.Repeat("1", 32), actor)
		if pgCode(err) != "23503" {
			t.Fatalf("actor %q: %v", actor, err)
		}
	}
	for name, values := range map[string][]any{
		"id format":      {"aud_1", staff.ID, "product.update", "product", "p01", nil},
		"action format":  {"aud_" + strings.Repeat("2", 32), staff.ID, "Product Update", "product", "p01", nil},
		"entity type":    {"aud_" + strings.Repeat("3", 32), staff.ID, "product.update", "", "p01", nil},
		"entity id":      {"aud_" + strings.Repeat("4", 32), staff.ID, "product.update", "product", strings.Repeat("x", 129), nil},
		"request id":     {"aud_" + strings.Repeat("5", 32), staff.ID, "product.update", "product", "p01", ""},
		"request id set": {"aud_" + strings.Repeat("6", 32), staff.ID, "product.update", "product", "p01", "has space"},
	} {
		_, err := d.pool.Exec(d.ctx, `INSERT INTO staff_audit_events(id,staff_user_id,action,entity_type,entity_id,request_id) VALUES ($1,$2,$3,$4,$5,$6)`, values...)
		if pgCode(err) != "23514" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A disable that starts while a mutation holds the actor row waits for its
// commit; afterwards the actor can no longer mutate.
func TestStaffDisableWaitsForARunningMutation(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	inside, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	mutation := mutationFor(t, staff, "")
	go func() {
		_, err := d.store.InStaffMutation(d.ctx, mutation, func(ctx context.Context, tx postgres.StaffTx) (domain.AuditTarget, error) {
			close(inside)
			<-release
			return restock("p01", 1)(ctx, tx)
		})
		done <- err
	}()
	<-inside
	disabled := make(chan error, 1)
	go func() {
		_, err := d.store.SetStaffDisabled(d.ctx, staff.Email, true)
		disabled <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for d.number(t, "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the disable did not wait for the mutation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("mutation: %v", err)
	}
	if err := <-disabled; err != nil {
		t.Fatalf("disable: %v", err)
	}
	if d.auditRows(t) != 1 {
		t.Fatal("committed mutation lost its audit event")
	}
	if _, err := d.store.InStaffMutation(d.ctx, mutationFor(t, staff, ""), restock("p01", 1)); !errors.Is(err, domain.ErrAuditActorInactive) {
		t.Fatalf("disabled actor mutated: %v", err)
	}
}

func TestStaffAuditRowsAreAppendOnly(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "ops@example.com")
	if _, err := d.store.InStaffMutation(d.ctx, mutationFor(t, staff, "req-1"), restock("p01", 1)); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"UPDATE staff_audit_events SET action='product.delete'",
		"UPDATE staff_audit_events SET created_at=created_at",
		"DELETE FROM staff_audit_events",
		"TRUNCATE staff_audit_events",
	} {
		if _, err := d.pool.Exec(d.ctx, statement); pgCode(err) != "23001" {
			t.Errorf("%s: %v", statement, err)
		}
	}
	// Staff are disabled, never deleted; an actor with history cannot be.
	if _, err := d.pool.Exec(d.ctx, "DELETE FROM staff_users WHERE id=$1", staff.ID); pgCode(err) != "23503" {
		t.Fatalf("actor deletion: %v", err)
	}
	if d.auditRows(t) != 1 {
		t.Fatal("audit row changed")
	}
	// The store offers no way to change or remove audit rows.
	store := reflect.TypeOf(d.store)
	for i := 0; i < store.NumMethod(); i++ {
		name := strings.ToLower(store.Method(i).Name)
		if strings.Contains(name, "audit") && (strings.Contains(name, "update") || strings.Contains(name, "delete") || strings.Contains(name, "remove") || strings.Contains(name, "purge")) {
			t.Errorf("store method %s mutates audit rows", store.Method(i).Name)
		}
	}
}

// The A8 image (migrations 001–011, RUN_MIGRATIONS=false or true) treats 012
// as a newer migration and keeps serving customers and staff.
func TestPreviousBinaryToleratesStaffAuditSchema(t *testing.T) {
	d := productionDatabase(t)
	hash, err := bcrypt.GenerateFromPassword([]byte(staffPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	staffID := "stf_" + strings.Repeat("c", 32)
	d.exec(t, "INSERT INTO staff_users(id,email,display_name,password_hash) VALUES ($1,$2,'Ops',$3)", staffID, staffEmail, string(hash))
	if _, err := d.store.InStaffMutation(d.ctx, mutationFor(t, domain.StaffUser{ID: staffID}, ""), func(context.Context, postgres.StaffTx) (domain.AuditTarget, error) {
		return domain.AuditTarget{Type: "staff_user", ID: staffID}, nil
	}); err != nil {
		t.Fatal(err)
	}
	previous := productionMigrator(migrationDir(t, []string{"001_", "002_", "003_", "004_", "005_", "006_", "007_", "008_", "009_", "010_", "011_"}, nil), &lockedBuffer{})
	if err := previous.Verify(d.ctx, d.pool); err != nil {
		t.Fatalf("A8 schema check on the B0 schema: %v", err)
	}
	if err := previous.Migrate(d.ctx, d.pool); err != nil {
		t.Fatalf("A8 migrate on the B0 schema: %v", err)
	}
	handler := d.staffServer(t, nil)
	d.operatorCustomer(t, customer, customerPassword)
	if cookieLogin(t, handler, customer, customerPassword) == "" {
		t.Fatal("customer login")
	}
	login, ex := loginStaff(t, handler, staffPassword, nil)
	if ex.Code != 200 || login.id != staffID || !staffState(handler, login.token) {
		t.Fatalf("staff login: %d", ex.Code)
	}
	if d.auditRows(t) != 1 {
		t.Fatal("audit history changed")
	}
}
