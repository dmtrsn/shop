package media

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
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

// MinGrace is the shortest grace period apply mode accepts.
const MinGrace = time.Hour

// Rows returns every media row: ID to whether it is logically deleted.
type Rows func(ctx context.Context) (map[string]bool, error)

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
}

// Problems reports whether anything needs attention (removable leftovers or
// missing objects).
func (r Report) Problems() bool {
	return len(r.Missing)+len(r.Orphans)+len(r.Deleted)+len(r.StaleTemp)+len(r.Failed) > 0
}

// Reconcile classifies the store against the rows. With apply it removes
// deleted rows' objects, orphans and temporary files older than grace; it
// never removes anything a live row references.
func Reconcile(ctx context.Context, store Store, rows Rows, now time.Time, grace time.Duration, apply bool) (Report, error) {
	if apply && grace < MinGrace {
		return Report{}, fmt.Errorf("grace period must be at least %s", MinGrace)
	}
	// Objects first: an upload that commits after this listing is then found
	// among the rows rather than reported as an orphan.
	objects, err := store.Objects()
	if err != nil {
		return Report{}, err
	}
	known, err := rows(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("read media rows: %w", err)
	}
	report := Report{Missing: []string{}, Orphans: []string{}, YoungOrphans: []string{}, Deleted: []string{}, StaleTemp: []string{}, YoungTemp: []string{}, Removed: []string{}, Failed: []string{}}
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
	if len(report.Failed) > 0 {
		return report, errors.New("some leftovers could not be removed")
	}
	return report, nil
}
