package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"sweetshop/internal/domain"
	"sweetshop/internal/media"
)

// Managed media actions (ADR 019). Reads are authorized as media.read.
// Product media updates share the product revision.
const (
	MediaRead          = "media.read"
	MediaUpload        = domain.AuditAction("media.upload")
	MediaDelete        = domain.AuditAction("media.delete")
	ProductMediaUpdate = domain.AuditAction("product.media_update")
)

// MaxGalleryImages bounds a product's managed gallery.
const MaxGalleryImages = 20

// maxConcurrentNormalizations bounds decoded pixel buffers in memory: each
// normalization or thumbnail derivation may hold up to media.MaxPixels RGBA
// pixels (64 MiB) and, for a thumbnail, its resampling buffer (ADR 022).
const maxConcurrentNormalizations = 2

// MediaStore is the metadata port; only PostgreSQL implements it.
type MediaStore interface {
	// CreateMediaAsset registers a stored object, its thumbnail if it has one,
	// and one audit event in one transaction.
	// An error does not prove rollback: the commit acknowledgement may be lost.
	CreateMediaAsset(ctx context.Context, mutation domain.StaffMutation, asset domain.MediaAsset, thumbnail *domain.MediaThumbnail) (domain.MediaAsset, error)
	MediaAssets(ctx context.Context, page, limit int) (domain.MediaPage, error)
	// MediaAsset and LiveMedia return ErrNotFound for unknown and deleted assets.
	MediaAsset(ctx context.Context, id string) (domain.MediaAsset, error)
	LiveMedia(ctx context.Context, id string) (domain.StoredMedia, error)
	// LiveMediaThumbnail returns the current thumbnail of a live asset, and
	// ErrNotFound for an unknown or deleted asset or one with no thumbnail.
	LiveMediaThumbnail(ctx context.Context, id string) (domain.MediaThumbnail, error)
	// DeleteMediaAsset tombstones an unreferenced asset with its audit event:
	// ErrNotFound, or *ReferenceConflictError while products reference it.
	DeleteMediaAsset(ctx context.Context, mutation domain.StaffMutation, id string) error
	// MediaRows lists every row, ID to deleted, for reconciliation.
	MediaRows(ctx context.Context) (map[string]bool, error)
	// MediaThumbnailStates lists every asset with its thumbnail, for reconciliation.
	MediaThumbnailStates(ctx context.Context) ([]domain.MediaThumbnailState, error)
	// LiveMediaThumbnailStates pages through live assets in ID order.
	LiveMediaThumbnailStates(ctx context.Context, after string, limit int) ([]domain.MediaThumbnailState, error)
	// SaveMediaThumbnail records a live asset's current thumbnail, or returns
	// ErrNotFound when the asset is gone or deleted.
	SaveMediaThumbnail(ctx context.Context, id string, thumbnail domain.MediaThumbnail) error
	ProductMedia(ctx context.Context, productID string) (domain.ProductMedia, error)
	// SetProductMedia replaces a product's managed images under the product
	// revision: ErrNotFound, ErrStaleRevision, or a ValidationError for a
	// missing or deleted asset.
	SetProductMedia(ctx context.Context, mutation domain.StaffMutation, expectedRevision int64, value domain.ProductMedia) (domain.ProductMedia, error)
}

// Media is the managed media use case: upload, list, delete, public delivery
// and the product reference primitive C2 builds on.
type Media struct {
	store  MediaStore
	files  media.Store
	logger *slog.Logger
	slots  chan struct{}
}

func NewMedia(store MediaStore, files media.Store, logger *slog.Logger) *Media {
	return &Media{store: store, files: files, logger: logger, slots: make(chan struct{}, maxConcurrentNormalizations)}
}

