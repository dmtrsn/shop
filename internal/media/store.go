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
// same operations later.
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
	// PutThumbnail stores a thumbnail of id under its digest. It never
	// overwrites an existing thumbnail.
	PutThumbnail(id, sum string, data []byte) error
	// ReplaceThumbnail stores a thumbnail of id under its digest, atomically
	// replacing a file already there. A digest names one content, so this
	// repairs a damaged file and leaves an intact one as it was.
	ReplaceThumbnail(id, sum string, data []byte) error
	// OpenThumbnail returns one thumbnail for reading and its size.
	OpenThumbnail(id, sum string) (io.ReadSeekCloser, int64, error)
	// RemoveThumbnails deletes every thumbnail of id; none is not an error.
	RemoveThumbnails(id string) error
	// RemoveThumbnail deletes one thumbnail; a missing file is not an error.
	RemoveThumbnail(id, sum string) error
	// Thumbnails lists every stored thumbnail.
	Thumbnails() ([]ThumbnailFile, error)
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

// ThumbnailFile is one stored thumbnail as reconciliation sees it.
type ThumbnailFile struct {
	ID       string
	SHA256   string
	Modified time.Time
}

// Layout under the root, all created and owned by the application:
//
//	objects/<first two hex digits of the ID>/<ID>   finished, immutable bytes
//	thumbnails/<two hex digits>/<ID>/<SHA-256>      derived images (ADR 022)
//	tmp/upload-<random>                             writes in progress
//
// Paths are built only from validated IDs, digests and fixed names, so no
// client input can reach the filesystem. Thumbnails stay outside objects/:
// reconciliation removes any object without a row, including in older binaries.
const (
	objectsDir    = "objects"
	thumbnailsDir = "thumbnails"
	tmpDir        = "tmp"
	tmpPrefix     = "upload-"
	dirMode       = 0o750
	fileMode      = 0o640
)

// ThumbnailDigestPattern is the lowercase SHA-256 that names a thumbnail, in
// its file name and in its URL.
var ThumbnailDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// FileStore keeps managed media in a directory the process owns.
type FileStore struct {
	root string
	// Per-store seam for testing failures after publication without changing
	// filesystem operations or sharing mutable hooks between stores.
	syncDirectory func(string) error
}

// OpenFileStore checks the configured root and prepares its layout. The root
// must be an absolute path to an existing directory (not a symlink) owned by
// this process and writable by it; anything else is a configuration error,
// so a misconfigured production server fails at startup instead of writing
// into an image layer or a working directory.
func OpenFileStore(root string) (*FileStore, error) {
	return openFileStore(root, false)
}

// OpenFileStoreReadOnly validates the root like OpenFileStore but creates and
// probes nothing, for a report that must not touch the store.
func OpenFileStoreReadOnly(root string) (*FileStore, error) {
	return openFileStore(root, true)
}

func openFileStore(root string, readOnly bool) (*FileStore, error) {
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
	store := &FileStore{root: root, syncDirectory: syncDir}
	if readOnly {
		return store, nil
	}
	for _, dir := range []string{objectsDir, thumbnailsDir, tmpDir} {
		if err := os.Mkdir(filepath.Join(root, dir), dirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("MEDIA_ROOT is not writable: %w", err)
		}
	}
	// Make the layout entries durable, including entries left by a failed
	// earlier startup, before any publication can be registered in the DB.
	if err := store.syncDirectory(root); err != nil {
		return nil, fmt.Errorf("sync MEDIA_ROOT layout: %w", err)
	}
	probe, err := os.CreateTemp(filepath.Join(root, tmpDir), "probe-")
	if err != nil {
		return nil, fmt.Errorf("MEDIA_ROOT is not writable: %w", err)
	}
	if err := probe.Close(); err != nil {
		_ = os.Remove(probe.Name())
		return nil, fmt.Errorf("MEDIA_ROOT probe close: %w", err)
	}
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
	if err := s.publish(target, data); err != nil {
		return err
	}
	// Sync the shard entry even if it existed: another writer (or an earlier
	// failed sync) may have created it without making its parent durable yet.
	if err := s.syncDirectory(filepath.Join(s.root, objectsDir)); err != nil {
		return fmt.Errorf("%w: sync object shard parent: %w", ErrStorage, err)
	}
	return nil
}

// thumbnailDir is the only way a thumbnail path is formed: from a validated ID.
func (s *FileStore) thumbnailDir(id string) (string, error) {
	if !IDPattern.MatchString(id) {
		return "", ErrNotStored
	}
	return filepath.Join(s.root, thumbnailsDir, id[4:6], id), nil
}

// thumbnailPath is the only way a thumbnail file path is formed.
func (s *FileStore) thumbnailPath(id, sum string) (string, error) {
	dir, err := s.thumbnailDir(id)
	if err != nil || !ThumbnailDigestPattern.MatchString(sum) {
		return "", ErrNotStored
	}
	return filepath.Join(dir, sum), nil
}

