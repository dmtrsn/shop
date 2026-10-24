package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"sweetshop/internal/domain"
)

type fakeOrderStore struct {
	filter  domain.AdminOrderFilter
	changes []domain.OrderStatus
}

func (f *fakeOrderStore) AdminOrders(_ context.Context, filter domain.AdminOrderFilter) (domain.AdminOrderPage, error) {
	f.filter = filter
	return domain.AdminOrderPage{}, nil
}

func (f *fakeOrderStore) AdminOrder(context.Context, string) (domain.AdminOrder, error) {
	return domain.AdminOrder{}, domain.ErrNotFound
}

func (f *fakeOrderStore) SetOrderStatus(_ context.Context, _ domain.StaffMutation, _ string, _ int64, to domain.OrderStatus) (domain.AdminOrder, error) {
	f.changes = append(f.changes, to)
	return domain.AdminOrder{Status: to}, nil
}

func TestOrderAdminStatusChangesOnlyMoveForward(t *testing.T) {
	store := &fakeOrderStore{}
	admin := NewOrderAdmin(store)
	ctx := context.Background()
	change := staffMutationFor(t, OrderStatusChange)
	for _, to := range []domain.OrderStatus{domain.OrderTransit, domain.OrderDelivered} {
		if _, err := admin.ChangeStatus(ctx, change, "ord-1", 3, to); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
	}
	// Cancellation returns stock, so it never travels as a plain status change.
	for _, to := range []domain.OrderStatus{domain.OrderCancelled, domain.OrderProcessing, "refunded", ""} {
		if _, err := admin.ChangeStatus(ctx, change, "ord-1", 3, to); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%q: %v", to, err)
		}
	}
	if _, err := admin.ChangeStatus(ctx, change, "ord-1", 0, domain.OrderTransit); fieldNames(err) == nil {
		t.Fatal("status change without a revision")
	}
	if _, err := admin.Cancel(ctx, staffMutationFor(t, OrderCancel), "ord-1", 4); err != nil {
		t.Fatal(err)
	}
	if strings.Join([]string{string(store.changes[0]), string(store.changes[1]), string(store.changes[2])}, ",") != "transit,delivered,cancelled" || len(store.changes) != 3 {
		t.Fatalf("%v", store.changes)
	}
}

func TestOrderAdminMutationsNeedTheirOwnAction(t *testing.T) {
	store := &fakeOrderStore{}
	admin := NewOrderAdmin(store)
	ctx := context.Background()
	if _, err := admin.Cancel(ctx, staffMutationFor(t, OrderStatusChange), "ord-1", 1); err == nil {
		t.Fatal("a status-change mutation cancelled an order")
	}
	if _, err := admin.ChangeStatus(ctx, staffMutationFor(t, OrderCancel), "ord-1", 1, domain.OrderTransit); err == nil {
		t.Fatal("a cancel mutation changed a status")
	}
	if _, err := admin.ChangeStatus(ctx, staffMutationFor(t, ProductUpdate), "ord-1", 1, domain.OrderTransit); err == nil {
		t.Fatal("a product mutation changed an order")
	}
	if len(store.changes) != 0 {
		t.Fatal("store reached")
	}
}

func TestOrderAdminListFilter(t *testing.T) {
	store := &fakeOrderStore{}
	admin := NewOrderAdmin(store)
	if _, err := admin.List(context.Background(), domain.AdminOrderFilter{Status: domain.OrderTransit, Query: " #10483 "}); err != nil {
		t.Fatal(err)
	}
	if store.filter != (domain.AdminOrderFilter{Status: domain.OrderTransit, Query: "#10483", Page: 1, Limit: 50}) {
		t.Fatalf("%+v", store.filter)
	}
	for _, filter := range []domain.AdminOrderFilter{{Status: "refunded"}, {Limit: 101}, {Page: -1}, {Query: strings.Repeat("q", 101)}} {
		if _, err := admin.List(context.Background(), filter); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%+v accepted", filter)
		}
	}
}

func TestOrderActionsAreValidAuditActions(t *testing.T) {
	for _, action := range []domain.AuditAction{OrderStatusChange, OrderCancel} {
		if !action.Valid() || !strings.HasPrefix(string(action), "order.") {
			t.Errorf("%q", action)
		}
	}
}
