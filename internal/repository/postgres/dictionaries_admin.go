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

// Staff dictionary writes (ADR 018) run in InStaffMutation. Lock order: the
// actor's staff row, the dictionary advisory lock, the written row FOR NO KEY
// UPDATE, then the rows it refers to FOR SHARE. The advisory lock serializes
// every dictionary write, so tree, slug and reference checks see no
// concurrent dictionary change. Product writes take the referenced
// dictionary rows FOR SHARE (checkProductWrite), which conflicts with the
// row lock here: an archive either waits for the product write and then
// counts its reference, or commits first and the product write sees it
// archived. Neither path locks the other's rows in reverse order.

// catalogDictionaryLock serializes staff dictionary writes; nothing else takes it.
const catalogDictionaryLock = "sweetshop/catalog-dictionaries"

const (
	adminCategoryColumns = `c.id,c.revision,c.archived_at,(SELECT count(*) FROM products p WHERE p.category_id=c.id),
		c.slug,c.name,c.description,COALESCE(c.parent_id,''),c.icon,c.sort_order,
		(SELECT count(*) FROM categories ch WHERE ch.parent_id=c.id) FROM categories c`
	adminManufacturerColumns = `m.id,m.revision,m.archived_at,(SELECT count(*) FROM products p WHERE p.manufacturer_id=m.id),
		m.slug,m.name,m.country,m.description,m.logo,m.sort_order,
		(SELECT count(*) FROM brands b WHERE b.manufacturer_id=m.id) FROM manufacturers m`
	adminBrandColumns = `b.id,b.revision,b.archived_at,(SELECT count(*) FROM products p WHERE p.brand_id=b.id),
		b.slug,b.name,bm.id,bm.name,b.description,b.logo,b.sort_order
		FROM brands b JOIN manufacturers bm ON bm.id=b.manufacturer_id`
	adminCollectionColumns = `co.id,co.revision,co.archived_at,(SELECT count(*) FROM collection_products cp WHERE cp.collection_id=co.id),
		co.slug,co.name,co.description,co.accent,co.image,co.starts_at,co.ends_at,co.sort_order FROM collections co`
)

func dictionaryState(state *domain.DictionaryState, archivedAt *time.Time) {
	if archivedAt != nil {
		utc := archivedAt.UTC()
		state.Archived, state.ArchivedAt = true, &utc
	}
}

func scanAdminCategory(row scanner) (domain.AdminCategory, error) {
	var c domain.AdminCategory
	var archivedAt *time.Time
	err := row.Scan(&c.ID, &c.Revision, &archivedAt, &c.ProductCount, &c.Slug, &c.Name, &c.Description, &c.ParentID, &c.Icon, &c.SortOrder, &c.ChildCount)
	dictionaryState(&c.DictionaryState, archivedAt)
	return c, err
}

func scanAdminManufacturer(row scanner) (domain.AdminManufacturer, error) {
	var m domain.AdminManufacturer
	var archivedAt *time.Time
	err := row.Scan(&m.ID, &m.Revision, &archivedAt, &m.ProductCount, &m.Slug, &m.Name, &m.Country, &m.Description, &m.Logo, &m.SortOrder, &m.BrandCount)
	dictionaryState(&m.DictionaryState, archivedAt)
	return m, err
}

func scanAdminBrand(row scanner) (domain.AdminBrand, error) {
	var b domain.AdminBrand
	var archivedAt *time.Time
	err := row.Scan(&b.ID, &b.Revision, &archivedAt, &b.ProductCount, &b.Slug, &b.Name, &b.Manufacturer.ID, &b.Manufacturer.Name, &b.Description, &b.Logo, &b.SortOrder)
	dictionaryState(&b.DictionaryState, archivedAt)
	return b, err
}

