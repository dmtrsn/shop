package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"sweetshop/internal/domain"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}

func (s *Store) FindUserByEmail(ctx context.Context, email string) (domain.UserCredential, error) {
	var credential domain.UserCredential
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, email, phone, created_at, password_hash
		FROM users WHERE lower(email) = lower($1)`, email).Scan(
		&credential.User.ID, &credential.User.Name, &credential.User.Email, &credential.User.Phone,
		&credential.User.CreatedAt, &credential.PasswordHash,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.UserCredential{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.UserCredential{}, fmt.Errorf("find user: %w", err)
	}
	return credential, nil
}

func (s *Store) CreateSession(ctx context.Context, userID, hash string, expires time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions(token_hash, user_id, expires_at) VALUES ($1,$2,$3)
		ON CONFLICT (token_hash) DO UPDATE SET user_id=EXCLUDED.user_id, expires_at=EXCLUDED.expires_at`, hash, userID, expires)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) UserBySession(ctx context.Context, hash string, now time.Time) (domain.User, error) {
	var user domain.User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id, u.name, u.email, u.phone, u.created_at
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=$1 AND s.expires_at>$2`, hash, now).Scan(&user.ID, &user.Name, &user.Email, &user.Phone, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrUnauthorized
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("load session: %w", err)
	}
	return user, nil
}

func (s *Store) ListCategories(ctx context.Context) ([]domain.Category, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id,c.slug,c.name,c.description,COALESCE(c.parent_id,''),c.icon,
			(SELECT count(*) FROM products p JOIN categories pc ON pc.id=p.category_id WHERE pc.id=c.id OR pc.parent_id=c.id)
		FROM categories c ORDER BY c.sort_order,c.name`)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()
	result := []domain.Category{}
	for rows.Next() {
		var item domain.Category
		if err := rows.Scan(&item.ID, &item.Slug, &item.Name, &item.Description, &item.ParentID, &item.Icon, &item.ProductCount); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}
	return result, nil
}

