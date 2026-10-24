package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"sweetshop/internal/domain"
)

// Staff order changes (ADR 016) run in InStaffMutation. Lock order: the
// actor's staff row, then the order row FOR UPDATE, then the ordered products
// FOR UPDATE in ID order (the order checkout locks products in). Repeat-order
// commits lock the order FOR SHARE before products too, and checkout never
// locks an existing order, so no path takes a product before an order.

const adminOrderColumns = `o.id,o.number,o.revision,o.status,o.status_label,o.created_at,o.delivered_at,
	u.id,u.name,u.email,u.phone,o.delivery_address,o.comment,o.item_count,o.total_minor`

const adminOrderJoins = ` FROM orders o JOIN users u ON u.id=o.user_id `

func scanAdminOrder(row scanner) (domain.AdminOrder, error) {
	var o domain.AdminOrder
	var delivered *time.Time
	var status string
	err := row.Scan(&o.ID, &o.Number, &o.Revision, &status, &o.StatusLabel, &o.CreatedAt, &delivered,
		&o.Customer.ID, &o.Customer.Name, &o.Customer.Email, &o.Customer.Phone, &o.DeliveryAddress, &o.Comment, &o.ItemCount, &o.TotalMinor)
	if err != nil {
		return domain.AdminOrder{}, err
	}
	o.Status = domain.OrderStatus(status)
	o.AllowedTransitions = domain.NextOrderStatuses(o.Status)
	o.CreatedAt = o.CreatedAt.UTC()
	if delivered != nil {
		utc := delivered.UTC()
		o.DeliveredAt = &utc
	}
	return o, nil
}

