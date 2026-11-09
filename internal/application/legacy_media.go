package application

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"sweetshop/internal/domain"
	"sweetshop/internal/media"
)

// ProductLegacyMediaRetire is the audit action of a product whose managed
// media is unchanged and whose legacy fallback was retired (ADR 021). A
// product whose images were imported logs ProductMediaUpdate instead.
const ProductLegacyMediaRetire = domain.AuditAction("product.legacy_media_retire")

// LegacyMediaStatus is what the migration found or did for one product.
type LegacyMediaStatus string

const (
	LegacyNeedsMigration  LegacyMediaStatus = "needs_migration"
	LegacyMigrated        LegacyMediaStatus = "migrated"
	LegacyAlreadyMigrated LegacyMediaStatus = "already_migrated"
	LegacyAlreadyManaged  LegacyMediaStatus = "already_managed"
	LegacyNoMedia         LegacyMediaStatus = "no_media"
	LegacyConflict        LegacyMediaStatus = "conflict"
	LegacyFailed          LegacyMediaStatus = "failed"
)

// Problem kinds of a product that could not be migrated.
const (
	LegacyMissingFile      = "missing_file"
	LegacyUnreadableFile   = "unreadable_file"
	LegacyInvalidImage     = "invalid_image"
	LegacyUnsupportedImage = "unsupported_image"
	LegacyUnsupportedPath  = "unsupported_path"
	LegacyGalleryTooLarge  = "gallery_too_large"
	LegacyStorageError     = "storage_error"
	LegacyDatabaseError    = "database_error"
	LegacyStateConflict    = "conflict"
)

// File retention categories.
const (
	LegacyFileProduct      = "product"
	LegacyFileOrder        = "historical_order"
	LegacyFileBoth         = "product_and_order"
	LegacyFileOther        = "other_consumer"
	LegacyFileUnreferenced = "unreferenced"
)

