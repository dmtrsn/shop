package media

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"sweetshop/internal/domain"
)

// Reconciliation compares the store with the metadata (ADR 019). The
// filesystem and PostgreSQL cannot commit together, so these states can occur:
//
//	live row, object present        healthy
//	live row, object missing        reported only: nothing can restore it
//	no row, object present          orphan of an upload that never committed;
//	                                removable once older than the grace period
//	deleted row, object present     leftover of a delete whose unlink failed;
//	                                removable at once
//	temporary file                  interrupted write; removable once older
//	                                than the grace period
//
// Only names the store itself owns are ever listed, so unknown files are
// never touched. The grace period keeps an upload that has written its file
// but not yet committed its row from being mistaken for an orphan.
//
// Thumbnails (ADR 022) are checked the same way, with one difference: a live
// row's file is also read and hashed, because a thumbnail is cheap to hash and
// can be derived again.
//
//	live asset, row, file matches      healthy
//	live asset, row, file gone         missing; media thumbnails --apply restores it
//	live asset, row, file differs      corrupt; media thumbnails --apply restores it
//	live asset, row of an older recipe still served; outdated until regenerated
//	live asset larger than the recipe, no row
//	                                   pending; informational during rollout
//	live asset, file not the row's     stale output; removable after the grace period
//	file with no row or no asset       orphan; removable after the grace period
//	deleted asset, file present        deleted; removable at once

// MinGrace is the shortest grace period apply mode accepts.
const MinGrace = time.Hour

// Rows returns every media row: ID to whether it is logically deleted.
type Rows func(ctx context.Context) (map[string]bool, error)

// ThumbnailRows returns every media row with its thumbnail metadata.
type ThumbnailRows func(ctx context.Context) ([]domain.MediaThumbnailState, error)

type Report struct {
	Healthy int `json:"healthy"`
	// Missing: live rows whose object is gone. Served as 404; needs an operator.
	Missing []string `json:"missing"`
	// Orphans: objects with no row; Young ones are inside the grace period.
	Orphans      []string `json:"orphans"`
	YoungOrphans []string `json:"youngOrphans"`
	// Deleted: objects of logically deleted rows.
	Deleted []string `json:"deleted"`
	// StaleTemp and YoungTemp: temporary files outside or inside the grace period.
	StaleTemp []string `json:"staleTemp"`
	YoungTemp []string `json:"youngTemp"`
	// Removed: what apply mode deleted; Failed: what it could not.
	Removed []string `json:"removed"`
	Failed  []string `json:"failed"`
	// Refused explains why apply mode removed no orphans, if it refused.
	Refused string `json:"refused,omitempty"`
	// HealthyThumbnails counts live rows whose file matches them.
	HealthyThumbnails int `json:"healthyThumbnails"`
	// PendingThumbnails: live assets larger than the recipe with no row yet.
	// Informational during rollout, never a problem by itself.
	PendingThumbnails []string `json:"pendingThumbnails"`
	// OutdatedThumbnails: rows of another recipe. Still served, so informational.
	OutdatedThumbnails []string `json:"outdatedThumbnails"`
	// MissingThumbnails and CorruptThumbnails: live rows whose file is gone or
	// differs from them. media thumbnails --apply restores both.
	MissingThumbnails []string `json:"missingThumbnails"`
	CorruptThumbnails []string `json:"corruptThumbnails"`
	// The rest are files, as <ID>/<SHA-256>: DeletedThumbnails belong to a
	// deleted asset; OrphanThumbnails no row names; StaleThumbnails are not
	// their row's current file. Young ones are inside the grace period.
	DeletedThumbnails     []string `json:"deletedThumbnails"`
	OrphanThumbnails      []string `json:"orphanThumbnails"`
	YoungOrphanThumbnails []string `json:"youngOrphanThumbnails"`
	StaleThumbnails       []string `json:"staleThumbnails"`
	YoungStaleThumbnails  []string `json:"youngStaleThumbnails"`
}