func scanAdminCollection(row scanner) (domain.AdminCollection, error) {
	var c domain.AdminCollection
	var archivedAt *time.Time
	err := row.Scan(&c.ID, &c.Revision, &archivedAt, &c.ProductCount, &c.Slug, &c.Name, &c.Description, &c.Accent, &c.Image, &c.StartsAt, &c.EndsAt, &c.SortOrder)
	dictionaryState(&c.DictionaryState, archivedAt)
	for _, at := range []**time.Time{&c.StartsAt, &c.EndsAt} {
		if *at != nil {
			utc := (*at).UTC()
			*at = &utc
		}
	}
	return c, err
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// listDictionary reads one dictionary in storefront order; alias is the
// table alias of columns.
func listDictionary[T any](ctx context.Context, q queryer, name, columns, alias string, status domain.DictionaryStatus, scan func(scanner) (T, error)) ([]T, error) {
	where := ""
	switch status {
	case domain.DictionaryActive:
		where = " WHERE " + alias + ".archived_at IS NULL"
	case domain.DictionaryArchived:
		where = " WHERE " + alias + ".archived_at IS NOT NULL"
	case domain.DictionaryAll:
	default:
		return nil, fmt.Errorf("admin %s: invalid status %q", name, status)
	}
	rows, err := q.Query(ctx, "SELECT "+columns+where+" ORDER BY "+alias+".sort_order,"+alias+".name,"+alias+".id")
	if err != nil {
		return nil, fmt.Errorf("list admin %s: %w", name, err)
	}
	defer rows.Close()
	items := []T{}
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan admin %s: %w", name, err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin %s: %w", name, err)
	}
	return items, nil
}

func readDictionary[T any](ctx context.Context, q rowQuerier, name, columns, alias, id string, scan func(scanner) (T, error)) (T, error) {
	item, err := scan(q.QueryRow(ctx, "SELECT "+columns+" WHERE "+alias+".id=$1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, domain.ErrNotFound
	}
	if err != nil {
		return item, fmt.Errorf("load admin %s: %w", name, err)
	}
	return item, nil
}

func (s *Store) AdminCategories(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminCategory, error) {
	return listDictionary(ctx, s.pool, "categories", adminCategoryColumns, "c", status, scanAdminCategory)
}

func (s *Store) AdminCategory(ctx context.Context, id string) (domain.AdminCategory, error) {
	return loadCategory(ctx, s.pool, id)
}

func (s *Store) AdminManufacturers(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminManufacturer, error) {
	return listDictionary(ctx, s.pool, "manufacturers", adminManufacturerColumns, "m", status, scanAdminManufacturer)
}

func (s *Store) AdminManufacturer(ctx context.Context, id string) (domain.AdminManufacturer, error) {
	return loadManufacturer(ctx, s.pool, id)
}

func (s *Store) AdminBrands(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminBrand, error) {
	return listDictionary(ctx, s.pool, "brands", adminBrandColumns, "b", status, scanAdminBrand)
}

func (s *Store) AdminBrand(ctx context.Context, id string) (domain.AdminBrand, error) {
	return loadBrand(ctx, s.pool, id)
}

func (s *Store) AdminCollections(ctx context.Context, status domain.DictionaryStatus) ([]domain.AdminCollection, error) {
	return listDictionary(ctx, s.pool, "collections", adminCollectionColumns, "co", status, scanAdminCollection)
}

func (s *Store) AdminCollection(ctx context.Context, id string) (domain.AdminCollection, error) {
	return loadCollection(ctx, s.pool, id)
}

// dictionaryTable names one dictionary for the shared write steps. The table
// name is a constant from this file, never input.
type dictionaryTable struct {
	table, target, slugTaken string
}

var (
	categoriesTable    = dictionaryTable{"categories", "category", "Slug уже используется другой категорией"}
	manufacturersTable = dictionaryTable{"manufacturers", "manufacturer", "Slug уже используется другим производителем"}
	brandsTable        = dictionaryTable{"brands", "brand", "Slug уже используется другим брендом"}
	collectionsTable   = dictionaryTable{"collections", "collection", "Slug уже используется другой подборкой"}
)

// dictionaryMutation runs work as one audited staff write under the
// dictionary lock and reloads the row in the same transaction.
func dictionaryMutation[T any](ctx context.Context, s *Store, mutation domain.StaffMutation, t dictionaryTable, id string,
	work func(context.Context, StaffTx) error, load func(context.Context, rowQuerier, string) (T, error)) (T, error) {
	var result T
	_, err := s.InStaffMutation(ctx, mutation, func(ctx context.Context, tx StaffTx) (domain.AuditTarget, error) {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, catalogDictionaryLock); err != nil {
			return domain.AuditTarget{}, fmt.Errorf("lock catalog dictionaries: %w", err)
		}
		if err := work(ctx, tx); err != nil {
			return domain.AuditTarget{}, err
		}
		var err error
		if result, err = load(ctx, tx, id); err != nil {
			return domain.AuditTarget{}, err
		}
		return domain.AuditTarget{Type: t.target, ID: id}, nil
	})
	return result, err
}

