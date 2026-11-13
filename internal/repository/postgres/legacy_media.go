package postgres

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"sweetshop/internal/domain"
)

// Legacy product media migration (ADR 021). The lock order is the product
// media order from media.go: staff row, product FOR UPDATE, assets FOR SHARE.
// Each product commits on its own with its audit event.

// legacyMigrationLockKey is the session-level advisory lock ("sweetlgc" in
// ASCII) held for one apply run.
const legacyMigrationLockKey int64 = 0x73776565746c6763

// Match the schema migrator's independent cleanup budget, shared by unlock
// and close so cancellation cannot leave command cleanup waiting indefinitely.
const legacyMigrationCleanupTimeout = 5 * time.Second

// ErrLegacyMigrationRunning: another apply run holds the lock.
var ErrLegacyMigrationRunning = errors.New("another legacy media migration is running")

// LockLegacyMigration takes the run lock and returns its release function.
func (s *Store) LockLegacyMigration(ctx context.Context) (func() error, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire migration connection: %w", err)
	}
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, legacyMigrationLockKey).Scan(&locked); err != nil {
		conn.Release()
		return nil, fmt.Errorf("take legacy migration lock: %w", err)
	}
	if !locked {
		conn.Release()
		return nil, ErrLegacyMigrationRunning
	}
	return func() error { return releaseLegacyMigrationLock(conn) }, nil
}

func releaseLegacyMigrationLock(conn *pgxpool.Conn) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), legacyMigrationCleanupTimeout)
	defer cancel()
	if _, err := conn.Exec(cleanupCtx, `SELECT pg_advisory_unlock($1)`, legacyMigrationLockKey); err != nil {
		// An uncertain unlock must never return the session to the pool.
		// Like the schema migrator, take ownership before closing. pgx Close
		// closes the underlying socket even when the cleanup budget expires.
		closeErr := conn.Hijack().Close(cleanupCtx)
		if closeErr != nil {
			closeErr = fmt.Errorf("close legacy migration connection: %w", closeErr)
		}
		return errors.Join(fmt.Errorf("release legacy migration lock: %w", err), closeErr)
	}
	conn.Release()
	return nil
}

// LegacyMediaProducts lists every product, archived ones included, in ID order.
func (s *Store) LegacyMediaProducts(ctx context.Context) ([]domain.LegacyMediaProduct, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.id,p.image,p.gallery,p.legacy_media_retired_at IS NOT NULL,
			EXISTS(SELECT 1 FROM product_media pm JOIN media_assets m ON m.id=pm.media_id
				WHERE pm.product_id=p.id AND pm.role='primary' AND m.deleted_at IS NULL),
			(SELECT count(*) FROM product_media pm JOIN media_assets m ON m.id=pm.media_id
				WHERE pm.product_id=p.id AND pm.role='gallery' AND m.deleted_at IS NULL)
		FROM products p ORDER BY p.id`)
	if err != nil {
		return nil, fmt.Errorf("list legacy media products: %w", err)
	}
	defer rows.Close()
	result := []domain.LegacyMediaProduct{}
	for rows.Next() {
		var p domain.LegacyMediaProduct
		if err := rows.Scan(&p.ID, &p.Image, &p.Gallery, &p.Retired, &p.ManagedPrimary, &p.ManagedGallery); err != nil {
			return nil, fmt.Errorf("scan legacy media product: %w", err)
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// LiveMediaBySHA256 returns the oldest live asset with these normalized
// bytes, or ErrNotFound. It is a sequential scan by design: a rare operator
// command does not justify an index on every upload.
func (s *Store) LiveMediaBySHA256(ctx context.Context, sum string) (domain.StoredMedia, error) {
	var stored domain.StoredMedia
	err := s.pool.QueryRow(ctx, `SELECT id,content_type,size_bytes,sha256 FROM media_assets
		WHERE sha256=$1 AND deleted_at IS NULL ORDER BY created_at,id LIMIT 1`, sum).
		Scan(&stored.ID, &stored.ContentType, &stored.SizeBytes, &stored.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StoredMedia{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.StoredMedia{}, fmt.Errorf("find media by digest: %w", err)
	}
	return stored, nil
}

// lockLegacyState locks the product row and reports its legacy columns,
// whether it is retired, and whether it has live managed references.
func lockLegacyState(ctx context.Context, tx StaffTx, id string) (image string, gallery []string, retired, managed bool, err error) {
	err = tx.QueryRow(ctx, `SELECT image,gallery,legacy_media_retired_at IS NOT NULL FROM products WHERE id=$1 FOR UPDATE`, id).Scan(&image, &gallery, &retired)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, false, false, domain.ErrNotFound
	}
	if err != nil {
		return "", nil, false, false, fmt.Errorf("lock product: %w", err)
	}
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM product_media pm JOIN media_assets m ON m.id=pm.media_id
		WHERE pm.product_id=$1 AND m.deleted_at IS NULL)`, id).Scan(&managed)
	if err != nil {
		return "", nil, false, false, fmt.Errorf("read product media: %w", err)
	}
	return image, gallery, retired, managed, nil
}

