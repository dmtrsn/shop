package application

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"sweetshop/internal/domain"
)

// Staff order actions (ADR 016): reads are authorized as order.read, and each
// mutation is authorized and audited under its own action.
const (
	OrderRead         = "order.read"
	OrderStatusChange = domain.AuditAction("order.status_change")
	OrderCancel       = domain.AuditAction("order.cancel")
)

// OrderAdminStore is the staff order port; only PostgreSQL implements it.
type OrderAdminStore interface {
	AdminOrders(ctx context.Context, filter domain.AdminOrderFilter) (domain.AdminOrderPage, error)
	// AdminOrder returns ErrNotFound for an unknown ID.
	AdminOrder(ctx context.Context, id string) (domain.AdminOrder, error)
	// SetOrderStatus runs in one audited staff transaction: it locks the
	// order, requires the expected revision (ErrStaleRevision) and an allowed
	// transition (ErrInvalidTransition), returns the ordered stock to
	// inventory when the new status is cancelled, and increments the revision
	// once. On any error nothing changes.
	SetOrderStatus(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, to domain.OrderStatus) (domain.AdminOrder, error)
}

type OrderAdmin struct {
	store OrderAdminStore
}

func NewOrderAdmin(store OrderAdminStore) *OrderAdmin {
	return &OrderAdmin{store: store}
}

func (a *OrderAdmin) List(ctx context.Context, filter domain.AdminOrderFilter) (domain.AdminOrderPage, error) {
	fields := []domain.FieldError{}
	if filter.Status != "" && !filter.Status.Known() {
		fields = append(fields, domain.FieldError{Field: "status", Message: "Допустимо: processing, transit, delivered, cancelled"})
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if utf8.RuneCountInString(filter.Query) > 100 || !singleLine(filter.Query) {
		fields = append(fields, domain.FieldError{Field: "q", Message: "Запрос: до 100 символов в одну строку"})
	}
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.Limit == 0 {
		filter.Limit = defaultAdminLimit
	}
	if filter.Page < 1 || filter.Page > 10_000 {
		fields = append(fields, domain.FieldError{Field: "page", Message: "Страница: от 1 до 10000"})
	}
	if filter.Limit < 1 || filter.Limit > MaxAdminPageLimit {
		fields = append(fields, domain.FieldError{Field: "limit", Message: fmt.Sprintf("Размер страницы: от 1 до %d", MaxAdminPageLimit)})
	}
	if len(fields) > 0 {
		return domain.AdminOrderPage{}, &domain.ValidationError{Fields: fields}
	}
	return a.store.AdminOrders(ctx, filter)
}

func (a *OrderAdmin) Get(ctx context.Context, id string) (domain.AdminOrder, error) {
	return a.store.AdminOrder(ctx, id)
}

// ChangeStatus moves an order forward in its fulfilment (transit, delivered).
// Cancellation has its own action because it returns stock.
func (a *OrderAdmin) ChangeStatus(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, to domain.OrderStatus) (domain.AdminOrder, error) {
	if err := requireAction(mutation, OrderStatusChange); err != nil {
		return domain.AdminOrder{}, err
	}
	fields := revisionFields(expectedRevision)
	if to != domain.OrderTransit && to != domain.OrderDelivered {
		fields = append(fields, domain.FieldError{Field: "status", Message: "Допустимо: transit или delivered; для отмены есть отдельное действие"})
	}
	if len(fields) > 0 {
		return domain.AdminOrder{}, &domain.ValidationError{Fields: fields}
	}
	return a.store.SetOrderStatus(ctx, mutation, id, expectedRevision, to)
}

// Cancel cancels an order that has not shipped and returns its stock.
func (a *OrderAdmin) Cancel(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64) (domain.AdminOrder, error) {
	if err := requireAction(mutation, OrderCancel); err != nil {
		return domain.AdminOrder{}, err
	}
	if fields := revisionFields(expectedRevision); len(fields) > 0 {
		return domain.AdminOrder{}, &domain.ValidationError{Fields: fields}
	}
	return a.store.SetOrderStatus(ctx, mutation, id, expectedRevision, domain.OrderCancelled)
}