// lock takes the row for writing and requires the expected revision; it
// reports whether the row is archived.
func (t dictionaryTable) lock(ctx context.Context, tx StaffTx, id string, expectedRevision int64) (bool, error) {
	var revision int64
	var archived bool
	err := tx.QueryRow(ctx, `SELECT revision,archived_at IS NOT NULL FROM `+t.table+` WHERE id=$1 FOR NO KEY UPDATE`, id).Scan(&revision, &archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, domain.ErrNotFound
	}
	if err != nil {
		return false, fmt.Errorf("lock %s: %w", t.target, err)
	}
	if revision != expectedRevision {
		return false, domain.ErrStaleRevision
	}
	return archived, nil
}

// checkSlug: customer filters accept a dictionary ID or slug, so a slug may
// equal no other row's slug or ID in the same dictionary.
func (t dictionaryTable) checkSlug(ctx context.Context, tx StaffTx, id, slug string) error {
	var taken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+t.table+` WHERE id<>$1 AND (slug=$2 OR id=$2))`, id, slug).Scan(&taken); err != nil {
		return fmt.Errorf("check %s slug: %w", t.target, err)
	}
	if taken {
		return &domain.ValidationError{Fields: []domain.FieldError{{Field: "slug", Message: t.slugTaken}}}
	}
	return nil
}

func (t dictionaryTable) writeError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == t.table+"_slug_key" {
		return &domain.ValidationError{Fields: []domain.FieldError{{Field: "slug", Message: t.slugTaken}}}
	}
	return fmt.Errorf("write %s: %w", t.target, err)
}

func (t dictionaryTable) setArchived(ctx context.Context, tx StaffTx, id string, archived bool) error {
	if _, err := tx.Exec(ctx, `UPDATE `+t.table+` SET archived_at=CASE WHEN $2 THEN now() END,revision=revision+1 WHERE id=$1`, id, archived); err != nil {
		return fmt.Errorf("archive %s: %w", t.target, err)
	}
	return nil
}

