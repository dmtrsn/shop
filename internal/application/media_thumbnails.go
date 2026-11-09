package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"sweetshop/internal/domain"
	"sweetshop/internal/media"
)

// Problem kinds of an asset the backfill could not finish. The report names
// only a kind; the cause, which may carry a path, goes to the log.
const (
	ThumbnailOriginalMissing = "original_missing"
	ThumbnailOriginalDamaged = "original_damaged"
	ThumbnailInvalidOriginal = "invalid_original"
	ThumbnailStorageError    = "storage_error"
	ThumbnailDatabaseError   = "database_error"
)

// thumbnailBackfillPage bounds one read of the asset list.
const thumbnailBackfillPage = 200

// ThumbnailBackfillFailure is one asset left without a current thumbnail.
type ThumbnailBackfillFailure struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// ThumbnailBackfillReport is the JSON report of one run. Bytes count what was
// actually read and written, so a dry run generates none.
type ThumbnailBackfillReport struct {
	Mode          string `json:"mode"`
	Inspected     int    `json:"inspected"`
	Complete      int    `json:"complete"`
	NotApplicable int    `json:"notApplicable"`
	// NeedsGeneration and NeedsRegeneration are what a dry run found to do.
	NeedsGeneration   int `json:"needsGeneration"`
	NeedsRegeneration int `json:"needsRegeneration"`
	Generated         int `json:"generated"`
	Regenerated       int `json:"regenerated"`
	// ParentDeleted counts assets deleted while they were being processed.
	ParentDeleted           int                        `json:"parentDeleted"`
	Failed                  int                        `json:"failed"`
	OriginalBytesInspected  int64                      `json:"originalBytesInspected"`
	ThumbnailBytesGenerated int64                      `json:"thumbnailBytesGenerated"`
	LimitReached            bool                       `json:"limitReached"`
	Failures                []ThumbnailBackfillFailure `json:"failures"`
}

// Pending reports whether a later run still has work, or an operator does.
func (r ThumbnailBackfillReport) Pending() bool {
	return r.NeedsGeneration > 0 || r.NeedsRegeneration > 0 || r.Failed > 0 || r.LimitReached
}

// BackfillThumbnails brings every live asset to a current thumbnail (ADR 022).
// Each asset stands alone: one that fails is reported and the next proceeds,
// and an interrupted run is finished by the next one, which derives the same
// bytes. limit counts thumbnails generated, not assets inspected or failed, so
// a rerun always reaches further than the last one; 0 means no limit.
func (m *Media) BackfillThumbnails(ctx context.Context, apply bool, limit int) (ThumbnailBackfillReport, error) {
	report := ThumbnailBackfillReport{Mode: "dry-run", Failures: []ThumbnailBackfillFailure{}}
	if apply {
		report.Mode = "apply"
	}
	after := ""
	for {
		page, err := m.store.LiveMediaThumbnailStates(ctx, after, thumbnailBackfillPage)
		if err != nil {
			return report, err
		}
		for _, state := range page {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			if limit > 0 && report.Generated+report.Regenerated+report.NeedsGeneration+report.NeedsRegeneration >= limit {
				report.LimitReached = true
				return report, nil
			}
			m.backfillThumbnail(ctx, apply, state, &report)
		}
		if len(page) < thumbnailBackfillPage {
			return report, nil
		}
		after = page[len(page)-1].Asset.ID
	}
}

func (m *Media) backfillThumbnail(ctx context.Context, apply bool, state domain.MediaThumbnailState, report *ThumbnailBackfillReport) {
	id := state.Asset.ID
	fail := func(kind string, err error) {
		// A cancelled run is not a damaged asset; the caller reports the cancellation.
		if ctx.Err() != nil {
			return
		}
		m.logger.Error("media thumbnail backfill failed", "media", id, "kind", kind, "error", err)
		report.Failed++
		report.Failures = append(report.Failures, ThumbnailBackfillFailure{ID: id, Kind: kind})
	}
	report.Inspected++
	if max(state.Width, state.Height) <= media.ThumbnailMaxSide {
		report.NotApplicable++
		return
	}
	if row := state.Thumbnail; row != nil && row.Recipe == media.ThumbnailRecipe {
		err := media.VerifyThumbnail(m.files, id, row.SHA256, row.SizeBytes)
		if err == nil {
			report.Complete++
			return
		}
		if !errors.Is(err, media.ErrNotStored) && !errors.Is(err, media.ErrThumbnailDamaged) {
			fail(ThumbnailStorageError, err)
			return
		}
	}
	original, kind, err := m.readVerifiedOriginal(state.Asset)
	if err != nil {
		fail(kind, err)
		return
	}
	report.OriginalBytesInspected += int64(len(original))
	replacing := state.Thumbnail != nil
	if !apply {
		if replacing {
			report.NeedsRegeneration++
		} else {
			report.NeedsGeneration++
		}
		return
	}
	// No transaction is open here: decoding holds only a normalization slot.
	thumbnail, needed, err := m.deriveThumbnail(ctx, original)
	if err == nil && !needed {
		err = errors.New("the stored image no longer exceeds the recipe's size")
	}
	if err != nil {
		fail(ThumbnailInvalidOriginal, err)
		return
	}
	if err := m.files.ReplaceThumbnail(id, thumbnail.SHA256, thumbnail.Data); err != nil {
		fail(ThumbnailStorageError, err)
		return
	}
	switch err := m.store.SaveMediaThumbnail(ctx, id, thumbnailRow(thumbnail)); {
	case errors.Is(err, domain.ErrNotFound):
		// The parent was deleted meanwhile, so nothing of it may stay. A failed
		// removal leaves a deleted asset's file, which reconciliation removes.
		report.ParentDeleted++
		if err := m.files.RemoveThumbnails(id); err != nil {
			m.logger.Warn("thumbnail of a deleted asset left for reconciliation", "media", id, "error", err)
		}
		return
	case err != nil:
		fail(ThumbnailDatabaseError, err)
		return
	}
	report.ThumbnailBytesGenerated += int64(len(thumbnail.Data))
	if replacing {
		report.Regenerated++
	} else {
		report.Generated++
	}
}

// readVerifiedOriginal returns the stored original only after its size and
// digest are proven against the asset row, so a thumbnail is never derived
// from damaged bytes. It returns the failure kind with any error.
func (m *Media) readVerifiedOriginal(stored domain.StoredMedia) ([]byte, string, error) {
	file, size, err := m.files.Open(stored.ID)
	if errors.Is(err, media.ErrNotStored) {
		return nil, ThumbnailOriginalMissing, err
	}
	if err != nil {
		return nil, ThumbnailStorageError, err
	}
	defer file.Close()
	if size != stored.SizeBytes {
		return nil, ThumbnailOriginalDamaged, fmt.Errorf("stored size %d differs from recorded size %d", size, stored.SizeBytes)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, ThumbnailStorageError, fmt.Errorf("read original: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != stored.SHA256 {
		return nil, ThumbnailOriginalDamaged, errors.New("stored bytes differ from the recorded digest")
	}
	return data, "", nil
}
