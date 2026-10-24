package domain

import (
	"reflect"
	"testing"
)

func TestOrderLifecycle(t *testing.T) {
	all := []OrderStatus{OrderProcessing, OrderTransit, OrderDelivered, OrderCancelled, "unknown", ""}
	allowed := map[[2]OrderStatus]bool{
		{OrderProcessing, OrderTransit}:   true,
		{OrderProcessing, OrderCancelled}: true,
		{OrderTransit, OrderDelivered}:    true,
	}
	for _, from := range all {
		for _, to := range all {
			if got := OrderTransitionAllowed(from, to); got != allowed[[2]OrderStatus{from, to}] {
				t.Errorf("%q -> %q: %v", from, to, got)
			}
		}
	}
	for _, terminal := range []OrderStatus{OrderDelivered, OrderCancelled, "unknown"} {
		if next := NextOrderStatuses(terminal); len(next) != 0 {
			t.Errorf("%q is not terminal: %v", terminal, next)
		}
	}
	if !reflect.DeepEqual(NextOrderStatuses(OrderProcessing), []OrderStatus{OrderTransit, OrderCancelled}) {
		t.Fatal("processing transitions")
	}
	// The lists are copies: callers cannot change the table.
	NextOrderStatuses(OrderProcessing)[0] = OrderDelivered
	if OrderTransitionAllowed(OrderProcessing, OrderDelivered) {
		t.Fatal("transition table mutated")
	}
}

func TestOrderLabelsKeepTheExistingOnes(t *testing.T) {
	// Checkout writes "Собираем заказ" and the seed "Доставлен"; the legacy UI
	// already names transit and cancelled.
	for status, label := range map[OrderStatus]string{OrderProcessing: "Собираем заказ", OrderTransit: "В пути", OrderDelivered: "Доставлен", OrderCancelled: "Отменён"} {
		if status.Label() != label || !status.Known() {
			t.Errorf("%q: %q", status, status.Label())
		}
	}
	if OrderStatus("refunded").Known() {
		t.Fatal("unknown status is known")
	}
}