// lockReference takes a referenced dictionary row FOR SHARE and reports
// whether it exists and is archived; extra columns are scanned into into.
func lockReference(ctx context.Context, tx StaffTx, table, columns, id string, into ...any) (bool, bool, error) {
	var archived bool
	err := tx.QueryRow(ctx, `SELECT archived_at IS NOT NULL`+columns+` FROM `+table+` WHERE id=$1 FOR SHARE`, id).Scan(append([]any{&archived}, into...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("lock %s reference: %w", table, err)
	}
	return true, archived, nil
}

func fieldError(field, message string) error {
	return &domain.ValidationError{Fields: []domain.FieldError{{Field: field, Message: message}}}
}

// --- categories -----------------------------------------------------------

func loadCategory(ctx context.Context, q rowQuerier, id string) (domain.AdminCategory, error) {
	return readDictionary(ctx, q, "category", adminCategoryColumns, "c", id, scanAdminCategory)
}

// checkCategoryParent enforces the two-level tree: the parent exists, is
// top-level, and is active unless it is the category's current parent.
func checkCategoryParent(ctx context.Context, tx StaffTx, parentID, currentParentID string) error {
	if parentID == "" {
		return nil
	}
	var grandparent string
	exists, archived, err := lockReference(ctx, tx, "categories", ",COALESCE(parent_id,'')", parentID, &grandparent)
	switch {
	case err != nil:
		return err
	case !exists:
		return fieldError("parentId", "Категория не найдена")
	case grandparent != "":
		return domain.ErrInvalidHierarchy
	case archived && parentID != currentParentID:
		return fieldError("parentId", "Родительская категория в архиве")
	}
	return nil
}

func (s *Store) CreateCategory(ctx context.Context, mutation domain.StaffMutation, id string, in domain.CategoryInput) (domain.AdminCategory, error) {
	t := categoriesTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		if err := t.checkSlug(ctx, tx, id, in.Slug); err != nil {
			return err
		}
		if err := checkCategoryParent(ctx, tx, in.ParentID, ""); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO categories(id,slug,name,description,parent_id,icon,sort_order) VALUES ($1,$2,$3,$4,NULLIF($5,''),$6,$7)`,
			id, in.Slug, in.Name, in.Description, in.ParentID, in.Icon, in.SortOrder)
		if err != nil {
			return t.writeError(err)
		}
		return nil
	}, loadCategory)
}

func (s *Store) UpdateCategory(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, in domain.CategoryInput) (domain.AdminCategory, error) {
	t := categoriesTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		if _, err := t.lock(ctx, tx, id, expectedRevision); err != nil {
			return err
		}
		if in.ParentID == id {
			return domain.ErrInvalidHierarchy
		}
		var currentParent string
		var children int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(parent_id,''),(SELECT count(*) FROM categories WHERE parent_id=$1) FROM categories WHERE id=$1`, id).
			Scan(&currentParent, &children); err != nil {
			return fmt.Errorf("read category tree: %w", err)
		}
		// Any subcategory, archived or not, keeps a category top-level, so a
		// restored subcategory can never become a third level.
		if in.ParentID != "" && children > 0 {
			return domain.ErrInvalidHierarchy
		}
		if err := t.checkSlug(ctx, tx, id, in.Slug); err != nil {
			return err
		}
		if err := checkCategoryParent(ctx, tx, in.ParentID, currentParent); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE categories SET slug=$2,name=$3,description=$4,parent_id=NULLIF($5,''),icon=$6,sort_order=$7,revision=revision+1 WHERE id=$1`,
			id, in.Slug, in.Name, in.Description, in.ParentID, in.Icon, in.SortOrder)
		if err != nil {
			return t.writeError(err)
		}
		return nil
	}, loadCategory)
}

func (s *Store) SetCategoryArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminCategory, error) {
	t := categoriesTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		current, err := t.lock(ctx, tx, id, expectedRevision)
		if err != nil {
			return err
		}
		if current == archived {
			return domain.ErrConflict
		}
		if archived {
			var conflict domain.ReferenceConflictError
			if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM products WHERE category_id=$1),
				(SELECT count(*) FROM categories WHERE parent_id=$1 AND archived_at IS NULL)`, id).Scan(&conflict.Products, &conflict.Categories); err != nil {
				return fmt.Errorf("count category references: %w", err)
			}
			if conflict.Products > 0 || conflict.Categories > 0 {
				return &conflict
			}
		} else {
			var parent string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(parent_id,'') FROM categories WHERE id=$1`, id).Scan(&parent); err != nil {
				return fmt.Errorf("read category parent: %w", err)
			}
			if parent != "" {
				if _, parentArchived, err := lockReference(ctx, tx, "categories", "", parent); err != nil {
					return err
				} else if parentArchived {
					return fieldError("parentId", "Сначала восстановите родительскую категорию")
				}
			}
		}
		return t.setArchived(ctx, tx, id, archived)
	}, loadCategory)
}

// --- manufacturers --------------------------------------------------------

func loadManufacturer(ctx context.Context, q rowQuerier, id string) (domain.AdminManufacturer, error) {
	return readDictionary(ctx, q, "manufacturer", adminManufacturerColumns, "m", id, scanAdminManufacturer)
}

func (s *Store) CreateManufacturer(ctx context.Context, mutation domain.StaffMutation, id string, in domain.ManufacturerInput) (domain.AdminManufacturer, error) {
	t := manufacturersTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		if err := t.checkSlug(ctx, tx, id, in.Slug); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO manufacturers(id,slug,name,country,description,sort_order) VALUES ($1,$2,$3,$4,$5,$6)`,
			id, in.Slug, in.Name, in.Country, in.Description, in.SortOrder)
		if err != nil {
			return t.writeError(err)
		}
		return nil
	}, loadManufacturer)
}

