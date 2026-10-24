package domain

import (
	"errors"
	"time"
)

// Staff order management (ADR 016). Orders are never deleted; staff move
// them through a small explicit lifecycle, and every change names the
// revision it was based on (ErrStaleRevision otherwise).

// ErrInvalidTransition: the order's current status does not allow the
// requested change. Nothing was written.
var ErrInvalidTransition = errors.New("order status transition not allowed")

type OrderStatus string

const (
	// OrderProcessing is what checkout writes.
	OrderProcessing OrderStatus = "processing"
	OrderTransit    OrderStatus = "transit"
	OrderDelivered  OrderStatus = "delivered"
	OrderCancelled  OrderStatus = "cancelled"
)

// orderLabels are the customer-facing labels stored with the status; the
// processing and delivered labels are the ones checkout and the seed write.
var orderLabels = map[OrderStatus]string{
	OrderProcessing: "Собираем заказ",
	OrderTransit:    "В пути",
	OrderDelivered:  "Доставлен",
	OrderCancelled:  "Отменён",
}

// orderTransitions is the whole staff lifecycle:
//
//	processing → transit → delivered
//	processing → cancelled (stock returns to inventory)
//
// delivered and cancelled are terminal, and so is any status this table
// does not know.
var orderTransitions = map[OrderStatus][]OrderStatus{
	OrderProcessing: {OrderTransit, OrderCancelled},
	OrderTransit:    {OrderDelivered},
}

func (s OrderStatus) Label() string { return orderLabels[s] }

func (s OrderStatus) Known() bool { _, ok := orderLabels[s]; return ok }

// NextOrderStatuses lists the statuses staff may move an order to from s.
func NextOrderStatuses(from OrderStatus) []OrderStatus {
	return append([]OrderStatus{}, orderTransitions[from]...)
}

func OrderTransitionAllowed(from, to OrderStatus) bool {
	for _, next := range orderTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
}

type OrderCustomer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

type AdminOrderLine struct {
	Position       int    `json:"position"`
	ProductID      string `json:"productId"`
	SKU            string `json:"sku"`
	Name           string `json:"name"`
	Manufacturer   string `json:"manufacturer"`
	Quantity       int    `json:"quantity"`
	UnitPriceMinor int64  `json:"unitPriceMinor"`
	LineTotalMinor int64  `json:"lineTotalMinor"`
}

// AdminOrder is the staff view of an order. Lines, totals, address and
// comment are the snapshot checkout stored; Customer is the account as it is
// now (orders store no contact snapshot). Items is nil in list results.
type AdminOrder struct {
	ID                 string           `json:"id"`
	Number             string           `json:"number"`
	Revision           int64            `json:"revision"`
	Status             OrderStatus      `json:"status"`
	StatusLabel        string           `json:"statusLabel"`
	AllowedTransitions []OrderStatus    `json:"allowedTransitions"`
	CreatedAt          time.Time        `json:"createdAt"`
	DeliveredAt        *time.Time       `json:"deliveredAt,omitempty"`
	Customer           OrderCustomer    `json:"customer"`
	DeliveryAddress    string           `json:"deliveryAddress"`
	Comment            string           `json:"comment"`
	ItemCount          int              `json:"itemCount"`
	TotalMinor         int64            `json:"totalMinor"`
	Items              []AdminOrderLine `json:"items,omitempty"`
}

type AdminOrderFilter struct {
	// Status is one OrderStatus or "" for all.
	Status OrderStatus
	// Query matches the order ID or number exactly, or a substring of the
	// customer's email or name, case-insensitively.
	Query string
	Page  int
	Limit int
}

type AdminOrderPage struct {
	Items      []AdminOrder `json:"items"`
	Total      int          `json:"total"`
	Page       int          `json:"page"`
	Limit      int          `json:"limit"`
	TotalPages int          `json:"totalPages"`
}
