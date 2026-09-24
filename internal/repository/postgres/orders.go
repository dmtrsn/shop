package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"sweetshop/internal/domain"
)

func (s *Store) ListOrders(ctx context.Context, userID string) ([]domain.Order, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,number,user_id,status,status_label,item_count,total_minor,delivery_address,comment,created_at,delivered_at FROM orders WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()
	result := []domain.Order{}
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, order)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	orderIDs := make([]string, 0, len(result))
	for _, order := range result {
		orderIDs = append(orderIDs, order.ID)
	}
	itemsByOrder, err := s.orderItemsByOrderIDs(ctx, orderIDs)
	if err != nil {
		return nil, err
	}
	for i := range result {
		result[i].Items = itemsByOrder[result[i].ID]
	}
	return result, nil
}

func (s *Store) Order(ctx context.Context, userID, orderID string) (domain.Order, error) {
	row := s.pool.QueryRow(ctx, `SELECT id,number,user_id,status,status_label,item_count,total_minor,delivery_address,comment,created_at,delivered_at FROM orders WHERE user_id=$1 AND (id=$2 OR number=$2)`, userID, orderID)
	order, err := scanOrder(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Order{}, fmt.Errorf("get order: %w", err)
	}
	order.Items, err = s.orderItems(ctx, order.ID)
	if err != nil {
		return domain.Order{}, err
	}
	return order, nil
}

func scanOrder(row scanner) (domain.Order, error) {
	var order domain.Order
	var delivered *time.Time
	err := row.Scan(&order.ID, &order.Number, &order.UserID, &order.Status, &order.StatusLabel, &order.ItemCount, &order.TotalMinor, &order.DeliveryAddress, &order.Comment, &order.CreatedAt, &delivered)
	if delivered != nil {
		order.DeliveredAt = *delivered
	}
	return order, err
}