func (s *Store) ListManufacturers(ctx context.Context) ([]domain.Manufacturer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id,m.slug,m.name,m.country,m.description,m.logo,count(p.id)
		FROM manufacturers m LEFT JOIN products p ON p.manufacturer_id=m.id
		GROUP BY m.id ORDER BY m.sort_order,m.name`)
	if err != nil {
		return nil, fmt.Errorf("list manufacturers: %w", err)
	}
	defer rows.Close()
	result := []domain.Manufacturer{}
	for rows.Next() {
		var item domain.Manufacturer
		if err := rows.Scan(&item.ID, &item.Slug, &item.Name, &item.Country, &item.Description, &item.Logo, &item.ProductCount); err != nil {
			return nil, fmt.Errorf("scan manufacturer: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListBrands(ctx context.Context) ([]domain.Brand, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.id,b.slug,b.name,b.manufacturer_id,m.name,b.description,b.logo,count(p.id)
		FROM brands b JOIN manufacturers m ON m.id=b.manufacturer_id LEFT JOIN products p ON p.brand_id=b.id
		GROUP BY b.id,m.name HAVING count(p.id)>0 ORDER BY b.sort_order,b.name`)
	if err != nil {
		return nil, fmt.Errorf("list brands: %w", err)
	}
	defer rows.Close()
	result := []domain.Brand{}
	for rows.Next() {
		var item domain.Brand
		if err := rows.Scan(&item.ID, &item.Slug, &item.Name, &item.ManufacturerID, &item.Manufacturer, &item.Description, &item.Logo, &item.ProductCount); err != nil {
			return nil, fmt.Errorf("scan brand: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListCollections(ctx context.Context) ([]domain.Collection, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id,c.slug,c.name,c.description,c.accent,c.image,c.starts_at,c.ends_at,
			COALESCE(array_agg(cp.product_id ORDER BY cp.sort_order) FILTER (WHERE cp.product_id IS NOT NULL),'{}'),count(cp.product_id)
		FROM collections c LEFT JOIN collection_products cp ON cp.collection_id=c.id
		WHERE (c.starts_at IS NULL OR c.starts_at<=now()) AND (c.ends_at IS NULL OR c.ends_at>=now())
		GROUP BY c.id ORDER BY c.sort_order,c.name`)
	if err != nil {
		return nil, fmt.Errorf("list collections: %w", err)
	}
	defer rows.Close()
	result := []domain.Collection{}
	for rows.Next() {
		var item domain.Collection
		var starts, ends *time.Time
		if err := rows.Scan(&item.ID, &item.Slug, &item.Name, &item.Description, &item.Accent, &item.Image, &starts, &ends, &item.ProductIDs, &item.ProductCount); err != nil {
			return nil, fmt.Errorf("scan collection: %w", err)
		}
		if starts != nil {
			item.StartsAt = *starts
		}
		if ends != nil {
			item.EndsAt = *ends
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

const productColumns = `
	p.id,p.slug,p.sku,p.name,p.short_name,p.description,p.price_minor,p.old_price_minor,p.currency,p.unit,
	p.weight_grams,p.image,p.gallery,p.series,p.tags,p.badges,p.ingredients,p.nutrition,p.shelf_life_days,
	p.rating,p.reviews_count,p.stock,p.minimum_quantity,p.quantity_step,p.featured,p.created_at,
	c.id,c.slug,c.name,c.description,COALESCE(c.parent_id,''),c.icon,
	m.id,m.slug,m.name,m.country,m.description,m.logo,
	b.id,b.slug,b.name,b.manufacturer_id,m.name,b.description,b.logo`

const productJoins = ` FROM products p JOIN categories c ON c.id=p.category_id JOIN manufacturers m ON m.id=p.manufacturer_id JOIN brands b ON b.id=p.brand_id `

type scanner interface{ Scan(...any) error }

func scanProduct(row scanner) (domain.Product, error) {
	var p domain.Product
	err := row.Scan(
		&p.ID, &p.Slug, &p.SKU, &p.Name, &p.ShortName, &p.Description, &p.PriceMinor, &p.OldPriceMinor, &p.Currency, &p.Unit,
		&p.WeightGrams, &p.Image, &p.Gallery, &p.Series, &p.Tags, &p.Badges, &p.Ingredients, &p.Nutrition, &p.ShelfLifeDays,
		&p.Rating, &p.ReviewsCount, &p.Stock, &p.MinimumQuantity, &p.QuantityStep, &p.Featured, &p.CreatedAt,
		&p.Category.ID, &p.Category.Slug, &p.Category.Name, &p.Category.Description, &p.Category.ParentID, &p.Category.Icon,
		&p.Manufacturer.ID, &p.Manufacturer.Slug, &p.Manufacturer.Name, &p.Manufacturer.Country, &p.Manufacturer.Description, &p.Manufacturer.Logo,
		&p.Brand.ID, &p.Brand.Slug, &p.Brand.Name, &p.Brand.ManufacturerID, &p.Brand.Manufacturer, &p.Brand.Description, &p.Brand.Logo,
	)
	if err != nil {
		return domain.Product{}, err
	}
	if p.Gallery == nil {
		p.Gallery = []string{}
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if p.Badges == nil {
		p.Badges = []string{}
	}
	return p, nil
}

func productWhere(filter domain.ProductFilter) (string, []any) {
	conditions := []string{"true"}
	args := []any{}
	add := func(value any) string { args = append(args, value); return fmt.Sprintf("$%d", len(args)) }
	if filter.Query != "" {
		p := add(filter.Query)
		like := add("%" + strings.ToLower(filter.Query) + "%")
		tag := add([]string{strings.ToLower(filter.Query)})
		conditions = append(conditions, "(p.search_vector @@ plainto_tsquery('simple',"+p+") OR p.tags @> "+tag+"::text[] OR lower(p.name) LIKE "+like+" OR lower(p.short_name) LIKE "+like+" OR lower(p.sku) LIKE "+like+" OR lower(p.series) LIKE "+like+" OR p.manufacturer_id IN (SELECT sm.id FROM manufacturers sm WHERE lower(sm.name) LIKE "+like+") OR p.brand_id IN (SELECT sb.id FROM brands sb WHERE lower(sb.name) LIKE "+like+") OR p.category_id IN (SELECT sc.id FROM categories sc WHERE lower(sc.name) LIKE "+like+"))")
	}
	if filter.Category != "" {
		p := add(filter.Category)
		conditions = append(conditions, "EXISTS(SELECT 1 FROM categories fc WHERE (fc.id="+p+" OR fc.slug="+p+") AND (p.category_id=fc.id OR c.parent_id=fc.id))")
	}
	if filter.Manufacturer != "" {
		p := add(filter.Manufacturer)
		conditions = append(conditions, "(m.id="+p+" OR m.slug="+p+")")
	}
	if filter.Brand != "" {
		p := add(filter.Brand)
		conditions = append(conditions, "(b.id="+p+" OR b.slug="+p+")")
	}
	if filter.Collection != "" {
		p := add(filter.Collection)
		conditions = append(conditions, "EXISTS(SELECT 1 FROM collection_products cp JOIN collections co ON co.id=cp.collection_id WHERE cp.product_id=p.id AND (co.id="+p+" OR co.slug="+p+"))")
	}
	if filter.Series != "" {
		p := add(strings.ToLower(filter.Series))
		conditions = append(conditions, "lower(p.series)="+p)
	}
	if len(filter.Tags) > 0 {
		p := add(filter.Tags)
		conditions = append(conditions, "p.tags @> "+p+"::text[]")
	}
	if filter.MinPriceMinor > 0 {
		p := add(filter.MinPriceMinor)
		conditions = append(conditions, "p.price_minor >= "+p)
	}
	if filter.MaxPriceMinor > 0 {
		p := add(filter.MaxPriceMinor)
		conditions = append(conditions, "p.price_minor <= "+p)
	}
	if filter.InStock != nil {
		if *filter.InStock {
			conditions = append(conditions, "p.stock>0")
		} else {
			conditions = append(conditions, "p.stock=0")
		}
	}
	if filter.OnSale {
		conditions = append(conditions, "p.old_price_minor>p.price_minor")
	}
	if filter.NewOnly {
		conditions = append(conditions, "p.badges @> ARRAY['Новинка']::text[]")
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func sortSQL(sortBy string) string {
	switch sortBy {
	case "price_asc":
		return "p.price_minor ASC,p.name ASC"
	case "price_desc":
		return "p.price_minor DESC,p.name ASC"
	case "rating":
		return "p.rating DESC,p.reviews_count DESC"
	case "name":
		return "p.name ASC"
	case "new":
		return "p.created_at DESC"
	default:
		return "p.featured DESC,p.reviews_count DESC,p.rating DESC"
	}
}

func (s *Store) queryProducts(ctx context.Context, filter domain.ProductFilter, paginate bool) ([]domain.Product, error) {
	where, args := productWhere(filter)
	query := "SELECT " + productColumns + productJoins + where + " ORDER BY " + sortSQL(filter.Sort)
	if paginate {
		limit := filter.Limit
		if limit < 1 {
			limit = 24
		}
		page := filter.Page
		if page < 1 {
			page = 1
		}
		args = append(args, limit, (page-1)*limit)
		query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query products: %w", err)
	}
	defer rows.Close()
	items := []domain.Product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate products: %w", err)
	}
	return items, nil
}

func (s *Store) ListProducts(ctx context.Context, filter domain.ProductFilter) (domain.ProductPage, error) {
	where, args := productWhere(filter)
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT count(*)"+productJoins+where, args...).Scan(&total); err != nil {
		return domain.ProductPage{}, fmt.Errorf("count products: %w", err)
	}
	items, err := s.queryProducts(ctx, filter, true)
	if err != nil {
		return domain.ProductPage{}, err
	}
	page, limit := filter.Page, filter.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 24
	}
	return domain.ProductPage{Items: items, Total: total, Page: page, Limit: limit, TotalPages: int(math.Ceil(float64(total) / float64(limit)))}, nil
}

func (s *Store) CatalogFacets(ctx context.Context, filter domain.ProductFilter) (domain.CatalogFacets, error) {
	where, args := productWhere(filter)
	result := domain.CatalogFacets{
		Categories: []domain.FacetValue{}, Manufacturers: []domain.FacetValue{},
		Brands: []domain.FacetValue{}, Tags: []domain.FacetValue{}, Series: []domain.FacetValue{},
	}
	if err := s.pool.QueryRow(ctx, "SELECT COALESCE(min(p.price_minor),0),COALESCE(max(p.price_minor),0)"+productJoins+where, args...).Scan(&result.MinPriceMinor, &result.MaxPriceMinor); err != nil {
		return domain.CatalogFacets{}, fmt.Errorf("catalog facet price range: %w", err)
	}

	queries := []struct {
		name   string
		query  string
		target *[]domain.FacetValue
	}{
		{name: "categories", query: "SELECT c.id,c.slug,c.name,count(*)" + productJoins + where + " GROUP BY c.id,c.slug,c.name ORDER BY c.name", target: &result.Categories},
		{name: "manufacturers", query: "SELECT m.id,m.slug,m.name,count(*)" + productJoins + where + " GROUP BY m.id,m.slug,m.name ORDER BY m.name", target: &result.Manufacturers},
		{name: "brands", query: "SELECT b.id,b.slug,b.name,count(*)" + productJoins + where + " GROUP BY b.id,b.slug,b.name ORDER BY b.name", target: &result.Brands},
		{name: "series", query: "SELECT p.series,p.series,p.series,count(*)" + productJoins + where + " AND p.series<>'' GROUP BY p.series ORDER BY p.series", target: &result.Series},
		{name: "tags", query: "SELECT tag.name,tag.name,tag.name,count(*)" + productJoins + " CROSS JOIN LATERAL unnest(p.tags) AS tag(name)" + where + " GROUP BY tag.name ORDER BY tag.name", target: &result.Tags},
	}
	for _, facet := range queries {
		values, err := s.queryFacetValues(ctx, facet.query, args)
		if err != nil {
			return domain.CatalogFacets{}, fmt.Errorf("catalog facet %s: %w", facet.name, err)
		}
		*facet.target = values
	}
	return result, nil
}

func (s *Store) queryFacetValues(ctx context.Context, query string, args []any) ([]domain.FacetValue, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []domain.FacetValue{}
	for rows.Next() {
		var value domain.FacetValue
		if err := rows.Scan(&value.ID, &value.Slug, &value.Name, &value.Count); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) Product(ctx context.Context, idOrSlug string) (domain.Product, error) {
	row := s.pool.QueryRow(ctx, "SELECT "+productColumns+productJoins+" WHERE p.id=$1 OR p.slug=$1 OR lower(p.sku)=lower($1) LIMIT 1", idOrSlug)
	p, err := scanProduct(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Product{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Product{}, fmt.Errorf("get product: %w", err)
	}
	return p, nil
}

func (s *Store) Suggestions(ctx context.Context, query string, limit int) ([]domain.SearchSuggestion, error) {
	page, err := s.ListProducts(ctx, domain.ProductFilter{Query: query, Page: 1, Limit: limit})
	if err != nil {
		return nil, err
	}
	result := make([]domain.SearchSuggestion, 0, limit)
	for _, p := range page.Items {
		result = append(result, domain.SearchSuggestion{Type: "product", ID: p.ID, Slug: p.Slug, Title: p.Name, Subtitle: p.Manufacturer.Name + " · " + p.Unit, Image: p.Image})
	}
	remaining := limit - len(result)
	if remaining <= 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT kind,id,slug,name,subtitle FROM (
		SELECT 'manufacturer' kind,id,slug,name,country subtitle FROM manufacturers
		UNION ALL SELECT 'brand',b.id,b.slug,b.name,m.name FROM brands b JOIN manufacturers m ON m.id=b.manufacturer_id
		UNION ALL SELECT 'category',id,slug,name,'' FROM categories) x
		WHERE lower(name) LIKE $1 ORDER BY name LIMIT $2`, "%"+strings.ToLower(query)+"%", remaining)
	if err != nil {
		return nil, fmt.Errorf("search suggestions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.SearchSuggestion
		if err := rows.Scan(&item.Type, &item.ID, &item.Slug, &item.Title, &item.Subtitle); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) Products(ctx context.Context, ids []string) (map[string]domain.Product, error) {
	if len(ids) == 0 {
		return map[string]domain.Product{}, nil
	}
	rows, err := s.pool.Query(ctx, "SELECT "+productColumns+productJoins+" WHERE p.id=ANY($1)", ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]domain.Product, len(ids))
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		result[p.ID] = p
	}
	return result, rows.Err()
}
