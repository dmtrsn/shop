package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"sweetshop/internal/domain"
)

// Staff product writes (ADR 013) run in InStaffMutation, so each commits
// together with its audit event. Lock order: the actor's staff row (taken by
// InStaffMutation), then the product row FOR UPDATE, then the product
// identifier advisory lock. Checkout locks the customer row and then product
// rows; neither path takes the other's first lock, so no cycle can form.

// productIdentifierLock serializes staff writes of slugs and SKUs, so the
// cross-column uniqueness check below cannot race; nothing else takes it.
const productIdentifierLock = "sweetshop/product-identifiers"

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

const adminProductColumns = `
	p.id,p.revision,p.archived_at,p.slug,p.sku,p.name,p.short_name,p.description,p.price_minor,p.old_price_minor,
	p.currency,p.unit,p.weight_grams,p.image,p.gallery,c.id,c.name,m.id,m.name,b.id,b.name,
	COALESCE((SELECT array_agg(co.id ORDER BY co.sort_order,co.name,co.id) FROM collection_products cp JOIN collections co ON co.id=cp.collection_id WHERE cp.product_id=p.id),'{}'),
	COALESCE((SELECT array_agg(co.name ORDER BY co.sort_order,co.name,co.id) FROM collection_products cp JOIN collections co ON co.id=cp.collection_id WHERE cp.product_id=p.id),'{}'),
	p.series,p.tags,p.badges,p.ingredients,p.nutrition,p.shelf_life_days,p.rating,p.reviews_count,p.stock,
	p.minimum_quantity,p.quantity_step,p.featured,p.created_at,p.updated_at`

func scanAdminProduct(row scanner) (domain.AdminProduct, error) {
	var p domain.AdminProduct
	var archived *time.Time
	var collectionIDs, collectionNames []string
	err := row.Scan(&p.ID, &p.Revision, &archived, &p.Slug, &p.SKU, &p.Name, &p.ShortName, &p.Description, &p.PriceMinor, &p.OldPriceMinor,
		&p.Currency, &p.Unit, &p.WeightGrams, &p.Image, &p.Gallery, &p.Category.ID, &p.Category.Name, &p.Manufacturer.ID, &p.Manufacturer.Name,
		&p.Brand.ID, &p.Brand.Name, &collectionIDs, &collectionNames, &p.Series, &p.Tags, &p.Badges, &p.Ingredients, &p.Nutrition,
		&p.ShelfLifeDays, &p.Rating, &p.ReviewsCount, &p.Stock, &p.MinimumQuantity, &p.QuantityStep, &p.Featured, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return domain.AdminProduct{}, err
	}
	if archived != nil {
		utc := archived.UTC()
		p.Archived, p.ArchivedAt = true, &utc
	}
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	p.Collections = make([]domain.Reference, len(collectionIDs))
	for i := range collectionIDs {
		p.Collections[i] = domain.Reference{ID: collectionIDs[i], Name: collectionNames[i]}
	}
	for _, list := range []*[]string{&p.Gallery, &p.Tags, &p.Badges} {
		if *list == nil {
			*list = []string{}
		}
	}
	return p, nil
}