func (s *Store) UpdateManufacturer(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, in domain.ManufacturerInput) (domain.AdminManufacturer, error) {
	t := manufacturersTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		if _, err := t.lock(ctx, tx, id, expectedRevision); err != nil {
			return err
		}
		if err := t.checkSlug(ctx, tx, id, in.Slug); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE manufacturers SET slug=$2,name=$3,country=$4,description=$5,sort_order=$6,revision=revision+1 WHERE id=$1`,
			id, in.Slug, in.Name, in.Country, in.Description, in.SortOrder)
		if err != nil {
			return t.writeError(err)
		}
		return nil
	}, loadManufacturer)
}

func (s *Store) SetManufacturerArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminManufacturer, error) {
	t := manufacturersTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		current, err := t.lock(ctx, tx, id, expectedRevision)
		if err != nil {
			return err
		}
		if current == archived {
			return domain.ErrConflict
		}
		if archived {
			var conflict domain.ReferenceConflictError
			if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM products WHERE manufacturer_id=$1),
				(SELECT count(*) FROM brands WHERE manufacturer_id=$1 AND archived_at IS NULL)`, id).Scan(&conflict.Products, &conflict.Brands); err != nil {
				return fmt.Errorf("count manufacturer references: %w", err)
			}
			if conflict.Products > 0 || conflict.Brands > 0 {
				return &conflict
			}
		}
		return t.setArchived(ctx, tx, id, archived)
	}, loadManufacturer)
}

// --- brands ---------------------------------------------------------------

func loadBrand(ctx context.Context, q rowQuerier, id string) (domain.AdminBrand, error) {
	return readDictionary(ctx, q, "brand", adminBrandColumns, "b", id, scanAdminBrand)
}

// checkBrandManufacturer: the manufacturer exists and is active unless it is
// the brand's current one.
func checkBrandManufacturer(ctx context.Context, tx StaffTx, manufacturerID, currentID string) error {
	exists, archived, err := lockReference(ctx, tx, "manufacturers", "", manufacturerID)
	switch {
	case err != nil:
		return err
	case !exists:
		return fieldError("manufacturerId", "Производитель не найден")
	case archived && manufacturerID != currentID:
		return fieldError("manufacturerId", "Производитель в архиве")
	}
	return nil
}

func (s *Store) CreateBrand(ctx context.Context, mutation domain.StaffMutation, id string, in domain.BrandInput) (domain.AdminBrand, error) {
	t := brandsTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		if err := t.checkSlug(ctx, tx, id, in.Slug); err != nil {
			return err
		}
		if err := checkBrandManufacturer(ctx, tx, in.ManufacturerID, ""); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO brands(id,slug,name,manufacturer_id,description,sort_order) VALUES ($1,$2,$3,$4,$5,$6)`,
			id, in.Slug, in.Name, in.ManufacturerID, in.Description, in.SortOrder)
		if err != nil {
			return t.writeError(err)
		}
		return nil
	}, loadBrand)
}

func (s *Store) UpdateBrand(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, in domain.BrandInput) (domain.AdminBrand, error) {
	t := brandsTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		if _, err := t.lock(ctx, tx, id, expectedRevision); err != nil {
			return err
		}
		var currentManufacturer string
		var products int
		if err := tx.QueryRow(ctx, `SELECT manufacturer_id,(SELECT count(*) FROM products WHERE brand_id=$1) FROM brands WHERE id=$1`, id).
			Scan(&currentManufacturer, &products); err != nil {
			return fmt.Errorf("read brand manufacturer: %w", err)
		}
		// Products pair their brand with its manufacturer (migration 004), so
		// a used brand keeps its manufacturer.
		if in.ManufacturerID != currentManufacturer && products > 0 {
			return &domain.ReferenceConflictError{Products: products}
		}
		if err := t.checkSlug(ctx, tx, id, in.Slug); err != nil {
			return err
		}
		if err := checkBrandManufacturer(ctx, tx, in.ManufacturerID, currentManufacturer); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE brands SET slug=$2,name=$3,manufacturer_id=$4,description=$5,sort_order=$6,revision=revision+1 WHERE id=$1`,
			id, in.Slug, in.Name, in.ManufacturerID, in.Description, in.SortOrder)
		if err != nil {
			return t.writeError(err)
		}
		return nil
	}, loadBrand)
}

