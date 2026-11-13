package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMediaVerifyDoesNotInitializeStorage(t *testing.T) {
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
			before := mediaTree(t, parent)
			t.Setenv("APP_MODE", "production")
			t.Setenv("DATABASE_URL", "postgres://example@127.0.0.1:1/shop?connect_timeout=1")
			t.Setenv("MIGRATIONS_DIR", "../../migrations")
			t.Setenv("MEDIA_ROOT", root)
			var stdout, stderr bytes.Buffer
			code := mediaCommand([]string{"verify"}, &stdout, &stderr)
			if code != exitFailure && code != exitUsage {
				t.Fatalf("exit=%d stderr=%s", code, stderr.String())
			}
			if after := mediaTree(t, parent); !reflect.DeepEqual(before, after) {
				t.Fatalf("default verify changed storage: before=%v after=%v", before, after)
			}
			if layout == "missing root" && (code != exitUsage || !strings.Contains(stderr.String(), "invalid MEDIA_ROOT")) {
				t.Fatalf("missing root: exit=%d stderr=%s", code, stderr.String())
			}
		})
	}
}

func mediaTree(t *testing.T, root string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}