func adminProduct(ctx context.Context, q rowQuerier, id string) (domain.AdminProduct, error) {
	p, err := scanAdminProduct(q.QueryRow(ctx, "SELECT "+adminProductColumns+productJoins+" WHERE p.id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.AdminProduct{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.AdminProduct{}, fmt.Errorf("load admin product: %w", err)
	}
	return p, nil
}

func (s *Store) AdminProduct(ctx context.Context, id string) (domain.AdminProduct, error) {
	return adminProduct(ctx, s.pool, id)
}

func (s *Store) AdminProducts(ctx context.Context, filter domain.AdminProductFilter) (domain.AdminProductPage, error) {
	conditions := []string{}
	args := []any{}
	switch filter.Status {
	case domain.AdminProductsActive:
		conditions = append(conditions, "p.archived_at IS NULL")
	case domain.AdminProductsArchived:
		conditions = append(conditions, "p.archived_at IS NOT NULL")
	case domain.AdminProductsAll:
	default:
		return domain.AdminProductPage{}, fmt.Errorf("admin products: invalid status %q", filter.Status)
	}
	if filter.Query != "" {
		args = append(args, filter.Query, "%"+escapeLike(strings.ToLower(filter.Query))+"%")
		conditions = append(conditions, "(p.id=$1 OR lower(p.sku)=lower($1) OR lower(p.name) LIKE $2 OR p.slug LIKE $2 OR lower(p.sku) LIKE $2)")
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT count(*)"+productJoins+where, args...).Scan(&total); err != nil {
		return domain.AdminProductPage{}, fmt.Errorf("count admin products: %w", err)
	}
	args = append(args, filter.Limit, (filter.Page-1)*filter.Limit)
	rows, err := s.pool.Query(ctx, "SELECT "+adminProductColumns+productJoins+where+
		fmt.Sprintf(" ORDER BY p.name,p.id LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return domain.AdminProductPage{}, fmt.Errorf("list admin products: %w", err)
	}
	defer rows.Close()
	page := domain.AdminProductPage{Items: []domain.AdminProduct{}, Total: total, Page: filter.Page, Limit: filter.Limit,
		TotalPages: int(math.Ceil(float64(total) / float64(filter.Limit)))}
	for rows.Next() {
		p, err := scanAdminProduct(rows)
		if err != nil {
			return domain.AdminProductPage{}, fmt.Errorf("scan admin product: %w", err)
		}
		page.Items = append(page.Items, p)
	}
	if err := rows.Err(); err != nil {
		return domain.AdminProductPage{}, fmt.Errorf("iterate admin products: %w", err)
	}
	return page, nil
}

func escapeLike(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}

func (s *Store) CreateProduct(ctx context.Context, mutation domain.StaffMutation, product domain.NewProduct) (domain.AdminProduct, error) {
	var result domain.AdminProduct
	_, err := s.InStaffMutation(ctx, mutation, func(ctx context.Context, tx StaffTx) (domain.AuditTarget, error) {
		in := product.Input
		if err := checkProductWrite(ctx, tx, product.ID, in); err != nil {
			return domain.AuditTarget{}, err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO products(id,slug,sku,name,short_name,description,price_minor,old_price_minor,unit,weight_grams,
				category_id,manufacturer_id,brand_id,series,tags,badges,ingredients,nutrition,shelf_life_days,stock,
				minimum_quantity,quantity_step,featured)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
			product.ID, in.Slug, in.SKU, in.Name, in.ShortName, in.Description, in.PriceMinor, in.OldPriceMinor, in.Unit, in.WeightGrams,
			in.CategoryID, in.ManufacturerID, in.BrandID, in.Series, in.Tags, in.Badges, in.Ingredients, in.Nutrition, in.ShelfLifeDays, in.Stock,
			in.MinimumQuantity, in.QuantityStep, in.Featured)
		if err != nil {
			return domain.AuditTarget{}, productWriteError("create product", err)
		}
		if err := setProductCollections(ctx, tx, product.ID, in.CollectionIDs); err != nil {
			return domain.AuditTarget{}, err
		}
		if result, err = adminProduct(ctx, tx, product.ID); err != nil {
			return domain.AuditTarget{}, err
		}
		return domain.AuditTarget{Type: "product", ID: product.ID}, nil
	})
	if err != nil {
		return domain.AdminProduct{}, err
	}
	return result, nil
}

func (s *Store) UpdateProduct(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, in domain.ProductInput) (domain.AdminProduct, error) {
	var result domain.AdminProduct
	_, err := s.InStaffMutation(ctx, mutation, func(ctx context.Context, tx StaffTx) (domain.AuditTarget, error) {
		if _, err := lockProductRevision(ctx, tx, id, expectedRevision); err != nil {
			return domain.AuditTarget{}, err
		}
		if err := checkProductWrite(ctx, tx, id, in); err != nil {
			return domain.AuditTarget{}, err
		}
		_, err := tx.Exec(ctx, `
			UPDATE products SET slug=$2,sku=$3,name=$4,short_name=$5,description=$6,price_minor=$7,old_price_minor=$8,unit=$9,
				weight_grams=$10,category_id=$11,manufacturer_id=$12,brand_id=$13,series=$14,tags=$15,badges=$16,ingredients=$17,
				nutrition=$18,shelf_life_days=$19,stock=$20,minimum_quantity=$21,quantity_step=$22,featured=$23,
				revision=revision+1,updated_at=now()
			WHERE id=$1`,
			id, in.Slug, in.SKU, in.Name, in.ShortName, in.Description, in.PriceMinor, in.OldPriceMinor, in.Unit,
			in.WeightGrams, in.CategoryID, in.ManufacturerID, in.BrandID, in.Series, in.Tags, in.Badges, in.Ingredients,
			in.Nutrition, in.ShelfLifeDays, in.Stock, in.MinimumQuantity, in.QuantityStep, in.Featured)
		if err != nil {
			return domain.AuditTarget{}, productWriteError("update product", err)
		}
		if err := setProductCollections(ctx, tx, id, in.CollectionIDs); err != nil {
			return domain.AuditTarget{}, err
		}
		if result, err = adminProduct(ctx, tx, id); err != nil {
			return domain.AuditTarget{}, err
		}
		return domain.AuditTarget{Type: "product", ID: id}, nil
	})
	if err != nil {
		return domain.AdminProduct{}, err
	}
	return result, nil
}

func (s *Store) SetProductArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminProduct, error) {
	var result domain.AdminProduct
	_, err := s.InStaffMutation(ctx, mutation, func(ctx context.Context, tx StaffTx) (domain.AuditTarget, error) {
		current, err := lockProductRevision(ctx, tx, id, expectedRevision)
		if err != nil {
			return domain.AuditTarget{}, err
		}
		if current == archived {
			return domain.AuditTarget{}, domain.ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE products SET archived_at=CASE WHEN $2 THEN now() END,revision=revision+1,updated_at=now() WHERE id=$1`, id, archived); err != nil {
			return domain.AuditTarget{}, fmt.Errorf("archive product: %w", err)
		}
		if result, err = adminProduct(ctx, tx, id); err != nil {
			return domain.AuditTarget{}, err
		}
		return domain.AuditTarget{Type: "product", ID: id}, nil
	})
	if err != nil {
		return domain.AdminProduct{}, err
	}
	return result, nil
}

// lockProductRevision locks the product row (the mode checkout takes) and
// requires the expected revision; it reports whether the product is archived.
func lockProductRevision(ctx context.Context, tx StaffTx, id string, expectedRevision int64) (bool, error) {
	var revision int64
	var archived bool
	err := tx.QueryRow(ctx, `SELECT revision,archived_at IS NOT NULL FROM products WHERE id=$1 FOR UPDATE`, id).Scan(&revision, &archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, domain.ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("lock product: %w", err)
	}
	if revision != expectedRevision {
		return false, domain.ErrStaleRevision
	}
	return archived, nil
}

// checkProductWrite verifies references and identifiers before the write, so
// the caller gets field errors instead of constraint violations. The
// brand/manufacturer foreign key is deferred to commit; checking it here
// keeps it from surfacing as a commit failure. Customer product lookups
// accept an ID, a slug or a case-insensitive SKU, so none of them may equal
// another product's identifier of any kind.
func checkProductWrite(ctx context.Context, tx StaffTx, id string, in domain.ProductInput) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, productIdentifierLock); err != nil {
		return fmt.Errorf("lock product identifiers: %w", err)
	}
	var category, manufacturer, brand, brandOwned, slugTaken, skuTaken bool
	var collections int
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM categories WHERE id=$1),
			EXISTS(SELECT 1 FROM manufacturers WHERE id=$2),
			EXISTS(SELECT 1 FROM brands WHERE id=$3),
			EXISTS(SELECT 1 FROM brands WHERE id=$3 AND manufacturer_id=$2),
			(SELECT count(*) FROM collections WHERE id=ANY($4)),
			EXISTS(SELECT 1 FROM products WHERE id<>$5 AND (slug=$6 OR id=$6 OR lower(sku)=lower($6))),
			EXISTS(SELECT 1 FROM products WHERE id<>$5 AND (lower(sku)=lower($7) OR lower(slug)=lower($7) OR lower(id)=lower($7)))`,
		in.CategoryID, in.ManufacturerID, in.BrandID, in.CollectionIDs, id, in.Slug, in.SKU).Scan(
		&category, &manufacturer, &brand, &brandOwned, &collections, &slugTaken, &skuTaken)
	if err != nil {
		return fmt.Errorf("check product references: %w", err)
	}
	fields := []domain.FieldError{}
	if !category {
		fields = append(fields, domain.FieldError{Field: "categoryId", Message: "Категория не найдена"})
	}
	if !manufacturer {
		fields = append(fields, domain.FieldError{Field: "manufacturerId", Message: "Производитель не найден"})
	}
	if !brand {
		fields = append(fields, domain.FieldError{Field: "brandId", Message: "Бренд не найден"})
	} else if manufacturer && !brandOwned {
		fields = append(fields, domain.FieldError{Field: "brandId", Message: "Бренд принадлежит другому производителю"})
	}
	if collections != len(in.CollectionIDs) {
		fields = append(fields, domain.FieldError{Field: "collectionIds", Message: "Подборка не найдена"})
	}
	if slugTaken {
		fields = append(fields, domain.FieldError{Field: "slug", Message: "Slug уже используется другим товаром"})
	}
	if skuTaken {
		fields = append(fields, domain.FieldError{Field: "sku", Message: "Артикул уже используется другим товаром"})
	}
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	return nil
}

// productWriteError maps the constraint violations checkProductWrite rules
// out (a write that races past it still fails safely) to field errors.
func productWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23505" && pgErr.ConstraintName == "products_slug_key":
			return &domain.ValidationError{Fields: []domain.FieldError{{Field: "slug", Message: "Slug уже используется другим товаром"}}}
		case pgErr.Code == "23505" && (pgErr.ConstraintName == "products_sku_key" || pgErr.ConstraintName == "products_sku_lower_uidx"):
			return &domain.ValidationError{Fields: []domain.FieldError{{Field: "sku", Message: "Артикул уже используется другим товаром"}}}
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// setProductCollections makes the product's memberships exactly ids: removed
// memberships are deleted, kept ones keep their position, and new ones are
// appended to the end of their collection.
func setProductCollections(ctx context.Context, tx StaffTx, id string, ids []string) error {
	if ids == nil {
		ids = []string{}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM collection_products WHERE product_id=$1 AND NOT (collection_id=ANY($2))`, id, ids); err != nil {
		return fmt.Errorf("remove product collections: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO collection_products(collection_id,product_id,sort_order)
		SELECT c.id,$1,COALESCE((SELECT max(sort_order)+1 FROM collection_products WHERE collection_id=c.id),0)
		FROM unnest($2::text[]) AS c(id)
		ON CONFLICT (collection_id,product_id) DO NOTHING`, id, ids); err != nil {
		return fmt.Errorf("add product collections: %w", err)
	}
	return nil
}

// AdminCatalogOptions reads the dictionaries in their storefront order. It is
// a plain read: nothing here locks, and every write rechecks its references.
func (s *Store) AdminCatalogOptions(ctx context.Context) (domain.CatalogOptions, error) {
	options := domain.CatalogOptions{
		Categories: []domain.CategoryOption{}, Manufacturers: []domain.Reference{},
		Brands: []domain.BrandOption{}, Collections: []domain.Reference{},
	}
	queries := []struct {
		name string
		sql  string
		scan func(pgx.Rows) error
	}{
		{"categories", `SELECT id,name,COALESCE(parent_id,'') FROM categories ORDER BY sort_order,name,id`, func(rows pgx.Rows) error {
			var c domain.CategoryOption
			if err := rows.Scan(&c.ID, &c.Name, &c.ParentID); err != nil {
				return err
			}
			options.Categories = append(options.Categories, c)
			return nil
		}},
		{"manufacturers", `SELECT id,name FROM manufacturers ORDER BY sort_order,name,id`, func(rows pgx.Rows) error {
			var m domain.Reference
			if err := rows.Scan(&m.ID, &m.Name); err != nil {
				return err
			}
			options.Manufacturers = append(options.Manufacturers, m)
			return nil
		}},
		{"brands", `SELECT id,name,manufacturer_id FROM brands ORDER BY sort_order,name,id`, func(rows pgx.Rows) error {
			var b domain.BrandOption
			if err := rows.Scan(&b.ID, &b.Name, &b.ManufacturerID); err != nil {
				return err
			}
			options.Brands = append(options.Brands, b)
			return nil
		}},
		{"collections", `SELECT id,name FROM collections ORDER BY sort_order,name,id`, func(rows pgx.Rows) error {
			var c domain.Reference
			if err := rows.Scan(&c.ID, &c.Name); err != nil {
				return err
			}
			options.Collections = append(options.Collections, c)
			return nil
		}},
	}
	for _, query := range queries {
		rows, err := s.pool.Query(ctx, query.sql)
		if err != nil {
			return domain.CatalogOptions{}, fmt.Errorf("list %s: %w", query.name, err)
		}
		for rows.Next() {
			if err := query.scan(rows); err != nil {
				rows.Close()
				return domain.CatalogOptions{}, fmt.Errorf("scan %s: %w", query.name, err)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return domain.CatalogOptions{}, fmt.Errorf("iterate %s: %w", query.name, err)
		}
	}
	return options, nil
}