// Upload validates and normalizes one image, derives its thumbnail, writes
// both to the store and only then registers them. Once registration starts,
// errors may mean a lost commit acknowledgement, so both files are retained.
// True orphans are left for reconciliation after its grace period. A row
// therefore never names bytes that were not written completely.
func (m *Media) Upload(ctx context.Context, mutation domain.StaffMutation, data []byte) (domain.MediaAsset, error) {
	if err := requireAction(mutation, MediaUpload); err != nil {
		return domain.MediaAsset{}, err
	}
	normalized, err := m.normalize(ctx, data)
	if err != nil {
		return domain.MediaAsset{}, uploadError(err)
	}
	return m.register(ctx, mutation, normalized)
}

func (m *Media) normalize(ctx context.Context, data []byte) (media.Normalized, error) {
	select {
	case m.slots <- struct{}{}:
	case <-ctx.Done():
		return media.Normalized{}, ctx.Err()
	}
	defer func() { <-m.slots }()
	return media.Normalize(data)
}

// deriveThumbnail shares the normalization slots, so the two together stay
// within the same bound of decoded images.
func (m *Media) deriveThumbnail(ctx context.Context, original []byte) (media.Thumbnail, bool, error) {
	select {
	case m.slots <- struct{}{}:
	case <-ctx.Done():
		return media.Thumbnail{}, false, ctx.Err()
	}
	defer func() { <-m.slots }()
	return media.DeriveThumbnail(original)
}

// register writes normalized bytes and their thumbnail and registers them;
// see Upload. The thumbnail is derived from those same normalized bytes, and
// before anything is written, so its failure leaves nothing behind.
func (m *Media) register(ctx context.Context, mutation domain.StaffMutation, normalized media.Normalized) (domain.MediaAsset, error) {
	id, err := media.NewID()
	if err != nil {
		return domain.MediaAsset{}, err
	}
	thumbnail, hasThumbnail, err := m.deriveThumbnail(ctx, normalized.Data)
	if err != nil {
		return domain.MediaAsset{}, fmt.Errorf("derive media thumbnail: %w", err)
	}
	if err := m.files.Put(id, normalized.Data); err != nil {
		m.logger.Error("media store write failed", "error", err)
		return domain.MediaAsset{}, domain.ErrStorageUnavailable
	}
	var row *domain.MediaThumbnail
	if hasThumbnail {
		if err := m.files.PutThumbnail(id, thumbnail.SHA256, thumbnail.Data); err != nil {
			m.logger.Error("media thumbnail write failed", "error", err)
			m.discardFiles(id)
			return domain.MediaAsset{}, domain.ErrStorageUnavailable
		}
		saved := thumbnailRow(thumbnail)
		row = &saved
	}
	asset, err := m.store.CreateMediaAsset(ctx, mutation, domain.MediaAsset{
		ID: id, ContentType: normalized.ContentType, SizeBytes: int64(len(normalized.Data)),
		Width: normalized.Width, Height: normalized.Height, SHA256: normalized.SHA256,
	}, row)
	if err != nil {
		// The metadata/audit transaction may already be durable. Never remove
		// its files on an error that does not prove the transaction rolled back.
		return domain.MediaAsset{}, err
	}
	return asset, nil
}

func thumbnailRow(thumbnail media.Thumbnail) domain.MediaThumbnail {
	return domain.MediaThumbnail{Recipe: thumbnail.Recipe, ContentType: thumbnail.ContentType,
		SizeBytes: int64(len(thumbnail.Data)), Width: thumbnail.Width, Height: thumbnail.Height, SHA256: thumbnail.SHA256}
}

// discardFiles is safe only before metadata registration has been attempted.
func (m *Media) discardFiles(id string) {
	if err := m.files.RemoveThumbnails(id); err != nil {
		m.logger.Error("unregistered media thumbnail left for reconciliation", "media", id, "error", err)
	}
	if err := m.files.Remove(id); err != nil {
		m.logger.Error("unregistered media object left for reconciliation", "media", id, "error", err)
	}
}