// Problems reports whether anything needs attention (removable leftovers or
// missing or damaged files). Pending and outdated thumbnails do not count, and
// neither does anything inside the grace period.
func (r Report) Problems() bool {
	return len(r.Missing)+len(r.Orphans)+len(r.Deleted)+len(r.StaleTemp)+len(r.Failed)+
		len(r.MissingThumbnails)+len(r.CorruptThumbnails)+len(r.DeletedThumbnails)+len(r.OrphanThumbnails)+len(r.StaleThumbnails) > 0
}

// DamagedThumbnails reports live rows only media thumbnails --apply can fix,
// which verify --apply therefore leaves behind.
func (r Report) DamagedThumbnails() bool {
	return len(r.MissingThumbnails)+len(r.CorruptThumbnails) > 0
}

// Reconcile classifies the store against the rows. With apply it removes
// deleted rows' objects, orphans and temporary files older than grace; it
// never removes anything a live row references.
func Reconcile(ctx context.Context, store Store, rows Rows, thumbnailRows ThumbnailRows, now time.Time, grace time.Duration, apply bool) (Report, error) {
	if apply && grace < MinGrace {
		return Report{}, fmt.Errorf("grace period must be at least %s", MinGrace)
	}
	// Objects first: an upload that commits after this listing is then found
	// among the rows rather than reported as an orphan.
	objects, err := store.Objects()
	if err != nil {
		return Report{}, err
	}
	thumbnails, err := store.Thumbnails()
	if err != nil {
		return Report{}, err
	}
	known, err := rows(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("read media rows: %w", err)
	}
	states, err := thumbnailRows(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("read media thumbnail rows: %w", err)
	}
	report := Report{Missing: []string{}, Orphans: []string{}, YoungOrphans: []string{}, Deleted: []string{}, StaleTemp: []string{}, YoungTemp: []string{}, Removed: []string{}, Failed: []string{},
		PendingThumbnails: []string{}, OutdatedThumbnails: []string{}, MissingThumbnails: []string{}, CorruptThumbnails: []string{},
		DeletedThumbnails: []string{}, OrphanThumbnails: []string{}, YoungOrphanThumbnails: []string{}, StaleThumbnails: []string{}, YoungStaleThumbnails: []string{}}
	stored := map[string]bool{}
	cutoff := now.Add(-grace)
	for _, object := range objects {
		old := object.Modified.Before(cutoff)
		if object.Temp {
			if old {
				report.StaleTemp = append(report.StaleTemp, object.Name)
			} else {
				report.YoungTemp = append(report.YoungTemp, object.Name)
			}
			continue
		}
		stored[object.ID] = true
		deleted, exists := known[object.ID]
		switch {
		case exists && !deleted:
			report.Healthy++
		case exists && deleted:
			report.Deleted = append(report.Deleted, object.ID)
		case old:
			report.Orphans = append(report.Orphans, object.ID)
		default:
			report.YoungOrphans = append(report.YoungOrphans, object.ID)
		}
	}
	for id, deleted := range known {
		if !deleted && !stored[id] {
			report.Missing = append(report.Missing, id)
		}
	}
	for _, list := range [][]string{report.Missing, report.Orphans, report.YoungOrphans, report.Deleted, report.StaleTemp, report.YoungTemp} {
		sort.Strings(list)
	}
	leftovers, err := classifyThumbnails(ctx, store, thumbnails, states, cutoff, &report)
	if err != nil {
		return Report{}, err
	}
	if !apply {
		return report, nil
	}
	remove := func(name string, err error) {
		if err != nil {
			report.Failed = append(report.Failed, name)
		} else {
			report.Removed = append(report.Removed, name)
		}
	}
	for _, id := range report.Deleted {
		remove(id, store.Remove(id))
	}
	// A database without a single media row next to stored objects is far
	// more likely the wrong DATABASE_URL than a store full of orphans.
	if len(report.Orphans) > 0 && len(known) == 0 {
		report.Refused = "the database has no media rows; orphans were not removed (check DATABASE_URL)"
	} else {
		for _, id := range report.Orphans {
			remove(id, store.Remove(id))
		}
	}
	for _, name := range report.StaleTemp {
		remove(name, store.RemoveTemp(name))
	}
	removeThumbnail := func(file ThumbnailFile) {
		remove(thumbnailName(file), store.RemoveThumbnail(file.ID, file.SHA256))
	}
	for _, file := range leftovers.deleted {
		removeThumbnail(file)
	}
	for _, file := range leftovers.stale {
		removeThumbnail(file)
	}
	// The same protection as for objects, and one more case: a database with
	// assets but no thumbnail row at all next to thumbnail files is more likely
	// a restored or wrong schema than a store full of orphans.
	if len(leftovers.orphans) > 0 && !leftovers.anyRow {
		if report.Refused == "" {
			report.Refused = "the database has no thumbnail rows; orphan thumbnails were not removed (check DATABASE_URL)"
		}
	} else {
		for _, file := range leftovers.orphans {
			removeThumbnail(file)
		}
	}
	if len(report.Failed) > 0 {
		return report, errors.New("some leftovers could not be removed")
	}
	return report, nil
}