// LegacyMediaProblem is one reason a product was left on its legacy images.
type LegacyMediaProblem struct {
	Path   string `json:"path,omitempty"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// LegacyMediaImage is one planned or attached managed image.
type LegacyMediaImage struct {
	Source  string `json:"source"`
	SHA256  string `json:"sha256"`
	AssetID string `json:"assetId,omitempty"`
	// Reused means the bytes were already in the library.
	Reused bool `json:"reused,omitempty"`
}

type LegacyProductReport struct {
	ID            string            `json:"id"`
	Status        LegacyMediaStatus `json:"status"`
	LegacyImage   string            `json:"legacyImage,omitempty"`
	LegacyGallery []string          `json:"legacyGallery,omitempty"`
	// Retired reports the legacy fallback as retired after this run.
	Retired bool `json:"retired"`
	// Primary and Gallery are the planned mapping, then the attached one.
	Primary *LegacyMediaImage  `json:"primary,omitempty"`
	Gallery []LegacyMediaImage `json:"gallery,omitempty"`
	// Duplicates lists sources merged into another image with the same bytes.
	Duplicates []string             `json:"duplicates,omitempty"`
	Note       string               `json:"note,omitempty"`
	Problems   []LegacyMediaProblem `json:"problems,omitempty"`
}

type LegacyMediaSummary struct {
	Products        int `json:"products"`
	NeedsMigration  int `json:"needsMigration"`
	Migrated        int `json:"migrated"`
	AlreadyMigrated int `json:"alreadyMigrated"`
	AlreadyManaged  int `json:"alreadyManaged"`
	NoMedia         int `json:"noMedia"`
	Conflict        int `json:"conflict"`
	Failed          int `json:"failed"`
}

// LegacyFileReport is the retention entry of one /assets path. Only the
// unreferenced category is eligible for physical removal.
type LegacyFileReport struct {
	Path                  string `json:"path"`
	Exists                bool   `json:"exists"`
	Category              string `json:"category"`
	ActiveProducts        int    `json:"activeProducts"`
	CompatibilityProducts int    `json:"compatibilityProducts"`
	HistoricalOrders      int    `json:"historicalOrders"`
	OtherCatalogRows      int    `json:"otherCatalogRows"`
	StaticReference       bool   `json:"staticReference"`
}

// LegacyMediaReport is the full JSON report of one run.
type LegacyMediaReport struct {
	Mode     string                `json:"mode"`
	Summary  LegacyMediaSummary    `json:"summary"`
	Products []LegacyProductReport `json:"products"`
	// ActiveLegacyProducts are products still rendered from legacy paths.
	ActiveLegacyProducts []string           `json:"activeLegacyProducts"`
	Files                []LegacyFileReport `json:"files"`
	// MissingFiles are referenced paths with no file behind them.
	MissingFiles []string `json:"missingFiles"`
	// SafeToDelete is empty unless the static web root was scanned.
	SafeToDelete  []string `json:"safeToDelete"`
	StaticScanned bool     `json:"staticScanned"`
}

// Pending reports products that still need operator attention.
func (r LegacyMediaReport) Pending() bool {
	return r.Summary.NeedsMigration > 0 || r.Summary.Conflict > 0 || r.Summary.Failed > 0
}

func (s *LegacyMediaSummary) add(status LegacyMediaStatus) {
	s.Products++
	switch status {
	case LegacyNeedsMigration:
		s.NeedsMigration++
	case LegacyMigrated:
		s.Migrated++
	case LegacyAlreadyMigrated:
		s.AlreadyMigrated++
	case LegacyAlreadyManaged:
		s.AlreadyManaged++
	case LegacyNoMedia:
		s.NoMedia++
	case LegacyConflict:
		s.Conflict++
	case LegacyFailed:
		s.Failed++
	}
}

// LegacyMediaStore is the metadata port of the migration; only PostgreSQL
// implements it.
type LegacyMediaStore interface {
	LockLegacyMigration(ctx context.Context) (func(), error)
	LegacyMediaProducts(ctx context.Context) ([]domain.LegacyMediaProduct, error)
	LiveMediaBySHA256(ctx context.Context, sum string) (domain.StoredMedia, error)
	MigrateLegacyProductMedia(ctx context.Context, mutation domain.StaffMutation, expected domain.LegacyMediaProduct, value domain.ProductMedia) error
	RetireLegacyProductMedia(ctx context.Context, mutation domain.StaffMutation, productID string) error
	LegacyMediaReferences(ctx context.Context) (map[string]*domain.LegacyPathReferences, error)
}

// legacyPathPattern is the only legacy reference the migration resolves: one
// file directly under /assets. Nothing else ever reaches the filesystem.
var legacyPathPattern = regexp.MustCompile(`^/assets/[A-Za-z0-9_][A-Za-z0-9._-]*$`)

// LegacyMediaMigration imports legacy product images into the managed
// library through the upload pipeline (ADR 021).
type LegacyMediaMigration struct {
	store  LegacyMediaStore
	media  *Media
	assets string
	logger *slog.Logger
}

func NewLegacyMediaMigration(store LegacyMediaStore, library *Media, assetsDir string, logger *slog.Logger) *LegacyMediaMigration {
	return &LegacyMediaMigration{store: store, media: library, assets: assetsDir, logger: logger}
}

// legacySource is the outcome for one path, shared by every product using it,
// so a file shared by the catalog is decoded once and imported once.
type legacySource struct {
	digest   string
	assetID  string
	reused   bool
	resolved bool
	problem  *LegacyMediaProblem
}

type legacyRun struct {
	*LegacyMediaMigration
	apply   bool
	upload  domain.StaffMutation
	attach  domain.StaffMutation
	retire  domain.StaffMutation
	sources map[string]*legacySource
}

// Run classifies every product and, with apply, migrates them one at a time.
// A product failure is recorded and never stops the others; the returned
// error means the run itself could not continue, and the report is partial.
func (m *LegacyMediaMigration) Run(ctx context.Context, apply bool, staff domain.StaffUser, requestID string) (LegacyMediaReport, error) {
	report := LegacyMediaReport{Mode: "dry-run", Products: []LegacyProductReport{}, ActiveLegacyProducts: []string{},
		Files: []LegacyFileReport{}, MissingFiles: []string{}, SafeToDelete: []string{}}
	run := &legacyRun{LegacyMediaMigration: m, apply: apply, sources: map[string]*legacySource{}}
	if apply {
		report.Mode = "apply"
		var err error
		if run.upload, err = domain.NewStaffMutation(staff, MediaUpload, requestID); err != nil {
			return report, err
		}
		if run.attach, err = domain.NewStaffMutation(staff, ProductMediaUpdate, requestID); err != nil {
			return report, err
		}
		if run.retire, err = domain.NewStaffMutation(staff, ProductLegacyMediaRetire, requestID); err != nil {
			return report, err
		}
		release, err := m.store.LockLegacyMigration(ctx)
		if err != nil {
			return report, err
		}
		defer release()
	}
	products, err := m.store.LegacyMediaProducts(ctx)
	if err != nil {
		return report, err
	}
	for _, product := range products {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		entry := run.product(ctx, product)
		report.Products = append(report.Products, entry)
		report.Summary.add(entry.Status)
		switch entry.Status {
		case LegacyNeedsMigration, LegacyConflict, LegacyFailed:
			report.ActiveLegacyProducts = append(report.ActiveLegacyProducts, entry.ID)
		}
	}
	return report, m.retention(ctx, &report)
}

func (r *legacyRun) product(ctx context.Context, p domain.LegacyMediaProduct) LegacyProductReport {
	entry := LegacyProductReport{ID: p.ID, LegacyImage: p.Image, LegacyGallery: p.Gallery, Retired: p.Retired}
	managed := p.ManagedPrimary || p.ManagedGallery > 0
	legacy := p.Image != "" || len(p.Gallery) > 0
	switch {
	case p.Retired:
		entry.Status = LegacyAlreadyMigrated
		return entry
	case p.ManagedPrimary || (managed && p.Image == ""):
		entry.Status = LegacyAlreadyManaged
		if !legacy {
			return entry
		}
		entry.Note = "managed media wins; legacy fallback data remains in the compatibility columns"
		if r.apply {
			if err := r.store.RetireLegacyProductMedia(ctx, r.retire, p.ID); err != nil {
				r.failProduct(&entry, err)
				return entry
			}
			entry.Retired = true
		}
		return entry
	case managed:
		entry.Status = LegacyConflict
		entry.Problems = []LegacyMediaProblem{{Path: p.Image, Kind: LegacyStateConflict,
			Detail: "the product has a managed gallery but its primary image is still legacy; choose a managed primary, then rerun"}}
		return entry
	case !legacy:
		entry.Status = LegacyNoMedia
		return entry
	}

	type image struct {
		path   string
		source *legacySource
		data   *media.Normalized
	}
	images := []image{}
	for _, path := range legacyPaths(p.Image, p.Gallery) {
		source, data, problem := r.prepare(ctx, path)
		if problem != nil {
			entry.Problems = append(entry.Problems, *problem)
			continue
		}
		images = append(images, image{path, source, data})
	}
	if len(entry.Problems) > 0 {
		entry.Status = LegacyFailed
		return entry
	}

	// Different paths with identical bytes are one image: the first
	// occurrence keeps its place, because references must not repeat.
	seen := map[string]bool{}
	unique := []image{}
	for _, item := range images {
		if seen[item.source.digest] {
			entry.Duplicates = append(entry.Duplicates, item.path)
			continue
		}
		seen[item.source.digest] = true
		unique = append(unique, item)
	}
	gallery := unique
	if p.Image != "" {
		gallery = unique[1:]
	}
	if len(gallery) > MaxGalleryImages {
		entry.Status = LegacyFailed
		entry.Problems = []LegacyMediaProblem{{Kind: LegacyGalleryTooLarge,
			Detail: fmt.Sprintf("the legacy gallery has %d distinct images; the managed limit is %d", len(gallery), MaxGalleryImages)}}
		return entry
	}

	for _, item := range unique {
		if problem := r.ensureAsset(ctx, item.source, item.data); problem != nil {
			problem.Path = item.path
			entry.Problems = append(entry.Problems, *problem)
		}
	}
	report := func(item image) LegacyMediaImage {
		return LegacyMediaImage{Source: item.path, SHA256: item.source.digest, AssetID: item.source.assetID, Reused: item.source.reused}
	}
	if p.Image != "" {
		primary := report(unique[0])
		entry.Primary = &primary
	}
	for _, item := range gallery {
		entry.Gallery = append(entry.Gallery, report(item))
	}
	if len(entry.Problems) > 0 {
		entry.Status = LegacyFailed
		return entry
	}
	if !r.apply {
		entry.Status = LegacyNeedsMigration
		return entry
	}
	value := domain.ProductMedia{ProductID: p.ID, GalleryIDs: []string{}}
	if entry.Primary != nil {
		value.PrimaryID = entry.Primary.AssetID
	}
	for _, item := range entry.Gallery {
		value.GalleryIDs = append(value.GalleryIDs, item.AssetID)
	}
	if err := r.store.MigrateLegacyProductMedia(ctx, r.attach, p, value); err != nil {
		r.failProduct(&entry, err)
		return entry
	}
	entry.Status = LegacyMigrated
	entry.Retired = true
	return entry
}

// failProduct records a store failure; the product keeps its previous state.
func (r *legacyRun) failProduct(entry *LegacyProductReport, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrNotFound), errors.As(err, &validation):
		entry.Status = LegacyConflict
		entry.Problems = []LegacyMediaProblem{{Kind: LegacyStateConflict,
			Detail: "the product or a selected image changed during the migration; rerun to pick up the new state"}}
	default:
		r.logger.Error("legacy media migration failed", "product", entry.ID, "error", err)
		entry.Status = LegacyFailed
		entry.Problems = []LegacyMediaProblem{{Kind: LegacyDatabaseError, Detail: "the product could not be saved; see the log"}}
	}
}

// legacyPaths is the exact normalization of the legacy columns: the primary
// first, then the gallery in stored order, without empty entries or repeats.
func legacyPaths(image string, gallery []string) []string {
	paths := []string{}
	for _, path := range append([]string{image}, gallery...) {
		if path != "" && !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	return paths
}

// prepare validates and normalizes one legacy file. The normalized bytes are
// returned only while they still have to be imported.
func (r *legacyRun) prepare(ctx context.Context, path string) (*legacySource, *media.Normalized, *LegacyMediaProblem) {
	source := r.sources[path]
	if source != nil && source.problem != nil {
		return nil, nil, source.problem
	}
	if source != nil && (!r.apply || source.assetID != "") {
		return source, nil, nil
	}
	normalized, problem := r.load(ctx, path)
	if problem != nil {
		r.sources[path] = &legacySource{problem: problem}
		return nil, nil, problem
	}
	if source == nil {
		source = &legacySource{}
		r.sources[path] = source
	}
	source.digest = normalized.SHA256
	return source, normalized, nil
}

func (m *LegacyMediaMigration) load(ctx context.Context, path string) (*media.Normalized, *LegacyMediaProblem) {
	if !legacyPathPattern.MatchString(path) {
		return nil, &LegacyMediaProblem{Path: path, Kind: LegacyUnsupportedPath, Detail: "not a single file directly under /assets"}
	}
	data, problem := m.read(path)
	if problem != nil {
		return nil, problem
	}
	normalized, err := m.media.normalize(ctx, data)
	var invalid *media.InvalidImageError
	switch {
	case err == nil:
		return &normalized, nil
	case errors.Is(err, media.ErrTooLarge):
		return nil, &LegacyMediaProblem{Path: path, Kind: LegacyUnsupportedImage, Detail: "the file is larger than the upload limit"}
	case errors.Is(err, media.ErrUnsupported):
		return nil, &LegacyMediaProblem{Path: path, Kind: LegacyUnsupportedImage, Detail: "not a still JPEG, PNG or WebP image"}
	case errors.As(err, &invalid):
		return nil, &LegacyMediaProblem{Path: path, Kind: LegacyInvalidImage, Detail: invalid.Reason}
	}
	m.logger.Error("legacy media normalization failed", "path", path, "error", err)
	return nil, &LegacyMediaProblem{Path: path, Kind: LegacyInvalidImage, Detail: "the image could not be normalized; see the log"}
}

func (m *LegacyMediaMigration) read(path string) ([]byte, *LegacyMediaProblem) {
	target := filepath.Join(m.assets, strings.TrimPrefix(path, "/assets/"))
	info, err := os.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, &LegacyMediaProblem{Path: path, Kind: LegacyMissingFile, Detail: "the file does not exist"}
	}
	if err != nil {
		m.logger.Error("legacy media stat failed", "path", path, "error", err)
		return nil, &LegacyMediaProblem{Path: path, Kind: LegacyUnreadableFile, Detail: "the file cannot be read; see the log"}
	}
	if !info.Mode().IsRegular() {
		return nil, &LegacyMediaProblem{Path: path, Kind: LegacyUnreadableFile, Detail: "not a regular file"}
	}
	file, err := os.Open(target)
	if err != nil {
		m.logger.Error("legacy media open failed", "path", path, "error", err)
		return nil, &LegacyMediaProblem{Path: path, Kind: LegacyUnreadableFile, Detail: "the file cannot be read; see the log"}
	}
	defer file.Close()
	// One byte over the limit is enough for Normalize to refuse the file.
	data, err := io.ReadAll(io.LimitReader(file, media.MaxUploadBytes+1))
	if err != nil {
		m.logger.Error("legacy media read failed", "path", path, "error", err)
		return nil, &LegacyMediaProblem{Path: path, Kind: LegacyUnreadableFile, Detail: "the file cannot be read; see the log"}
	}
	return data, nil
}

// ensureAsset finds a live asset with these normalized bytes or, in apply
// mode, imports them through the upload path. An interrupted run resumes
// here: the asset it left behind is found by its digest and reused.
func (r *legacyRun) ensureAsset(ctx context.Context, source *legacySource, data *media.Normalized) *LegacyMediaProblem {
	if source.assetID != "" || (source.resolved && !r.apply) {
		return nil
	}
	stored, err := r.store.LiveMediaBySHA256(ctx, source.digest)
	switch {
	case err == nil:
		if err := r.media.verifyStored(stored); err != nil {
			r.logger.Error("reusable media object is damaged", "media", stored.ID, "error", err)
			return &LegacyMediaProblem{Kind: LegacyStorageError, Detail: "an asset with these bytes exists but its stored file is missing or damaged; run shopctl media verify"}
		}
		source.assetID, source.reused = stored.ID, true
	case errors.Is(err, domain.ErrNotFound):
		if !r.apply {
			source.resolved = true
			return nil
		}
		asset, err := r.media.register(ctx, r.upload, *data)
		if errors.Is(err, domain.ErrStorageUnavailable) {
			return &LegacyMediaProblem{Kind: LegacyStorageError, Detail: "the media store could not save the image; see the log"}
		}
		if err != nil {
			r.logger.Error("legacy media registration failed", "error", err)
			return &LegacyMediaProblem{Kind: LegacyDatabaseError, Detail: "the asset could not be registered; see the log"}
		}
		source.assetID = asset.ID
	default:
		r.logger.Error("legacy media lookup failed", "error", err)
		return &LegacyMediaProblem{Kind: LegacyDatabaseError, Detail: "the asset library could not be searched; see the log"}
	}
	return nil
}

// retention builds the file inventory after the run. It deletes nothing:
// removal stays a separate operator decision (ADR 021).
func (m *LegacyMediaMigration) retention(ctx context.Context, report *LegacyMediaReport) error {
	references, err := m.store.LegacyMediaReferences(ctx)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(m.assets)
	if err != nil {
		return fmt.Errorf("list legacy assets: %w", err)
	}
	physical := map[string]bool{}
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			physical["/assets/"+entry.Name()] = true
		}
	}
	static, scanned, err := staticWebText(filepath.Dir(m.assets))
	if err != nil {
		return err
	}
	report.StaticScanned = scanned
	paths := []string{}
	for path := range physical {
		paths = append(paths, path)
	}
	for path := range references {
		if !physical[path] {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	for _, path := range paths {
		refs := domain.LegacyPathReferences{}
		if known := references[path]; known != nil {
			refs = *known
		}
		file := LegacyFileReport{Path: path, Exists: physical[path], ActiveProducts: refs.Active, CompatibilityProducts: refs.Compatibility,
			HistoricalOrders: refs.Orders, OtherCatalogRows: refs.Other, StaticReference: strings.Contains(static, strings.TrimPrefix(path, "/assets/"))}
		switch {
		case refs.Active > 0 && refs.Orders > 0:
			file.Category = LegacyFileBoth
		case refs.Active > 0:
			file.Category = LegacyFileProduct
		case refs.Orders > 0:
			file.Category = LegacyFileOrder
		case refs.Other > 0 || file.StaticReference:
			file.Category = LegacyFileOther
		default:
			file.Category = LegacyFileUnreferenced
		}
		report.Files = append(report.Files, file)
		if !file.Exists && (refs.Active > 0 || refs.Orders > 0 || refs.Compatibility > 0 || refs.Other > 0) {
			report.MissingFiles = append(report.MissingFiles, path)
		}
		if file.Exists && file.Category == LegacyFileUnreferenced && scanned {
			report.SafeToDelete = append(report.SafeToDelete, path)
		}
	}
	return nil
}

// staticWebText reads the static storefront next to the assets. A match by
// file name anywhere in it keeps a file, so a false match only keeps more. A
// directory without index.html is not the web root and proves nothing.
func staticWebText(root string) (string, bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false, fmt.Errorf("list static web root: %w", err)
	}
	found := false
	var text strings.Builder
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !slices.Contains([]string{".html", ".js", ".css"}, filepath.Ext(entry.Name())) {
			continue
		}
		found = found || entry.Name() == "index.html"
		data, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			return "", false, fmt.Errorf("read static web file: %w", err)
		}
		text.Write(data)
	}
	return text.String(), found, nil
}
