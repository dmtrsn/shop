package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"sweetshop/internal/domain"
)

// Managed media metadata (ADR 019). Lock order, shared with every future
// product-media write:
//
//	upload           staff row → insert asset row
//	delete           staff row → asset row FOR UPDATE → count product_media
//	product media    staff row → product FOR UPDATE → asset rows FOR SHARE (ID order)
//	                 → replace product_media rows
//
// Delete's row lock conflicts with the share lock (and the foreign key's key
// share) an attachment takes. Whichever commits first decides: a delete that
// commits first makes the attachment see the tombstone and fail; an
// attachment that commits first makes the delete count its reference and
// refuse. No path locks a product after an asset, so no cycle forms.

const adminMediaColumns = `m.id,m.content_type,m.size_bytes,m.width,m.height,m.sha256,m.created_at,
	(SELECT count(DISTINCT pm.product_id) FROM product_media pm WHERE pm.media_id=m.id),
 (SELECT count(DISTINCT oi.order_id) FROM order_items oi WHERE oi.image='/media/'||m.id AND oi.image LIKE '/media/%')`

func scanMediaAsset(row scanner) (domain.MediaAsset, error) {
	var asset domain.MediaAsset
	err := row.Scan(&asset.ID, &asset.ContentType, &asset.SizeBytes, &asset.Width, &asset.Height, &asset.SHA256, &asset.CreatedAt, &asset.ProductCount, &asset.OrderCount)
	asset.CreatedAt = asset.CreatedAt.UTC()
	asset.URL = "/media/" + asset.ID
	return asset, err
}

