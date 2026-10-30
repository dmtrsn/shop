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
