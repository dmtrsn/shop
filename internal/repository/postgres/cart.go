package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"sweetshop/internal/domain"
)

type storedCartRow struct {
	productID string
	quantity  int
	selected  bool
	updatedAt time.Time
}

func (s *Store) Cart(ctx context.Context, userID string) (domain.Cart, error) {
	rows, err := s.pool.Query(ctx, `SELECT product_id,quantity,selected,updated_at FROM cart_items WHERE user_id=$1`, userID)
	if err != nil {
		return domain.Cart{}, fmt.Errorf("load cart: %w", err)
	}
	defer rows.Close()
	stored := []storedCartRow{}
	ids := []string{}
	for rows.Next() {
		var row storedCartRow
		if err := rows.Scan(&row.productID, &row.quantity, &row.selected, &row.updatedAt); err != nil {
			return domain.Cart{}, fmt.Errorf("scan cart: %w", err)
		}
		stored = append(stored, row)
		ids = append(ids, row.productID)
	}
	if err := rows.Err(); err != nil {
		return domain.Cart{}, err
	}
	// Archived lines stay visible as unavailable, so they can be removed.
	products, err := s.productsByID(ctx, ids, true)
	if err != nil {
		return domain.Cart{}, fmt.Errorf("load cart products: %w", err)
	}
	return buildCart(stored, products), nil
}

func buildCart(rows []storedCartRow, products map[string]domain.Product) domain.Cart {
	items := make([]domain.CartItem, 0, len(rows))
	var updated time.Time
	for _, row := range rows {
		p, ok := products[row.productID]
		if !ok {
			continue
		}
		if p.Archived {
			p.Stock = 0
		}
		item := domain.CartItem{Product: p, Quantity: row.quantity, Selected: row.selected, Available: p.Stock, LineTotalMinor: int64(row.quantity) * p.PriceMinor}
		item.Issue = domain.CartLineIssue(p, row.quantity)
		items = append(items, item)
		if row.updatedAt.After(updated) {
			updated = row.updatedAt
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Product.Manufacturer.Name == items[j].Product.Manufacturer.Name {
			return items[i].Product.Name < items[j].Product.Name
		}
		return items[i].Product.Manufacturer.Name < items[j].Product.Manufacturer.Name
	})
	groups := []domain.CartGroup{}
	groupIndex := map[string]int{}
	summary := domain.CartSummary{PositionCount: len(items)}
	for _, item := range items {
		summary.ItemCount += item.Quantity
		if item.Issue != "" {
			summary.UnavailableCount++
		}
		idx, ok := groupIndex[item.Product.Manufacturer.ID]
		if !ok {
			idx = len(groups)
			groupIndex[item.Product.Manufacturer.ID] = idx
			groups = append(groups, domain.CartGroup{Manufacturer: item.Product.Manufacturer, Items: []domain.CartItem{}})
		}
		groups[idx].Items = append(groups[idx].Items, item)
		if item.Selected {
			summary.SelectedCount += item.Quantity
			groups[idx].SelectedCount += item.Quantity
			if item.Issue == "" {
				originalLineTotal := int64(item.Quantity) * domain.ListPriceMinor(item.Product)
				summary.SubtotalMinor += originalLineTotal
				summary.DiscountMinor += originalLineTotal - item.LineTotalMinor
				groups[idx].SubtotalMinor += item.LineTotalMinor
			}
		}
	}
	summary.DeliveryMinor = domain.DeliveryMinor(summary.SubtotalMinor - summary.DiscountMinor)
	summary.TotalMinor = summary.SubtotalMinor - summary.DiscountMinor + summary.DeliveryMinor
	return domain.Cart{Items: items, Groups: groups, Summary: summary, UpdatedAt: updated}
}