// thumbnailLeftovers are the files apply mode may remove.
type thumbnailLeftovers struct {
	deleted, orphans, stale []ThumbnailFile
	// anyRow reports that the database holds at least one thumbnail row.
	anyRow bool
}

func thumbnailName(file ThumbnailFile) string {
	return file.ID + "/" + file.SHA256
}

// classifyThumbnails fills the report's thumbnail fields. Only a row's current
// file is read; a file that is not current is judged by its name and age alone.
func classifyThumbnails(ctx context.Context, store Store, files []ThumbnailFile, states []domain.MediaThumbnailState, cutoff time.Time, report *Report) (thumbnailLeftovers, error) {
	var leftovers thumbnailLeftovers
	byID := make(map[string]domain.MediaThumbnailState, len(states))
	for _, state := range states {
		byID[state.Asset.ID] = state
		leftovers.anyRow = leftovers.anyRow || state.Thumbnail != nil
	}
	for _, file := range files {
		state, known := byID[file.ID]
		old := file.Modified.Before(cutoff)
		switch {
		case known && state.Deleted:
			report.DeletedThumbnails = append(report.DeletedThumbnails, thumbnailName(file))
			leftovers.deleted = append(leftovers.deleted, file)
		case known && state.Thumbnail != nil && state.Thumbnail.SHA256 == file.SHA256:
			// The row's current file; checked below, and never removed here.
		case known && state.Thumbnail != nil && old:
			report.StaleThumbnails = append(report.StaleThumbnails, thumbnailName(file))
			leftovers.stale = append(leftovers.stale, file)
		case known && state.Thumbnail != nil:
			report.YoungStaleThumbnails = append(report.YoungStaleThumbnails, thumbnailName(file))
		case old:
			report.OrphanThumbnails = append(report.OrphanThumbnails, thumbnailName(file))
			leftovers.orphans = append(leftovers.orphans, file)
		default:
			report.YoungOrphanThumbnails = append(report.YoungOrphanThumbnails, thumbnailName(file))
		}
	}
	for _, state := range states {
		if state.Deleted {
			continue
		}
		if state.Thumbnail == nil {
			if max(state.Width, state.Height) > ThumbnailMaxSide {
				report.PendingThumbnails = append(report.PendingThumbnails, state.Asset.ID)
			}
			continue
		}
		if err := ctx.Err(); err != nil {
			return leftovers, err
		}
		if state.Thumbnail.Recipe != ThumbnailRecipe {
			report.OutdatedThumbnails = append(report.OutdatedThumbnails, state.Asset.ID)
		}
		switch err := VerifyThumbnail(store, state.Asset.ID, state.Thumbnail.SHA256, state.Thumbnail.SizeBytes); {
		case err == nil:
			report.HealthyThumbnails++
		case errors.Is(err, ErrNotStored):
			report.MissingThumbnails = append(report.MissingThumbnails, state.Asset.ID)
		default:
			// Unreadable is reported with different bytes: neither can be served
			// with confidence, and the operator must look at both.
			report.CorruptThumbnails = append(report.CorruptThumbnails, state.Asset.ID)
		}
	}
	for _, list := range [][]string{report.PendingThumbnails, report.OutdatedThumbnails, report.MissingThumbnails, report.CorruptThumbnails,
		report.DeletedThumbnails, report.OrphanThumbnails, report.YoungOrphanThumbnails, report.StaleThumbnails, report.YoungStaleThumbnails} {
		sort.Strings(list)
	}
	return leftovers, nil
}
