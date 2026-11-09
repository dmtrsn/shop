package domain

import (
	"errors"
	"time"
)

// Managed product media (ADR 019). An asset is one immutable normalized
// image: its bytes live in the media store, its metadata here. Assets are
// tombstoned, never removed, and their IDs are never reused.

var (
	// ErrMediaTooLarge: the upload exceeds the size limit (413).
	ErrMediaTooLarge = errors.New("media upload too large")
	// ErrUnsupportedMedia: the upload is not a supported still image (415).
	ErrUnsupportedMedia = errors.New("unsupported media type")
	// ErrStorageUnavailable: the media store failed (503); nothing was
	// registered. Details are logged, never returned.
	ErrStorageUnavailable = errors.New("media storage unavailable")
)

// MediaAsset is the staff view of a live asset. Every field is derived from
// the stored bytes; nothing comes from the client.
type MediaAsset struct {
	ID          string    `json:"id"`
	ContentType string    `json:"contentType"`
	SizeBytes   int64     `json:"sizeBytes"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"createdAt"`
	// URL is the public delivery path, /media/{id}.
	URL string `json:"url"`
	// ProductCount counts products that reference the asset.
	ProductCount int `json:"productCount"`
	OrderCount   int `json:"orderCount"`
}

// MediaThumbnail describes the derived thumbnail of one asset (ADR 022). Every
// field is computed from the thumbnail's bytes or names the recipe that made it.
type MediaThumbnail struct {
	Recipe      string
	ContentType string
	SizeBytes   int64
	Width       int
	Height      int
	SHA256      string
}

// MediaPage is one page of live assets, newest first.
type MediaPage struct {
	Items      []MediaAsset `json:"items"`
	Total      int          `json:"total"`
	Page       int          `json:"page"`
	Limit      int          `json:"limit"`
	TotalPages int          `json:"totalPages"`
}

// StoredMedia is what the public route needs to serve one live asset.
type StoredMedia struct {
	ID          string
	ContentType string
	SizeBytes   int64
	SHA256      string
}

// ProductMediaRole: a product has at most one primary image and an ordered
// gallery.
type ProductMediaRole string

const (
	ProductMediaPrimary ProductMediaRole = "primary"
	ProductMediaGallery ProductMediaRole = "gallery"
)

// ProductMedia is a product's managed images: PrimaryID ("" for none) and the
// gallery in display order.
type ProductMedia struct {
	ProductID  string   `json:"productId"`
	PrimaryID  string   `json:"primaryId,omitempty"`
	GalleryIDs []string `json:"galleryIds"`
}

// ProductMediaItem contains only public, immutable image metadata.
type ProductMediaItem struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ContentType string `json:"contentType"`
	Position    int    `json:"position"`
}

type ManagedMedia struct {
	Primary *ProductMediaItem  `json:"primary"`
	Gallery []ProductMediaItem `json:"gallery"`
}

// ResolveProductMedia preserves legacy semantics until any live managed
// reference exists. A managed primary with an empty gallery never falls back
// to the legacy gallery. Detaching all references restores legacy behavior,
// until the product's legacy media is retired (ADR 021): from then on only
// managed media counts, and a product with none has no images.
func ResolveProductMedia(image string, gallery []string, managed *ManagedMedia, legacyRetired bool) (string, []string) {
	if legacyRetired {
		image, gallery = "", []string{}
	}
	if managed == nil || (managed.Primary == nil && len(managed.Gallery) == 0) {
		return image, gallery
	}
	if managed.Primary != nil {
		image = managed.Primary.URL
	}
	gallery = make([]string, 0, len(managed.Gallery))
	for _, item := range managed.Gallery {
		gallery = append(gallery, item.URL)
	}
	return image, gallery
}

// LegacyMediaProduct is what migrate-legacy needs to classify one product.
type LegacyMediaProduct struct {
	ID             string
	Image          string
	Gallery        []string
	Retired        bool
	ManagedPrimary bool
	ManagedGallery int
}

// LegacyPathReferences counts the database consumers of one /assets path.
type LegacyPathReferences struct {
	// Active products have not been cut over and can still render the path.
	Active int
	// Compatibility products are cut over and keep the path only in their
	// legacy columns; only a rollback would read it.
	Compatibility int
	Orders        int
	// Other counts non-product catalog rows (collection images, brand and
	// manufacturer logos).
	Other int
}