// MigrateLegacyProductMedia attaches a legacy-only product's images and
// retires its legacy fallback in one transaction, bumping the revision once.
// A product that changed since it was classified is ErrConflict and stays
// exactly as it was; the legacy columns themselves are never written.
func (s *Store) MigrateLegacyProductMedia(ctx context.Context, mutation domain.StaffMutation, expected domain.LegacyMediaProduct, value domain.ProductMedia) error {
	_, err := s.InStaffMutation(ctx, mutation, func(ctx context.Context, tx StaffTx) (domain.AuditTarget, error) {
		image, gallery, retired, managed, err := lockLegacyState(ctx, tx, value.ProductID)
		if err != nil {
			return domain.AuditTarget{}, err
		}
		if retired || managed || image != expected.Image || !slices.Equal(gallery, expected.Gallery) {
			return domain.AuditTarget{}, domain.ErrConflict
		}
		if err := replaceProductMedia(ctx, tx, value); err != nil {
			return domain.AuditTarget{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE products SET legacy_media_retired_at=now(),revision=revision+1,updated_at=now() WHERE id=$1`, value.ProductID); err != nil {
			return domain.AuditTarget{}, fmt.Errorf("retire legacy media: %w", err)
		}
		return domain.AuditTarget{Type: "product", ID: value.ProductID}, nil
	})
	return err
}

// RetireLegacyProductMedia stamps a product that already has managed media.
// Its references and revision stay exactly as staff left them.
func (s *Store) RetireLegacyProductMedia(ctx context.Context, mutation domain.StaffMutation, productID string) error {
	_, err := s.InStaffMutation(ctx, mutation, func(ctx context.Context, tx StaffTx) (domain.AuditTarget, error) {
		_, _, retired, managed, err := lockLegacyState(ctx, tx, productID)
		if err != nil {
			return domain.AuditTarget{}, err
		}
		if retired || !managed {
			return domain.AuditTarget{}, domain.ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE products SET legacy_media_retired_at=now() WHERE id=$1`, productID); err != nil {
			return domain.AuditTarget{}, fmt.Errorf("retire legacy media: %w", err)
		}
		return domain.AuditTarget{Type: "product", ID: productID}, nil
	})
	return err
}

// LegacyMediaReferences counts every database reference to an /assets path.
func (s *Store) LegacyMediaReferences(ctx context.Context) (map[string]*domain.LegacyPathReferences, error) {
	result := map[string]*domain.LegacyPathReferences{}
	at := func(path string) *domain.LegacyPathReferences {
		if result[path] == nil {
			result[path] = &domain.LegacyPathReferences{}
		}
		return result[path]
	}
	products, err := s.pool.Query(ctx, `
		SELECT path,count(*) FILTER (WHERE NOT retired),count(*) FILTER (WHERE retired)
		FROM (SELECT DISTINCT p.id,path,p.legacy_media_retired_at IS NOT NULL AS retired
			FROM products p,unnest(p.gallery||ARRAY[p.image]) AS path WHERE path LIKE '/assets/%') refs
		GROUP BY path`)
	if err != nil {
		return nil, fmt.Errorf("count legacy product references: %w", err)
	}
	defer products.Close()
	for products.Next() {
		var path string
		var active, compatibility int
		if err := products.Scan(&path, &active, &compatibility); err != nil {
			return nil, fmt.Errorf("scan legacy product references: %w", err)
		}
		at(path).Active, at(path).Compatibility = active, compatibility
	}
	if err := products.Err(); err != nil {
		return nil, fmt.Errorf("iterate legacy product references: %w", err)
	}
	products.Close()
	for _, query := range []struct {
		name, sql string
		add       func(*domain.LegacyPathReferences, int)
	}{
		{"order", `SELECT image,count(DISTINCT order_id) FROM order_items WHERE image LIKE '/assets/%' GROUP BY image`,
			func(r *domain.LegacyPathReferences, n int) { r.Orders += n }},
		{"catalog", `SELECT path,count(*) FROM (
				SELECT image AS path FROM collections UNION ALL SELECT logo FROM brands UNION ALL SELECT logo FROM manufacturers
			) refs WHERE path LIKE '/assets/%' GROUP BY path`,
			func(r *domain.LegacyPathReferences, n int) { r.Other += n }},
	} {
		rows, err := s.pool.Query(ctx, query.sql)
		if err != nil {
			return nil, fmt.Errorf("count legacy %s references: %w", query.name, err)
		}
		for rows.Next() {
			var path string
			var count int
			if err := rows.Scan(&path, &count); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan legacy %s references: %w", query.name, err)
			}
			query.add(at(path), count)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate legacy %s references: %w", query.name, err)
		}
	}
	return result, nil
}