func adminOrder(ctx context.Context, q interface {
	rowQuerier
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}, id string) (domain.AdminOrder, error) {
	order, err := scanAdminOrder(q.QueryRow(ctx, "SELECT "+adminOrderColumns+adminOrderJoins+" WHERE o.id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AdminOrder{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AdminOrder{}, fmt.Errorf("load admin order: %w", err)
	}
	rows, err := q.Query(ctx, `SELECT position,product_id,sku,name,manufacturer,quantity,unit_price_minor,line_total_minor
		FROM order_items WHERE order_id=$1 ORDER BY position`, id)
	if err != nil {
		return domain.AdminOrder{}, fmt.Errorf("load admin order lines: %w", err)
	}
	defer rows.Close()
	order.Items = []domain.AdminOrderLine{}
	for rows.Next() {
		var line domain.AdminOrderLine
		if err := rows.Scan(&line.Position, &line.ProductID, &line.SKU, &line.Name, &line.Manufacturer, &line.Quantity, &line.UnitPriceMinor, &line.LineTotalMinor); err != nil {
			return domain.AdminOrder{}, fmt.Errorf("scan admin order line: %w", err)
		}
		order.Items = append(order.Items, line)
	}
	if err := rows.Err(); err != nil {
		return domain.AdminOrder{}, fmt.Errorf("iterate admin order lines: %w", err)
	}
	return order, nil
}

func (s *Store) AdminOrder(ctx context.Context, id string) (domain.AdminOrder, error) {
	return adminOrder(ctx, s.pool, id)
}

func (s *Store) AdminOrders(ctx context.Context, filter domain.AdminOrderFilter) (domain.AdminOrderPage, error) {
	conditions := []string{}
	args := []any{}
	if filter.Status != "" {
		args = append(args, string(filter.Status))
		conditions = append(conditions, fmt.Sprintf("o.status=$%d", len(args)))
	}
	if filter.Query != "" {
		args = append(args, filter.Query, "#"+strings.TrimPrefix(filter.Query, "#"), "%"+escapeLike(strings.ToLower(filter.Query))+"%")
		n := len(args)
		conditions = append(conditions, fmt.Sprintf("(o.id=$%d OR o.number=$%d OR lower(u.email) LIKE $%d OR lower(u.name) LIKE $%d)", n-2, n-1, n, n))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT count(*)"+adminOrderJoins+where, args...).Scan(&total); err != nil {
		return domain.AdminOrderPage{}, fmt.Errorf("count admin orders: %w", err)
	}
	args = append(args, filter.Limit, (filter.Page-1)*filter.Limit)
	rows, err := s.pool.Query(ctx, "SELECT "+adminOrderColumns+adminOrderJoins+where+
		fmt.Sprintf(" ORDER BY o.created_at DESC,o.id LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return domain.AdminOrderPage{}, fmt.Errorf("list admin orders: %w", err)
	}
	defer rows.Close()
	page := domain.AdminOrderPage{Items: []domain.AdminOrder{}, Total: total, Page: filter.Page, Limit: filter.Limit,
		TotalPages: int(math.Ceil(float64(total) / float64(filter.Limit)))}
	for rows.Next() {
		order, err := scanAdminOrder(rows)
		if err != nil {
			return domain.AdminOrderPage{}, fmt.Errorf("scan admin order: %w", err)
		}
		page.Items = append(page.Items, order)
	}
	if err := rows.Err(); err != nil {
		return domain.AdminOrderPage{}, fmt.Errorf("iterate admin orders: %w", err)
	}
	return page, nil
}

func (s *Store) SetOrderStatus(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, to domain.OrderStatus) (domain.AdminOrder, error) {
	var result domain.AdminOrder
	_, err := s.InStaffMutation(ctx, mutation, func(ctx context.Context, tx StaffTx) (domain.AuditTarget, error) {
		var status string
		var revision int64
		err := tx.QueryRow(ctx, `SELECT status,revision FROM orders WHERE id=$1 FOR UPDATE`, id).Scan(&status, &revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.AuditTarget{}, domain.ErrNotFound
		}
		if err != nil {
			return domain.AuditTarget{}, fmt.Errorf("lock order: %w", err)
		}
		if revision != expectedRevision {
			return domain.AuditTarget{}, domain.ErrStaleRevision
		}
		// Under the order lock the transition is checked once: an order can
		// reach cancelled only from processing, so its stock returns once.
		if !domain.OrderTransitionAllowed(domain.OrderStatus(status), to) {
			return domain.AuditTarget{}, domain.ErrInvalidTransition
		}
		if to == domain.OrderCancelled {
			if err := restockOrder(ctx, tx, id); err != nil {
				return domain.AuditTarget{}, err
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE orders SET status=$2,status_label=$3,revision=revision+1,
				delivered_at=CASE WHEN $2='delivered' THEN now() ELSE delivered_at END
			WHERE id=$1`, id, string(to), to.Label()); err != nil {
			return domain.AuditTarget{}, fmt.Errorf("update order status: %w", err)
		}
		if result, err = adminOrder(ctx, tx, id); err != nil {
			return domain.AuditTarget{}, err
		}
		return domain.AuditTarget{Type: "order", ID: id}, nil
	})
	if err != nil {
		return domain.AdminOrder{}, err
	}
	return result, nil
}

// restockOrder returns every ordered quantity to its product, archived or
// not (archiving keeps the row; it is not reactivated). Each change is a new
// product revision, as a sale is, so a staff stock edit based on the earlier
// stock is refused. A product row that no longer exists gets nothing back.
func restockOrder(ctx context.Context, tx StaffTx, orderID string) error {
	if _, err := tx.Exec(ctx, `
		SELECT p.id FROM products p
		WHERE p.id IN (SELECT product_id FROM order_items WHERE order_id=$1)
		ORDER BY p.id FOR UPDATE`, orderID); err != nil {
		return fmt.Errorf("lock ordered products: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE products p SET stock=p.stock+items.quantity,revision=p.revision+1,updated_at=now()
		FROM (SELECT product_id,sum(quantity) AS quantity FROM order_items WHERE order_id=$1 GROUP BY product_id) items
		WHERE p.id=items.product_id`, orderID); err != nil {
		return fmt.Errorf("restock order: %w", err)
	}
	return nil
}