func (s *Store) SetCartItem(ctx context.Context, userID, productID string, quantity int, selected bool) (domain.Cart, error) {
	tx, err := s.beginCartMutation(ctx, userID)
	if err != nil {
		return domain.Cart{}, err
	}
	defer tx.Rollback(ctx)
	var id string
	var stock int
	err = tx.QueryRow(ctx, `SELECT id,stock FROM products WHERE (id=$1 OR slug=$1 OR lower(sku)=lower($1)) AND archived_at IS NULL ORDER BY id FOR UPDATE`, productID).Scan(&id, &stock)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Cart{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Cart{}, fmt.Errorf("lock cart product: %w", err)
	}
	if quantity > stock {
		return domain.Cart{}, domain.ErrOutOfStock
	}
	_, err = tx.Exec(ctx, `INSERT INTO cart_items(user_id,product_id,quantity,selected,updated_at) VALUES($1,$2,$3,$4,now()) ON CONFLICT(user_id,product_id) DO UPDATE SET quantity=EXCLUDED.quantity,selected=EXCLUDED.selected,updated_at=now()`, userID, id, quantity, selected)
	if err != nil {
		return domain.Cart{}, fmt.Errorf("set cart item: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Cart{}, err
	}
	return s.Cart(ctx, userID)
}

func (s *Store) RemoveCartItem(ctx context.Context, userID, productID string) (domain.Cart, error) {
	tx, err := s.beginCartMutation(ctx, userID)
	if err != nil {
		return domain.Cart{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `WITH locked AS MATERIALIZED (SELECT ci.user_id,ci.product_id FROM cart_items ci JOIN products p ON ci.product_id=p.id WHERE ci.user_id=$1 AND (p.id=$2 OR p.slug=$2 OR lower(p.sku)=lower($2)) ORDER BY ci.product_id FOR UPDATE OF ci) DELETE FROM cart_items ci USING locked l WHERE ci.user_id=l.user_id AND ci.product_id=l.product_id`, userID, productID)
	if err != nil {
		return domain.Cart{}, fmt.Errorf("remove cart item: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Cart{}, err
	}
	return s.Cart(ctx, userID)
}

func (s *Store) ClearCart(ctx context.Context, userID string, selectedOnly bool) (domain.Cart, error) {
	tx, err := s.beginCartMutation(ctx, userID)
	if err != nil {
		return domain.Cart{}, err
	}
	defer tx.Rollback(ctx)
	query := `WITH locked AS MATERIALIZED (SELECT user_id,product_id FROM cart_items WHERE user_id=$1`
	if selectedOnly {
		query += ` AND selected=true`
	}
	query += ` ORDER BY product_id FOR UPDATE) DELETE FROM cart_items ci USING locked l WHERE ci.user_id=l.user_id AND ci.product_id=l.product_id`
	if _, err := tx.Exec(ctx, query, userID); err != nil {
		return domain.Cart{}, fmt.Errorf("clear cart: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Cart{}, err
	}
	return s.Cart(ctx, userID)
}

func (s *Store) SetCartSelection(ctx context.Context, userID string, productIDs []string, selected bool) (domain.Cart, error) {
	tx, err := s.beginCartMutation(ctx, userID)
	if err != nil {
		return domain.Cart{}, err
	}
	defer tx.Rollback(ctx)
	if len(productIDs) == 0 {
		_, err = tx.Exec(ctx, `WITH locked AS MATERIALIZED (SELECT user_id,product_id FROM cart_items WHERE user_id=$1 ORDER BY product_id FOR UPDATE) UPDATE cart_items ci SET selected=$2,updated_at=now() FROM locked l WHERE ci.user_id=l.user_id AND ci.product_id=l.product_id`, userID, selected)
	} else {
		_, err = tx.Exec(ctx, `WITH locked AS MATERIALIZED (SELECT user_id,product_id FROM cart_items WHERE user_id=$1 AND product_id=ANY($2) ORDER BY product_id FOR UPDATE) UPDATE cart_items ci SET selected=$3,updated_at=now() FROM locked l WHERE ci.user_id=l.user_id AND ci.product_id=l.product_id`, userID, productIDs, selected)
	}
	if err != nil {
		return domain.Cart{}, fmt.Errorf("select cart items: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Cart{}, err
	}
	return s.Cart(ctx, userID)
}

func (s *Store) AddCartItems(ctx context.Context, userID string, mutations []domain.CartMutation) (domain.Cart, error) {
	if len(mutations) == 0 {
		return s.Cart(ctx, userID)
	}
	sort.Slice(mutations, func(i, j int) bool { return mutations[i].ProductID < mutations[j].ProductID })
	productIDs := make([]string, 0, len(mutations))
	for i, mutation := range mutations {
		if i > 0 && mutations[i-1].ProductID == mutation.ProductID {
			return domain.Cart{}, domain.ErrOutOfStock
		}
		productIDs = append(productIDs, mutation.ProductID)
	}
	tx, err := s.beginCartMutation(ctx, userID)
	if err != nil {
		return domain.Cart{}, err
	}
	defer tx.Rollback(ctx)
	type quantityRule struct {
		stock   int
		minimum int
		step    int
	}
	rules := make(map[string]quantityRule, len(productIDs))
	rows, err := tx.Query(ctx, `SELECT id,stock,minimum_quantity,quantity_step FROM products WHERE id=ANY($1) AND archived_at IS NULL ORDER BY id FOR UPDATE`, productIDs)
	if err != nil {
		return domain.Cart{}, fmt.Errorf("lock repeated products: %w", err)
	}
	for rows.Next() {
		var id string
		var rule quantityRule
		if err := rows.Scan(&id, &rule.stock, &rule.minimum, &rule.step); err != nil {
			rows.Close()
			return domain.Cart{}, fmt.Errorf("scan repeated product: %w", err)
		}
		rules[id] = rule
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.Cart{}, fmt.Errorf("iterate repeated products: %w", err)
	}
	if len(rules) != len(productIDs) {
		return domain.Cart{}, domain.ErrNotFound
	}

	existing := make(map[string]int, len(productIDs))
	rows, err = tx.Query(ctx, `SELECT product_id,quantity FROM cart_items WHERE user_id=$1 AND product_id=ANY($2) ORDER BY product_id FOR UPDATE`, userID, productIDs)
	if err != nil {
		return domain.Cart{}, fmt.Errorf("lock repeated cart items: %w", err)
	}
	for rows.Next() {
		var id string
		var quantity int
		if err := rows.Scan(&id, &quantity); err != nil {
			rows.Close()
			return domain.Cart{}, fmt.Errorf("scan repeated cart item: %w", err)
		}
		existing[id] = quantity
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.Cart{}, fmt.Errorf("iterate repeated cart items: %w", err)
	}

	quantities := make([]int32, 0, len(mutations))
	selections := make([]bool, 0, len(mutations))
	for _, mutation := range mutations {
		rule := rules[mutation.ProductID]
		minimum := max(1, rule.minimum)
		step := max(1, rule.step)
		finalQuantity := existing[mutation.ProductID] + mutation.Quantity
		if mutation.Quantity < 1 || finalQuantity > rule.stock || finalQuantity < minimum || (finalQuantity-minimum)%step != 0 {
			return domain.Cart{}, domain.ErrOutOfStock
		}
		quantities = append(quantities, int32(mutation.Quantity))
		selections = append(selections, mutation.Selected)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO cart_items(user_id,product_id,quantity,selected,updated_at)
		SELECT $1,items.product_id,items.quantity,items.selected,now()
		FROM unnest($2::text[],$3::integer[],$4::boolean[]) AS items(product_id,quantity,selected)
		ON CONFLICT(user_id,product_id) DO UPDATE SET
			quantity=cart_items.quantity+EXCLUDED.quantity,
			selected=cart_items.selected OR EXCLUDED.selected,
			updated_at=now()`, userID, productIDs, quantities, selections)
	if err != nil {
		return domain.Cart{}, fmt.Errorf("add repeated products: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Cart{}, fmt.Errorf("commit repeated products: %w", err)
	}
	return s.Cart(ctx, userID)
}

func (s *Store) Favorites(ctx context.Context, userID string) (domain.Favorites, error) {
	// Favorites of archived products are kept but hidden until a restore.
	rows, err := s.pool.Query(ctx, "SELECT "+productColumns+productJoins+` JOIN favorites f ON f.product_id=p.id WHERE f.user_id=$1 AND p.archived_at IS NULL ORDER BY m.name,p.name`, userID)
	if err != nil {
		return domain.Favorites{}, fmt.Errorf("load favorites: %w", err)
	}
	defer rows.Close()
	items := []domain.Product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return domain.Favorites{}, err
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return domain.Favorites{}, err
	}
	groups := []domain.FavoriteGroup{}
	index := map[string]int{}
	for _, p := range items {
		idx, ok := index[p.Manufacturer.ID]
		if !ok {
			idx = len(groups)
			index[p.Manufacturer.ID] = idx
			groups = append(groups, domain.FavoriteGroup{Manufacturer: p.Manufacturer, Products: []domain.Product{}})
		}
		groups[idx].Products = append(groups[idx].Products, p)
	}
	return domain.Favorites{Items: items, Groups: groups, Total: len(items)}, nil
}

func (s *Store) SetFavorite(ctx context.Context, userID, productID string, enabled bool) (domain.Favorites, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM products WHERE id=$1 OR slug=$1 OR lower(sku)=lower($1)`, productID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Favorites{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Favorites{}, err
	}
	if enabled {
		_, err = s.pool.Exec(ctx, `INSERT INTO favorites(user_id,product_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, userID, id)
	} else {
		_, err = s.pool.Exec(ctx, `DELETE FROM favorites WHERE user_id=$1 AND product_id=$2`, userID, id)
	}
	if err != nil {
		return domain.Favorites{}, fmt.Errorf("set favorite: %w", err)
	}
	return s.Favorites(ctx, userID)
}