func (s *FileStore) PutThumbnail(id, sum string, data []byte) error {
	return s.writeThumbnail(id, sum, data, os.Link)
}

func (s *FileStore) ReplaceThumbnail(id, sum string, data []byte) error {
	return s.writeThumbnail(id, sum, data, os.Rename)
}

func (s *FileStore) writeThumbnail(id, sum string, data []byte, place func(oldpath, newpath string) error) error {
	target, err := s.thumbnailPath(id, sum)
	if err != nil {
		return fmt.Errorf("%w: invalid thumbnail name", ErrStorage)
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("%w: create thumbnail directory: %v", ErrStorage, err)
	}
	if err := s.write(target, data, place); err != nil {
		return err
	}
	// Persist both the per-asset and shard entries. Existing directories may
	// still need this after another writer or an earlier failed sync.
	for _, parent := range []string{filepath.Dir(dir), filepath.Join(s.root, thumbnailsDir)} {
		if err := s.syncDirectory(parent); err != nil {
			return fmt.Errorf("%w: sync thumbnail parent: %w", ErrStorage, err)
		}
	}
	return nil
}

func (s *FileStore) RemoveThumbnails(id string) error {
	dir, err := s.thumbnailDir(id)
	if err != nil {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("%w: remove thumbnails: %v", ErrStorage, err)
	}
	return nil
}

// publish durably writes data to a temporary file and links it to target.
// A hard link fails if the target exists, so an ID is never overwritten.
func (s *FileStore) publish(target string, data []byte) error {
	return s.write(target, data, os.Link)
}

// write durably writes data to a temporary file and places it at target; the
// finished file appears under its final name complete or not at all.
func (s *FileStore) write(target string, data []byte, place func(oldpath, newpath string) error) error {
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
	if err := place(temp.Name(), target); err != nil {
		return fmt.Errorf("%w: publish object: %v", ErrStorage, err)
	}
	// Publication has happened. On sync failure retain the final name: callers
	// must see the error, but a recoverable file is safer than deleting bytes
	// that may already be durable (including thumbnail replacements).
	if err := s.syncDirectory(filepath.Dir(target)); err != nil {
		return fmt.Errorf("%w: sync published file directory: %w", ErrStorage, err)
	}
	return nil
}

func syncDir(path string) error {
	return syncDirWithOpen(path, os.Open)
}

func syncDirWithOpen(path string, open func(string) (*os.File, error)) error {
	dir, err := open(path)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	// Close even when Sync fails, preserving either or both errors.
	return errors.Join(dir.Sync(), dir.Close())
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
	return openRegular(target)
}

func (s *FileStore) OpenThumbnail(id, sum string) (io.ReadSeekCloser, int64, error) {
	target, err := s.thumbnailPath(id, sum)
	if err != nil {
		return nil, 0, ErrNotStored
	}
	return openRegular(target)
}

func openRegular(target string) (io.ReadSeekCloser, int64, error) {
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

// RemoveThumbnail deletes one file and then the asset's directory if that
// emptied it. A directory that still holds a file simply stays.
func (s *FileStore) RemoveThumbnail(id, sum string) error {
	target, err := s.thumbnailPath(id, sum)
	if err != nil {
		return nil
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: remove thumbnail: %v", ErrStorage, err)
	}
	_ = os.Remove(filepath.Dir(target))
	return nil
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

// Thumbnails lists files whose names and placement match the layout exactly.
// As with Objects, anything else in the tree is never listed and so can never
// be removed. A tree that does not exist yet, as on a root last opened by a
// binary from before thumbnails, holds none; a directory removed while it is
// being listed is equally none.
func (s *FileStore) Thumbnails() ([]ThumbnailFile, error) {
	result := []ThumbnailFile{}
	base := filepath.Join(s.root, thumbnailsDir)
	list := func(path string) ([]fs.DirEntry, error) {
		entries, err := os.ReadDir(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: list thumbnails: %v", ErrStorage, err)
		}
		return entries, nil
	}
	shards, err := list(base)
	if err != nil {
		return nil, err
	}
	for _, shard := range shards {
		if !shard.IsDir() || len(shard.Name()) != 2 {
			continue
		}
		assets, err := list(filepath.Join(base, shard.Name()))
		if err != nil {
			return nil, err
		}
		for _, asset := range assets {
			id := asset.Name()
			if !asset.IsDir() || !IDPattern.MatchString(id) || id[4:6] != shard.Name() {
				continue
			}
			files, err := list(filepath.Join(base, shard.Name(), id))
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				if !file.Type().IsRegular() || !ThumbnailDigestPattern.MatchString(file.Name()) {
					continue
				}
				if info, err := file.Info(); err == nil {
					result = append(result, ThumbnailFile{ID: id, SHA256: file.Name(), Modified: info.ModTime()})
				}
			}
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