func loadMediaAsset(ctx context.Context, q rowQuerier, id string) (domain.MediaAsset, error) {
	asset, err := scanMediaAsset(q.QueryRow(ctx, "SELECT "+adminMediaColumns+" FROM media_assets m WHERE m.id=$1 AND m.deleted_at IS NULL", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MediaAsset{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.MediaAsset{}, fmt.Errorf("load media asset: %w", err)
	}
	return asset, nil
}

func (s *Store) CreateMediaAsset(ctx context.Context, mutation domain.StaffMutation, asset domain.MediaAsset) (domain.MediaAsset, error) {
	var result domain.MediaAsset
	_, err := s.InStaffMutation(ctx, mutation, func(ctx context.Context, tx StaffTx) (domain.AuditTarget, error) {
		if _, err := tx.Exec(ctx, `INSERT INTO media_assets(id,content_type,size_bytes,width,height,sha256) VALUES ($1,$2,$3,$4,$5,$6)`,
			asset.ID, asset.ContentType, asset.SizeBytes, asset.Width, asset.Height, asset.SHA256); err != nil {
			return domain.AuditTarget{}, fmt.Errorf("insert media asset: %w", err)
		}
		var err error
		if result, err = loadMediaAsset(ctx, tx, asset.ID); err != nil {
			return domain.AuditTarget{}, err
		}
		return domain.AuditTarget{Type: "media", ID: asset.ID}, nil
	})
	if err != nil {
		return domain.MediaAsset{}, err
	}
	return result, nil
}

func (s *Store) MediaAssets(ctx context.Context, page, limit int) (domain.MediaPage, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM media_assets WHERE deleted_at IS NULL`).Scan(&total); err != nil {
		return domain.MediaPage{}, fmt.Errorf("count media assets: %w", err)
	}
	rows, err := s.pool.Query(ctx, "SELECT "+adminMediaColumns+` FROM media_assets m WHERE m.deleted_at IS NULL
		ORDER BY m.created_at DESC,m.id LIMIT $1 OFFSET $2`, limit, (page-1)*limit)
	if err != nil {
		return domain.MediaPage{}, fmt.Errorf("list media assets: %w", err)
	}
	defer rows.Close()
	result := domain.MediaPage{Items: []domain.MediaAsset{}, Total: total, Page: page, Limit: limit, TotalPages: int(math.Ceil(float64(total) / float64(limit)))}
	for rows.Next() {
		asset, err := scanMediaAsset(rows)
		if err != nil {
			return domain.MediaPage{}, fmt.Errorf("scan media asset: %w", err)
		}
		result.Items = append(result.Items, asset)
	}
	if err := rows.Err(); err != nil {
		return domain.MediaPage{}, fmt.Errorf("iterate media assets: %w", err)
	}
	return result, nil
}

func (s *Store) MediaAsset(ctx context.Context, id string) (domain.MediaAsset, error) {
	return loadMediaAsset(ctx, s.pool, id)
}

func (s *Store) LiveMedia(ctx context.Context, id string) (domain.StoredMedia, error) {
	var stored domain.StoredMedia
	err := s.pool.QueryRow(ctx, `SELECT id,content_type,size_bytes,sha256 FROM media_assets WHERE id=$1 AND deleted_at IS NULL`, id).
		Scan(&stored.ID, &stored.ContentType, &stored.SizeBytes, &stored.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StoredMedia{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.StoredMedia{}, fmt.Errorf("load live media: %w", err)
	}
	return stored, nil
}

func (s *Store) DeleteMediaAsset(ctx context.Context, mutation domain.StaffMutation, id string) error {
	_, err := s.InStaffMutation(ctx, mutation, func(ctx context.Context, tx StaffTx) (domain.AuditTarget, error) {
		var deleted *time.Time
		err := tx.QueryRow(ctx, `SELECT deleted_at FROM media_assets WHERE id=$1 FOR UPDATE`, id).Scan(&deleted)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && deleted != nil) {
			return domain.AuditTarget{}, domain.ErrNotFound
		}
		if err != nil {
			return domain.AuditTarget{}, fmt.Errorf("lock media asset: %w", err)
		}
		var conflict domain.ReferenceConflictError
		if err := tx.QueryRow(ctx, `SELECT count(DISTINCT product_id) FROM product_media WHERE media_id=$1`, id).Scan(&conflict.Products); err != nil {
			return domain.AuditTarget{}, fmt.Errorf("count media references: %w", err)
		}
		if err := tx.QueryRow(ctx, `SELECT count(DISTINCT order_id) FROM order_items WHERE image=$1 AND image LIKE '/media/%'`, "/media/"+id).Scan(&conflict.Orders); err != nil {
			return domain.AuditTarget{}, fmt.Errorf("count order media references: %w", err)
		}
		if conflict.Products > 0 || conflict.Orders > 0 {
			return domain.AuditTarget{}, &conflict
		}
		if _, err := tx.Exec(ctx, `UPDATE media_assets SET deleted_at=now() WHERE id=$1`, id); err != nil {
			return domain.AuditTarget{}, fmt.Errorf("delete media asset: %w", err)
		}
		return domain.AuditTarget{Type: "media", ID: id}, nil
	})
	return err
}

func (s *Store) MediaRows(ctx context.Context) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,deleted_at IS NOT NULL FROM media_assets`)
	if err != nil {
		return nil, fmt.Errorf("list media rows: %w", err)
	}
	defer rows.Close()
	result := map[string]bool{}
	for rows.Next() {
		var id string
		var deleted bool
		if err := rows.Scan(&id, &deleted); err != nil {
			return nil, err
		}
		result[id] = deleted
	}
	return result, rows.Err()
}

func productMedia(ctx context.Context, q queryer, productID string) (domain.ProductMedia, error) {
	rows, err := q.Query(ctx, `SELECT media_id,role FROM product_media WHERE product_id=$1 ORDER BY role DESC,position`, productID)
	if err != nil {
		return domain.ProductMedia{}, fmt.Errorf("read product media: %w", err)
	}
	defer rows.Close()
	result := domain.ProductMedia{ProductID: productID, GalleryIDs: []string{}}
	for rows.Next() {
		var id, role string
		if err := rows.Scan(&id, &role); err != nil {
			return domain.ProductMedia{}, err
		}
		if role == string(domain.ProductMediaPrimary) {
			result.PrimaryID = id
		} else {
			result.GalleryIDs = append(result.GalleryIDs, id)
		}
	}
	return result, rows.Err()
}

func (s *Store) ProductMedia(ctx context.Context, productID string) (domain.ProductMedia, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM products WHERE id=$1)`, productID).Scan(&exists); err != nil {
		return domain.ProductMedia{}, fmt.Errorf("check product: %w", err)
	}
	if !exists {
		return domain.ProductMedia{}, domain.ErrNotFound
	}
	return productMedia(ctx, s.pool, productID)
}

// SetProductMedia replaces the product's managed images and bumps the
// product revision, so a concurrent product edit based on the old state is
// refused. Every referenced asset must be live when its share lock is held.
func (s *Store) SetProductMedia(ctx context.Context, mutation domain.StaffMutation, expectedRevision int64, value domain.ProductMedia) (domain.ProductMedia, error) {
	var result domain.ProductMedia
	_, err := s.InStaffMutation(ctx, mutation, func(ctx context.Context, tx StaffTx) (domain.AuditTarget, error) {
		if _, err := lockProductRevision(ctx, tx, value.ProductID, expectedRevision); err != nil {
			return domain.AuditTarget{}, err
		}
		if err := replaceProductMedia(ctx, tx, value); err != nil {
			return domain.AuditTarget{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE products SET revision=revision+1,updated_at=now() WHERE id=$1`, value.ProductID); err != nil {
			return domain.AuditTarget{}, fmt.Errorf("bump product revision: %w", err)
		}
		var err error
		if result, err = productMedia(ctx, tx, value.ProductID); err != nil {
			return domain.AuditTarget{}, err
		}
		return domain.AuditTarget{Type: "product", ID: value.ProductID}, nil
	})
	if err != nil {
		return domain.ProductMedia{}, err
	}
	return result, nil
}

