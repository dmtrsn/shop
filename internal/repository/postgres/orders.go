package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

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

// orderIdempotencyIndex is the only unique violation that means "another
// request committed this key"; every other 23505 is an ordinary failure.
const orderIdempotencyIndex = "orders_user_idempotency_key_idx"

func (s *Store) CreateOrder(ctx context.Context, userID string, input domain.CheckoutInput) (domain.Order, bool, error) {
	if input.IdempotencyKey == nil {
		order, err := s.createOrder(ctx, userID, input)
		return order, false, err
	}
	if input.RequestDigest == "" {
		return domain.Order{}, false, errors.New("keyed checkout without request digest")
	}
	// A committed key wins before the cart is consulted: the first request
	// consumed the selected lines, so a retry must replay instead of conflicting.
	if order, found, err := s.orderByIdempotencyKey(ctx, userID, *input.IdempotencyKey, input.RequestDigest); found || err != nil {
		return order, found && err == nil, err
	}
	order, err := s.createOrder(ctx, userID, input)
	if err == nil {
		return order, false, nil
	}
	// The transaction has rolled back. A concurrent request with the same key
	// may have committed meanwhile; its record decides the outcome.
	if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrOutOfStock) || errors.Is(err, domain.ErrCheckoutChanged) || isOrderIdempotencyViolation(err) {
		if replay, found, lookupErr := s.orderByIdempotencyKey(ctx, userID, *input.IdempotencyKey, input.RequestDigest); found || lookupErr != nil {
			return replay, found && lookupErr == nil, lookupErr
		}
	}
	return domain.Order{}, false, err
}

// orderByIdempotencyKey reports found=true when the user already committed the
// key: the stored order for the same digest, or ErrIdempotencyKeyReused.
func (s *Store) orderByIdempotencyKey(ctx context.Context, userID, key, digest string) (domain.Order, bool, error) {
	var orderID, storedDigest string
	err := s.pool.QueryRow(ctx, `SELECT id,request_digest FROM orders WHERE user_id=$1 AND idempotency_key=$2`, userID, key).Scan(&orderID, &storedDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, false, nil
	}
	if err != nil {
		return domain.Order{}, false, fmt.Errorf("load idempotent order: %w", err)
	}
	if storedDigest != digest {
		return domain.Order{}, true, domain.ErrIdempotencyKeyReused
	}
	order, err := s.Order(ctx, userID, orderID)
	if err != nil {
		return domain.Order{}, true, fmt.Errorf("load replayed order: %w", err)
	}
	// pgx scans timestamptz in the process zone; the original 201 was UTC.
	order.CreatedAt = order.CreatedAt.UTC()
	return order, true, nil
}

func isOrderIdempotencyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == orderIdempotencyIndex
}

func (s *Store) createOrder(ctx context.Context, userID string, input domain.CheckoutInput) (domain.Order, error) {
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
	items := make([]domain.OrderItem, 0, len(selected))
	itemCount, subtotal := 0, int64(0)
	for _, row := range selected {
		selectedProductIDs = append(selectedProductIDs, row.product.ID)
		item := domain.OrderItem{ProductID: row.product.ID, SKU: row.product.SKU, Name: row.product.Name, Image: row.product.Image, ManufacturerID: row.product.Manufacturer.ID, Manufacturer: row.product.Manufacturer.Name, Quantity: row.quantity, UnitPriceMinor: row.product.PriceMinor, LineTotalMinor: int64(row.quantity) * row.product.PriceMinor}
		items = append(items, item)
		itemCount += row.quantity
		subtotal += item.LineTotalMinor
	}
	total := subtotal + domain.DeliveryMinor(subtotal)
	// Compare the confirmed review with the locked lines before any write, even
	// before the non-transactional order number sequence is advanced.
	if !domain.CheckoutMatchesExpectation(input, items, total) {
		return domain.Order{}, domain.ErrCheckoutChanged
	}
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT nextval('order_number_seq')`).Scan(&sequence); err != nil {
		return domain.Order{}, fmt.Errorf("next order number: %w", err)
	}
	// PostgreSQL stores microseconds; return exactly the committed instant.
	now := time.Now().UTC().Truncate(time.Microsecond)
	order := domain.Order{ID: fmt.Sprintf("ord-%d-%d", now.Year(), sequence), Number: fmt.Sprintf("#%d", sequence), UserID: userID, Status: "processing", StatusLabel: "Собираем заказ", DeliveryAddress: input.DeliveryAddress, Comment: input.Comment, CreatedAt: now, Items: items, ItemCount: itemCount, TotalMinor: total}
	var requestDigest *string
	if input.IdempotencyKey != nil {
		requestDigest = &input.RequestDigest
	}
	_, err = tx.Exec(ctx, `INSERT INTO orders(id,number,user_id,status,status_label,item_count,total_minor,delivery_address,comment,created_at,idempotency_key,request_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, order.ID, order.Number, userID, order.Status, order.StatusLabel, order.ItemCount, order.TotalMinor, order.DeliveryAddress, order.Comment, now, input.IdempotencyKey, requestDigest)
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
