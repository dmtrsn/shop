package media

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// IDPattern is the managed media ID: "med_" and 128 random bits in hex. IDs
// are generated here, never derived from a file or product name, and never
// reused.
var IDPattern = regexp.MustCompile(`^med_[0-9a-f]{32}$`)

// NewID returns a fresh media ID from crypto/rand.
func NewID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("create media id: %w", err)
	}
	return "med_" + hex.EncodeToString(b), nil
}

// ErrStorage: the media store could not complete an operation. Callers map
// it to storage_unavailable; the wrapped cause (paths, errno) is for logs only.
var ErrStorage = errors.New("media storage unavailable")

// ErrNotStored: the store has no object for a valid ID.
var ErrNotStored = errors.New("media object not stored")

// Store is the narrow storage boundary of managed media. The filesystem
// implementation below is the only one; an object store could implement the
// same four operations later.
type Store interface {
	// Put stores data under id. It never overwrites an existing object.
	Put(id string, data []byte) error
	// Open returns the object for reading and its size.
	Open(id string) (io.ReadSeekCloser, int64, error)
	// Remove deletes the object; a missing object is not an error.
	Remove(id string) error
	// Objects lists every stored object and every leftover temporary file.
	Objects() ([]Object, error)
	// RemoveTemp deletes one leftover temporary file Objects reported.
	RemoveTemp(name string) error
}

// Object is one entry of the store as reconciliation sees it.
type Object struct {
	// ID is the media ID for a stored object, empty for a temporary file.
	ID string
	// Name is the entry's path relative to the root, for operator reports.
	Name     string
	Temp     bool
	Modified time.Time
}

// Layout under the root, all created and owned by the application:
//
//	objects/<first two hex digits of the ID>/<ID>   finished, immutable bytes
//	tmp/upload-<random>                             writes in progress
//
// Paths are built only from validated IDs and fixed names, so no client
// input can reach the filesystem.
const (
	objectsDir = "objects"
	tmpDir     = "tmp"
	tmpPrefix  = "upload-"
	dirMode    = 0o750
	fileMode   = 0o640
)

// FileStore keeps managed media in a directory the process owns.
type FileStore struct {
	root string
}

// OpenFileStore checks the configured root and prepares its layout. The root
// must be an absolute path to an existing directory (not a symlink) owned by
// this process and writable by it; anything else is a configuration error,
// so a misconfigured production server fails at startup instead of writing
// into an image layer or a working directory.
func OpenFileStore(root string) (*FileStore, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" {
		return nil, errors.New("MEDIA_ROOT must be a clean absolute path other than /")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("MEDIA_ROOT is not accessible: %w", err)
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("MEDIA_ROOT must be a directory, not a file or symlink")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return nil, fmt.Errorf("MEDIA_ROOT must be owned by the server user (uid %d)", os.Geteuid())
	}
	if info.Mode().Perm()&0o002 != 0 {
		return nil, errors.New("MEDIA_ROOT must not be world-writable")
	}
	store := &FileStore{root: root}
	for _, dir := range []string{objectsDir, tmpDir} {
		if err := os.Mkdir(filepath.Join(root, dir), dirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("MEDIA_ROOT is not writable: %w", err)
		}
	}
	probe, err := os.CreateTemp(filepath.Join(root, tmpDir), "probe-")
	if err != nil {
		return nil, fmt.Errorf("MEDIA_ROOT is not writable: %w", err)
	}
	probe.Close()
	if err := os.Remove(probe.Name()); err != nil {
		return nil, fmt.Errorf("MEDIA_ROOT is not writable: %w", err)
	}
	return store, nil
}

// objectPath is the only way an object path is formed: from a validated ID.
func (s *FileStore) objectPath(id string) (string, error) {
	if !IDPattern.MatchString(id) {
		return "", ErrNotStored
	}
	return filepath.Join(s.root, objectsDir, id[4:6], id), nil
}