// replaceProductMedia share-locks the selected assets, requires them to be
// live and replaces every reference of the product, whose row the caller
// holds locked.
func replaceProductMedia(ctx context.Context, tx StaffTx, value domain.ProductMedia) error {
	ids := append([]string{}, value.GalleryIDs...)
	if value.PrimaryID != "" {
		ids = append(ids, value.PrimaryID)
	}
	rows, err := tx.Query(ctx, `SELECT id FROM media_assets WHERE id=ANY($1) AND deleted_at IS NULL ORDER BY id FOR SHARE`, ids)
	if err != nil {
		return fmt.Errorf("lock media assets: %w", err)
	}
	live := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		live[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	fields := []domain.FieldError{}
	if value.PrimaryID != "" && !live[value.PrimaryID] {
		fields = append(fields, domain.FieldError{Field: "primaryId", Message: "Изображение не найдено или удалено"})
	}
	for _, id := range value.GalleryIDs {
		if !live[id] {
			fields = append(fields, domain.FieldError{Field: "galleryIds", Message: "Изображение не найдено или удалено"})
			break
		}
	}
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM product_media WHERE product_id=$1`, value.ProductID); err != nil {
		return fmt.Errorf("clear product media: %w", err)
	}
	if value.PrimaryID != "" {
		if _, err := tx.Exec(ctx, `INSERT INTO product_media(product_id,media_id,role,position) VALUES ($1,$2,'primary',0)`, value.ProductID, value.PrimaryID); err != nil {
			return fmt.Errorf("set primary media: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO product_media(product_id,media_id,role,position)
		SELECT $1,g.id,'gallery',g.ordinality-1 FROM unnest($2::text[]) WITH ORDINALITY AS g(id,ordinality)`, value.ProductID, value.GalleryIDs); err != nil {
		return fmt.Errorf("set gallery media: %w", err)
	}
	return nil
}

// The correlated read runs in the same statement/snapshot as the product and
// revision. No N+1 queries or torn revision/media pairs on admin reads.
const managedMediaColumn = `(SELECT jsonb_build_object(
 'primary', (jsonb_agg(image) FILTER (WHERE role='primary'))->0,
 'gallery', COALESCE(jsonb_agg(image ORDER BY position) FILTER (WHERE role='gallery'),'[]'::jsonb))
 FROM (SELECT pm.role,pm.position,jsonb_build_object('id',m.id,'url','/media/'||m.id,
 'width',m.width,'height',m.height,'contentType',m.content_type,'position',pm.position) AS image
 FROM product_media pm JOIN media_assets m ON m.id=pm.media_id
 WHERE pm.product_id=p.id AND m.deleted_at IS NULL) images HAVING count(*)>0)`
