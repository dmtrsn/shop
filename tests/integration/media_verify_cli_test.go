//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"sweetshop/internal/media"
)

func (d *database) verifyMediaCLI(t *testing.T, root string, apply bool) shopctlResult {
	t.Helper()
	migrations, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"media", "verify"}
	if apply {
		args = append(args, "--apply")
	}
	cmd := exec.CommandContext(d.ctx, shopctl(t), args...)
	cmd.Env = []string{"APP_MODE=demo", "DATABASE_URL=" + d.dsn(), "MIGRATIONS_DIR=" + migrations, "MEDIA_ROOT=" + root}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	result := shopctlResult{stdout: stdout.String(), stderr: stderr.String()}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		result.code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return result
}

type mediaEntrySnapshot struct {
	Mode     fs.FileMode
	Modified int64
	Data     string
}

// Directory mtimes also expose transient create/remove probes. File bytes,
// modes and mtimes must survive verification unchanged; access times may move
// when the command reads a file.
func snapshotMediaTree(t *testing.T, root string) map[string]mediaEntrySnapshot {
	t.Helper()
	result := map[string]mediaEntrySnapshot{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		state := mediaEntrySnapshot{Mode: info.Mode(), Modified: info.ModTime().UnixNano()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			state.Data = string(data)
		}
		result[path] = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func verifyReport(t *testing.T, result shopctlResult, code int) media.Report {
	t.Helper()
	if result.code != code {
		t.Fatalf("exit=%d want=%d stderr=%s stdout=%s", result.code, code, result.stderr, result.stdout)
	}
	var report media.Report
	if err := json.Unmarshal([]byte(result.stdout), &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func TestMediaVerifyCLIDoesNotInitializeMissingLayout(t *testing.T) {
	d := newDatabase(t, true)
	for _, layout := range []string{"empty", "missing root", "malformed"} {
		t.Run(layout, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "media")
			if layout != "missing root" {
				if err := os.Mkdir(root, 0o750); err != nil {
					t.Fatal(err)
				}
			}
			if layout == "malformed" {
				if err := os.WriteFile(filepath.Join(root, "objects"), []byte("not a directory"), 0o640); err != nil {
					t.Fatal(err)
				}
			}
			before := snapshotMediaTree(t, parent)
			result := d.verifyMediaCLI(t, root, false)
			if after := snapshotMediaTree(t, parent); !reflect.DeepEqual(before, after) {
				t.Fatalf("default verify changed storage: before=%v after=%v", before, after)
			}
			if layout == "missing root" {
				if result.code != 2 || !strings.Contains(result.stderr, "invalid MEDIA_ROOT") {
					t.Fatalf("missing root: %+v", result)
				}
			} else if result.code != 1 || !strings.Contains(result.stderr, "media storage unavailable") {
				t.Fatalf("missing/malformed layout must report storage failure: %+v", result)
			}
		})
	}
	// Apply keeps its writable opener and initializes the existing empty root.
	root := t.TempDir()
	report := verifyReport(t, d.verifyMediaCLI(t, root, true), 0)
	if report.Problems() || report.Healthy != 0 {
		t.Fatalf("empty apply report: %+v", report)
	}
	for _, dir := range []string{"objects", "thumbnails", "tmp"} {
		if info, err := os.Stat(filepath.Join(root, dir)); err != nil || !info.IsDir() {
			t.Fatalf("apply did not create %s: %v", dir, err)
		}
	}
}

func TestMediaVerifyCLIReadOnlyReportAndApplyCleanup(t *testing.T) {
	d := newDatabase(t, true)
	staff := d.auditStaff(t, "verify@example.com")
	healthy := d.uploadMedia(t, staff)
	want, err := d.mediaLibrary(t).Reconcile(d.ctx, 24*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Unix(1700000000, 0)
	// Give every directory a known mtime; even a removed writable probe would
	// change tmp's mtime and fail the snapshot comparison.
	for path, entry := range snapshotMediaTree(t, d.mediaRoot) {
		if entry.Mode.IsDir() {
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	tmp := filepath.Join(d.mediaRoot, "tmp")
	if err := os.Chmod(tmp, 0o550); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmp, 0o750) })
	before := snapshotMediaTree(t, d.mediaRoot)
	report := verifyReport(t, d.verifyMediaCLI(t, d.mediaRoot, false), 0)
	if report.Healthy != 1 || !reflect.DeepEqual(report, want) {
		t.Fatalf("healthy report changed: got=%+v want=%+v", report, want)
	}
	if after := snapshotMediaTree(t, d.mediaRoot); !reflect.DeepEqual(before, after) {
		t.Fatalf("default verify wrote files or probes: before=%v after=%v", before, after)
	}
	if err := os.Chmod(tmp, 0o750); err != nil {
		t.Fatal(err)
	}
	deleted := d.uploadMedia(t, staff)
	d.exec(t, `UPDATE media_assets SET deleted_at=now() WHERE id=$1`, deleted.ID)
	stale := filepath.Join(tmp, "upload-interrupted")
	if err := os.WriteFile(stale, []byte("unfinished"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	want, err = d.mediaLibrary(t).Reconcile(d.ctx, 24*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	before = snapshotMediaTree(t, d.mediaRoot)
	report = verifyReport(t, d.verifyMediaCLI(t, d.mediaRoot, false), 3)
	if !reflect.DeepEqual(report, want) || len(report.Deleted) != 1 || len(report.StaleTemp) != 1 || len(report.Removed) != 0 {
		t.Fatalf("dry-run classification changed: got=%+v want=%+v", report, want)
	}
	if after := snapshotMediaTree(t, d.mediaRoot); !reflect.DeepEqual(before, after) {
		t.Fatal("default verify changed leftovers")
	}
	report = verifyReport(t, d.verifyMediaCLI(t, d.mediaRoot, true), 0)
	if report.Healthy != 1 || len(report.Removed) != 2 || len(report.Failed) != 0 {
		t.Fatalf("apply cleanup changed: %+v", report)
	}
	for _, path := range []string{d.objectPath(deleted.ID), stale} {
		if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("apply left %s: %v", path, err)
		}
	}
	if _, err := os.Stat(d.objectPath(healthy.ID)); err != nil {
		t.Fatalf("apply removed live media: %v", err)
	}
}