// verifyStored proves a live asset's object exists with its recorded size.
func (m *Media) verifyStored(stored domain.StoredMedia) error {
	file, size, err := m.files.Open(stored.ID)
	if err != nil {
		return err
	}
	defer file.Close()
	if size != stored.SizeBytes {
		return fmt.Errorf("stored size %d differs from recorded size %d", size, stored.SizeBytes)
	}
	return nil
}

func uploadError(err error) error {
	var invalid *media.InvalidImageError
	switch {
	case errors.Is(err, media.ErrTooLarge):
		return domain.ErrMediaTooLarge
	case errors.Is(err, media.ErrUnsupported):
		return domain.ErrUnsupportedMedia
	case errors.As(err, &invalid) && invalid.Dimensions:
		return &domain.ValidationError{Fields: []domain.FieldError{{Field: "file",
			Message: fmt.Sprintf("Изображение должно быть не больше %d пикселей по стороне и %d мегапикселей всего", media.MaxSide, media.MaxPixels/1_000_000)}}}
	case errors.As(err, &invalid):
		return &domain.ValidationError{Fields: []domain.FieldError{{Field: "file", Message: "Файл изображения повреждён или обрезан"}}}
	}
	return err
}

func (m *Media) List(ctx context.Context, page, limit int) (domain.MediaPage, error) {
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = defaultAdminLimit
	}
	fields := []domain.FieldError{}
	if page < 1 || page > 10_000 {
		fields = append(fields, domain.FieldError{Field: "page", Message: "От 1 до 10000"})
	}
	if limit < 1 || limit > MaxAdminPageLimit {
		fields = append(fields, domain.FieldError{Field: "limit", Message: fmt.Sprintf("От 1 до %d", MaxAdminPageLimit)})
	}
	if len(fields) > 0 {
		return domain.MediaPage{}, &domain.ValidationError{Fields: fields}
	}
	return m.store.MediaAssets(ctx, page, limit)
}

func (m *Media) Get(ctx context.Context, id string) (domain.MediaAsset, error) {
	if !media.IDPattern.MatchString(id) {
		return domain.MediaAsset{}, domain.ErrNotFound
	}
	return m.store.MediaAsset(ctx, id)
}

// Delete tombstones an unreferenced asset and then removes its bytes. The
// tombstone stands even if the unlink fails: the public route already
// refuses the asset, and reconciliation removes the leftover later.
func (m *Media) Delete(ctx context.Context, mutation domain.StaffMutation, id string) error {
	if err := requireAction(mutation, MediaDelete); err != nil {
		return err
	}
	if !media.IDPattern.MatchString(id) {
		return domain.ErrNotFound
	}
	if err := m.store.DeleteMediaAsset(ctx, mutation, id); err != nil {
		return err
	}
	if err := m.files.RemoveThumbnails(id); err != nil {
		m.logger.Warn("deleted media thumbnail left for reconciliation", "media", id, "error", err)
	}
	if err := m.files.Remove(id); err != nil {
		m.logger.Warn("deleted media object left for reconciliation", "media", id, "error", err)
	}
	return nil
}

// Open returns a live asset's metadata and bytes for public delivery. A live
// row without its object is served as missing and logged: the bytes cannot be
// recreated, and reconciliation reports it.
func (m *Media) Open(ctx context.Context, id string) (domain.StoredMedia, io.ReadSeekCloser, error) {
	if !media.IDPattern.MatchString(id) {
		return domain.StoredMedia{}, nil, domain.ErrNotFound
	}
	stored, err := m.store.LiveMedia(ctx, id)
	if err != nil {
		return domain.StoredMedia{}, nil, err
	}
	file, size, err := m.files.Open(id)
	if errors.Is(err, media.ErrNotStored) {
		m.logger.Error("live media object missing", "media", id)
		return domain.StoredMedia{}, nil, domain.ErrNotFound
	}
	if err != nil {
		m.logger.Error("media store read failed", "media", id, "error", err)
		return domain.StoredMedia{}, nil, domain.ErrStorageUnavailable
	}
	if size != stored.SizeBytes {
		file.Close()
		m.logger.Error("media object size differs from its metadata", "media", id)
		return domain.StoredMedia{}, nil, domain.ErrNotFound
	}
	return stored, file, nil
}