func (s *FileStore) Put(id string, data []byte) error {
	target, err := s.objectPath(id)
	if err != nil {
		return fmt.Errorf("%w: invalid media id", ErrStorage)
	}
	if err := os.Mkdir(filepath.Dir(target), dirMode); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w: create object directory: %v", ErrStorage, err)
	}
	temp, err := os.CreateTemp(filepath.Join(s.root, tmpDir), tmpPrefix)
	if err != nil {
		return fmt.Errorf("%w: create temporary file: %v", ErrStorage, err)
	}
	// The temporary name goes away in every case; after a successful link
	// the object keeps the bytes under its final name.
	defer os.Remove(temp.Name())
	if err := writeSynced(temp, data); err != nil {
		temp.Close()
		return fmt.Errorf("%w: %v", ErrStorage, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("%w: close: %v", ErrStorage, err)
	}
	// A hard link fails if the target exists, so an ID is never overwritten;
	// the finished file appears under its final name complete or not at all.
	if err := os.Link(temp.Name(), target); err != nil {
		return fmt.Errorf("%w: publish object: %v", ErrStorage, err)
	}
	if dir, err := os.Open(filepath.Dir(target)); err == nil {
		dir.Sync()
		dir.Close()
	}
	return nil
}

func writeSynced(file *os.File, data []byte) error {
	if err := file.Chmod(fileMode); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

func (s *FileStore) Open(id string) (io.ReadSeekCloser, int64, error) {
	target, err := s.objectPath(id)
	if err != nil {
		return nil, 0, ErrNotStored
	}
	file, err := os.Open(target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, ErrNotStored
	}
	if err != nil {
		return nil, 0, fmt.Errorf("%w: open: %v", ErrStorage, err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, 0, fmt.Errorf("%w: stat object", ErrStorage)
	}
	return file, info.Size(), nil
}

func (s *FileStore) Remove(id string) error {
	target, err := s.objectPath(id)
	if err != nil {
		return nil
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: remove: %v", ErrStorage, err)
	}
	return nil
}

// Objects lists finished objects whose names and placement match the layout
// exactly, and temporary upload files. Anything else in the tree is not the
// store's and is never listed, so reconciliation can never remove it.
func (s *FileStore) Objects() ([]Object, error) {
	result := []Object{}
	shards, err := os.ReadDir(filepath.Join(s.root, objectsDir))
	if err != nil {
		return nil, fmt.Errorf("%w: list objects: %v", ErrStorage, err)
	}
	for _, shard := range shards {
		if !shard.IsDir() || len(shard.Name()) != 2 {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(s.root, objectsDir, shard.Name()))
		if err != nil {
			return nil, fmt.Errorf("%w: list objects: %v", ErrStorage, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !entry.Type().IsRegular() || !IDPattern.MatchString(name) || name[4:6] != shard.Name() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			result = append(result, Object{ID: name, Name: filepath.Join(objectsDir, shard.Name(), name), Modified: info.ModTime()})
		}
	}
	temps, err := os.ReadDir(filepath.Join(s.root, tmpDir))
	if err != nil {
		return nil, fmt.Errorf("%w: list temporary files: %v", ErrStorage, err)
	}
	for _, entry := range temps {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), tmpPrefix) {
			continue
		}
		if info, err := entry.Info(); err == nil {
			result = append(result, Object{Name: filepath.Join(tmpDir, entry.Name()), Temp: true, Modified: info.ModTime()})
		}
	}
	return result, nil
}

// RemoveTemp deletes one leftover temporary file reported by Objects.
func (s *FileStore) RemoveTemp(name string) error {
	base := filepath.Base(name)
	if name != filepath.Join(tmpDir, base) || !strings.HasPrefix(base, tmpPrefix) {
		return errors.New("not a temporary media file")
	}
	if err := os.Remove(filepath.Join(s.root, tmpDir, base)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: remove temporary file: %v", ErrStorage, err)
	}
	return nil
}