func (s *Store) orderItems(ctx context.Context, orderID string) ([]domain.OrderItem, error) {
	rows, err := s.pool.Query(ctx, `SELECT product_id,sku,name,image,manufacturer_id,manufacturer,quantity,unit_price_minor,line_total_minor,replacement_product_id FROM order_items WHERE order_id=$1 ORDER BY position`, orderID)
	if err != nil {
		return nil, fmt.Errorf("load order items: %w", err)
	}
	defer rows.Close()
	items := []domain.OrderItem{}
	for rows.Next() {
		var item domain.OrderItem
		if err := rows.Scan(&item.ProductID, &item.SKU, &item.Name, &item.Image, &item.ManufacturerID, &item.Manufacturer, &item.Quantity, &item.UnitPriceMinor, &item.LineTotalMinor, &item.ReplacementProductID); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) orderItemsByOrderIDs(ctx context.Context, orderIDs []string) (map[string][]domain.OrderItem, error) {
	result := make(map[string][]domain.OrderItem, len(orderIDs))
	if len(orderIDs) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT order_id,product_id,sku,name,image,manufacturer_id,manufacturer,quantity,unit_price_minor,line_total_minor,replacement_product_id FROM order_items WHERE order_id=ANY($1) ORDER BY order_id,position`, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("load order history items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var orderID string
		var item domain.OrderItem
		if err := rows.Scan(&orderID, &item.ProductID, &item.SKU, &item.Name, &item.Image, &item.ManufacturerID, &item.Manufacturer, &item.Quantity, &item.UnitPriceMinor, &item.LineTotalMinor, &item.ReplacementProductID); err != nil {
			return nil, fmt.Errorf("scan order history item: %w", err)
		}
		result[orderID] = append(result[orderID], item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order history items: %w", err)
	}
	return result, nil
}

func (s *Store) CreateOrder(ctx context.Context, userID string, input domain.CheckoutInput) (domain.Order, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback(ctx)
	type checkoutRow struct {
		product  domain.Product
		quantity int
	}
	// Lock both the selected cart rows and their products. Cart mutations then
	// serialize with this snapshot instead of being accidentally ordered or
	// removed by a checkout that did not include them.
	rows, err := tx.Query(ctx, "SELECT "+productColumns+`,ci.quantity`+productJoins+` JOIN cart_items ci ON ci.product_id=p.id WHERE ci.user_id=$1 AND ci.selected=true ORDER BY p.id FOR UPDATE OF p,ci`, userID)
	if err != nil {
		return domain.Order{}, fmt.Errorf("lock checkout cart: %w", err)
	}
	selected := []checkoutRow{}
	for rows.Next() {
		var p domain.Product
		var qty int
		err := rows.Scan(
			&p.ID, &p.Slug, &p.SKU, &p.Name, &p.ShortName, &p.Description, &p.PriceMinor, &p.OldPriceMinor, &p.Currency, &p.Unit,
			&p.WeightGrams, &p.Image, &p.Gallery, &p.Series, &p.Tags, &p.Badges, &p.Ingredients, &p.Nutrition, &p.ShelfLifeDays,
			&p.Rating, &p.ReviewsCount, &p.Stock, &p.MinimumQuantity, &p.QuantityStep, &p.Featured, &p.CreatedAt,
			&p.Category.ID, &p.Category.Slug, &p.Category.Name, &p.Category.Description, &p.Category.ParentID, &p.Category.Icon,
			&p.Manufacturer.ID, &p.Manufacturer.Slug, &p.Manufacturer.Name, &p.Manufacturer.Country, &p.Manufacturer.Description, &p.Manufacturer.Logo,
			&p.Brand.ID, &p.Brand.Slug, &p.Brand.Name, &p.Brand.ManufacturerID, &p.Brand.Manufacturer, &p.Brand.Description, &p.Brand.Logo, &qty)
		if err != nil {
			rows.Close()
			return domain.Order{}, fmt.Errorf("scan checkout cart: %w", err)
		}
		if qty > p.Stock {
			rows.Close()
			return domain.Order{}, domain.ErrOutOfStock
		}
		selected = append(selected, checkoutRow{product: p, quantity: qty})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.Order{}, err
	}
	if len(selected) == 0 {
		return domain.Order{}, domain.ErrConflict
	}
	selectedProductIDs := make([]string, 0, len(selected))
	for _, row := range selected {
		selectedProductIDs = append(selectedProductIDs, row.product.ID)
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT nextval('order_number_seq')`).Scan(&sequence); err != nil {
		return domain.Order{}, fmt.Errorf("next order number: %w", err)
	}
	now := time.Now().UTC()
	order := domain.Order{ID: fmt.Sprintf("ord-%d-%d", now.Year(), sequence), Number: fmt.Sprintf("#%d", sequence), UserID: userID, Status: "processing", StatusLabel: "Собираем заказ", DeliveryAddress: input.DeliveryAddress, Comment: input.Comment, CreatedAt: now, Items: make([]domain.OrderItem, 0, len(selected))}
	for _, row := range selected {
		item := domain.OrderItem{ProductID: row.product.ID, SKU: row.product.SKU, Name: row.product.Name, Image: row.product.Image, ManufacturerID: row.product.Manufacturer.ID, Manufacturer: row.product.Manufacturer.Name, Quantity: row.quantity, UnitPriceMinor: row.product.PriceMinor, LineTotalMinor: int64(row.quantity) * row.product.PriceMinor}
		order.Items = append(order.Items, item)
		order.ItemCount += row.quantity
		order.TotalMinor += item.LineTotalMinor
	}
	order.TotalMinor += domain.DeliveryMinor(order.TotalMinor)
	_, err = tx.Exec(ctx, `INSERT INTO orders(id,number,user_id,status,status_label,item_count,total_minor,delivery_address,comment,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, order.ID, order.Number, userID, order.Status, order.StatusLabel, order.ItemCount, order.TotalMinor, order.DeliveryAddress, order.Comment, now)
	if err != nil {
		return domain.Order{}, fmt.Errorf("insert order: %w", err)
	}
	positions := make([]int32, 0, len(order.Items))
	productIDs := make([]string, 0, len(order.Items))
	skus := make([]string, 0, len(order.Items))
	names := make([]string, 0, len(order.Items))
	images := make([]string, 0, len(order.Items))
	manufacturerIDs := make([]string, 0, len(order.Items))
	manufacturers := make([]string, 0, len(order.Items))
	quantities := make([]int32, 0, len(order.Items))
	unitPrices := make([]int64, 0, len(order.Items))
	lineTotals := make([]int64, 0, len(order.Items))
	for i, item := range order.Items {
		positions = append(positions, int32(i+1))
		productIDs = append(productIDs, item.ProductID)
		skus = append(skus, item.SKU)
		names = append(names, item.Name)
		images = append(images, item.Image)
		manufacturerIDs = append(manufacturerIDs, item.ManufacturerID)
		manufacturers = append(manufacturers, item.Manufacturer)
		quantities = append(quantities, int32(item.Quantity))
		unitPrices = append(unitPrices, item.UnitPriceMinor)
		lineTotals = append(lineTotals, item.LineTotalMinor)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO order_items(order_id,position,product_id,sku,name,image,manufacturer_id,manufacturer,quantity,unit_price_minor,line_total_minor)
		SELECT $1,items.position,items.product_id,items.sku,items.name,items.image,items.manufacturer_id,items.manufacturer,items.quantity,items.unit_price_minor,items.line_total_minor
		FROM unnest($2::integer[],$3::text[],$4::text[],$5::text[],$6::text[],$7::text[],$8::text[],$9::integer[],$10::bigint[],$11::bigint[])
			AS items(position,product_id,sku,name,image,manufacturer_id,manufacturer,quantity,unit_price_minor,line_total_minor)`,
		order.ID, positions, productIDs, skus, names, images, manufacturerIDs, manufacturers, quantities, unitPrices, lineTotals)
	if err != nil {
		return domain.Order{}, fmt.Errorf("insert order items: %w", err)
	}
	command, err := tx.Exec(ctx, `
		UPDATE products AS product SET stock=product.stock-items.quantity,updated_at=now()
		FROM unnest($1::text[],$2::integer[]) AS items(product_id,quantity)
		WHERE product.id=items.product_id AND product.stock>=items.quantity`, productIDs, quantities)
	if err != nil {
		return domain.Order{}, fmt.Errorf("update checkout stock: %w", err)
	}
	if command.RowsAffected() != int64(len(order.Items)) {
		return domain.Order{}, domain.ErrOutOfStock
	}
	_, err = tx.Exec(ctx, `DELETE FROM cart_items WHERE user_id=$1 AND product_id=ANY($2)`, userID, selectedProductIDs)
	if err != nil {
		return domain.Order{}, fmt.Errorf("clear checked out cart: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Order{}, fmt.Errorf("commit order: %w", err)
	}
	return order, nil
}