func (s *Store) SetBrandArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminBrand, error) {
	t := brandsTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		current, err := t.lock(ctx, tx, id, expectedRevision)
		if err != nil {
			return err
		}
		if current == archived {
			return domain.ErrConflict
		}
		if archived {
			var conflict domain.ReferenceConflictError
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM products WHERE brand_id=$1`, id).Scan(&conflict.Products); err != nil {
				return fmt.Errorf("count brand references: %w", err)
			}
			if conflict.Products > 0 {
				return &conflict
			}
		} else {
			var manufacturer string
			if err := tx.QueryRow(ctx, `SELECT manufacturer_id FROM brands WHERE id=$1`, id).Scan(&manufacturer); err != nil {
				return fmt.Errorf("read brand manufacturer: %w", err)
			}
			if _, manufacturerArchived, err := lockReference(ctx, tx, "manufacturers", "", manufacturer); err != nil {
				return err
			} else if manufacturerArchived {
				return fieldError("manufacturerId", "Сначала восстановите производителя")
			}
		}
		return t.setArchived(ctx, tx, id, archived)
	}, loadBrand)
}

// --- collections ----------------------------------------------------------

func loadCollection(ctx context.Context, q rowQuerier, id string) (domain.AdminCollection, error) {
	return readDictionary(ctx, q, "collection", adminCollectionColumns, "co", id, scanAdminCollection)
}

func (s *Store) CreateCollection(ctx context.Context, mutation domain.StaffMutation, id string, in domain.CollectionInput) (domain.AdminCollection, error) {
	t := collectionsTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		if err := t.checkSlug(ctx, tx, id, in.Slug); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO collections(id,slug,name,description,accent,starts_at,ends_at,sort_order) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			id, in.Slug, in.Name, in.Description, in.Accent, in.StartsAt, in.EndsAt, in.SortOrder)
		if err != nil {
			return t.writeError(err)
		}
		return nil
	}, loadCollection)
}

func (s *Store) UpdateCollection(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, in domain.CollectionInput) (domain.AdminCollection, error) {
	t := collectionsTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		if _, err := t.lock(ctx, tx, id, expectedRevision); err != nil {
			return err
		}
		if err := t.checkSlug(ctx, tx, id, in.Slug); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE collections SET slug=$2,name=$3,description=$4,accent=$5,starts_at=$6,ends_at=$7,sort_order=$8,revision=revision+1 WHERE id=$1`,
			id, in.Slug, in.Name, in.Description, in.Accent, in.StartsAt, in.EndsAt, in.SortOrder)
		if err != nil {
			return t.writeError(err)
		}
		return nil
	}, loadCollection)
}

// SetCollectionArchived keeps memberships: an archived collection only
// leaves the storefront and new product assignments.
func (s *Store) SetCollectionArchived(ctx context.Context, mutation domain.StaffMutation, id string, expectedRevision int64, archived bool) (domain.AdminCollection, error) {
	t := collectionsTable
	return dictionaryMutation(ctx, s, mutation, t, id, func(ctx context.Context, tx StaffTx) error {
		current, err := t.lock(ctx, tx, id, expectedRevision)
		if err != nil {
			return err
		}
		if current == archived {
			return domain.ErrConflict
		}
		return t.setArchived(ctx, tx, id, archived)
	}, loadCollection)
}