// OpenThumbnail returns one thumbnail for public delivery, only while its
// parent is live and sum is the digest its row names now (ADR 022). The file
// alone proves nothing: an older binary's delete can leave it and its row
// behind, and a regenerated thumbnail's old file stays until reconciliation.
func (m *Media) OpenThumbnail(ctx context.Context, id, sum string) (domain.MediaThumbnail, io.ReadSeekCloser, error) {
	if !media.IDPattern.MatchString(id) || !media.ThumbnailDigestPattern.MatchString(sum) {
		return domain.MediaThumbnail{}, nil, domain.ErrNotFound
	}
	thumbnail, err := m.store.LiveMediaThumbnail(ctx, id)
	if err != nil {
		return domain.MediaThumbnail{}, nil, err
	}
	if thumbnail.SHA256 != sum {
		return domain.MediaThumbnail{}, nil, domain.ErrNotFound
	}
	file, size, err := m.files.OpenThumbnail(id, sum)
	if errors.Is(err, media.ErrNotStored) {
		m.logger.Error("media thumbnail file missing", "media", id)
		return domain.MediaThumbnail{}, nil, domain.ErrNotFound
	}
	if err != nil {
		m.logger.Error("media thumbnail read failed", "media", id, "error", err)
		return domain.MediaThumbnail{}, nil, domain.ErrStorageUnavailable
	}
	if size != thumbnail.SizeBytes {
		file.Close()
		m.logger.Error("media thumbnail size differs from its metadata", "media", id)
		return domain.MediaThumbnail{}, nil, domain.ErrNotFound
	}
	return thumbnail, file, nil
}

// SetProductMedia replaces a product's managed primary image and gallery.
// All references are replaced atomically.
func (m *Media) SetProductMedia(ctx context.Context, mutation domain.StaffMutation, expectedRevision int64, value domain.ProductMedia) (domain.ProductMedia, error) {
	if err := requireAction(mutation, ProductMediaUpdate); err != nil {
		return domain.ProductMedia{}, err
	}
	fields := revisionFields(expectedRevision)
	if value.PrimaryID != "" && !media.IDPattern.MatchString(value.PrimaryID) {
		fields = append(fields, domain.FieldError{Field: "primaryId", Message: "Укажите изображение из медиатеки"})
	}
	if value.GalleryIDs == nil {
		value.GalleryIDs = []string{}
	}
	seen := map[string]bool{}
	for _, id := range value.GalleryIDs {
		if !media.IDPattern.MatchString(id) || seen[id] || id == value.PrimaryID {
			fields = append(fields, domain.FieldError{Field: "galleryIds", Message: "Изображения галереи должны быть из медиатеки и не повторяться"})
			break
		}
		seen[id] = true
	}
	if len(value.GalleryIDs) > MaxGalleryImages {
		fields = append(fields, domain.FieldError{Field: "galleryIds", Message: fmt.Sprintf("Не больше %d изображений", MaxGalleryImages)})
	}
	if len(fields) > 0 {
		return domain.ProductMedia{}, &domain.ValidationError{Fields: fields}
	}
	return m.store.SetProductMedia(ctx, mutation, expectedRevision, value)
}

func (m *Media) ProductMedia(ctx context.Context, productID string) (domain.ProductMedia, error) {
	return m.store.ProductMedia(ctx, productID)
}

// Reconcile compares the store with the metadata; see media.Reconcile.
func (m *Media) Reconcile(ctx context.Context, grace time.Duration, apply bool) (media.Report, error) {
	return media.Reconcile(ctx, m.files, m.store.MediaRows, m.store.MediaThumbnailStates, time.Now(), grace, apply)
}
