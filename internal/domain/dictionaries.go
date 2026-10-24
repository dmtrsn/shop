package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Staff management of the catalog dictionaries (ADR 018): categories,
// manufacturers, brands and collections. Rows are archived, never deleted;
// every change names the revision it was based on (ErrStaleRevision).

// ErrInvalidHierarchy: the category tree change is not allowed: a category
// as its own parent, a parent that is itself a subcategory, or a parent for a
// category that has subcategories (the tree is two levels deep). Nothing
// changed. A missing or archived parent is a ValidationError on parentId.
var ErrInvalidHierarchy = errors.New("category hierarchy change not allowed")

// ReferenceConflictError: the change would leave products, active brands or
// active subcategories pointing at a row they may no longer use (archiving a
// used row, moving a used brand to another manufacturer). It carries counts
// only.
type ReferenceConflictError struct {
	Products   int `json:"products"`
	Brands     int `json:"brands"`
	Categories int `json:"categories"`
}

func (e *ReferenceConflictError) Error() string {
	parts := []string{}
	if e.Products > 0 {
		parts = append(parts, fmt.Sprintf("товаров: %d", e.Products))
	}
	if e.Brands > 0 {
		parts = append(parts, fmt.Sprintf("активных брендов: %d", e.Brands))
	}
	if e.Categories > 0 {
		parts = append(parts, fmt.Sprintf("активных подкатегорий: %d", e.Categories))
	}
	return "Запись используется — " + strings.Join(parts, ", ")
}

// DictionaryStatus filters staff dictionary lists.
type DictionaryStatus string

const (
	DictionaryActive   DictionaryStatus = "active"
	DictionaryArchived DictionaryStatus = "archived"
	DictionaryAll      DictionaryStatus = "all"
)

// DictionaryState is what every staff dictionary view carries.
type DictionaryState struct {
	ID         string     `json:"id"`
	Revision   int64      `json:"revision"`
	Archived   bool       `json:"archived"`
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
	// ProductCount counts every product, archived or not, that refers to the row.
	ProductCount int `json:"productCount"`
}

type CategoryInput struct {
	Slug        string
	Name        string
	Description string
	// ParentID is "" for a top-level category; a parent must be top-level.
	ParentID  string
	Icon      string
	SortOrder int
}

type AdminCategory struct {
	DictionaryState
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    string `json:"parentId,omitempty"`
	Icon        string `json:"icon"`
	SortOrder   int    `json:"sortOrder"`
	ChildCount  int    `json:"childCount"`
}

type ManufacturerInput struct {
	Slug        string
	Name        string
	Country     string
	Description string
	SortOrder   int
}

// AdminManufacturer: Logo is read-only until media management exists.
type AdminManufacturer struct {
	DictionaryState
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Country     string `json:"country"`
	Description string `json:"description"`
	Logo        string `json:"logo"`
	SortOrder   int    `json:"sortOrder"`
	BrandCount  int    `json:"brandCount"`
}

type BrandInput struct {
	Slug           string
	Name           string
	ManufacturerID string
	Description    string
	SortOrder      int
}

type AdminBrand struct {
	DictionaryState
	Slug         string    `json:"slug"`
	Name         string    `json:"name"`
	Manufacturer Reference `json:"manufacturer"`
	Description  string    `json:"description"`
	Logo         string    `json:"logo"`
	SortOrder    int       `json:"sortOrder"`
}

type CollectionInput struct {
	Slug        string
	Name        string
	Description string
	Accent      string
	// StartsAt/EndsAt bound when the storefront lists the collection; nil is open.
	StartsAt  *time.Time
	EndsAt    *time.Time
	SortOrder int
}

// AdminCollection: Image is read-only until media management exists.
type AdminCollection struct {
	DictionaryState
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Accent      string     `json:"accent"`
	Image       string     `json:"image"`
	StartsAt    *time.Time `json:"startsAt,omitempty"`
	EndsAt      *time.Time `json:"endsAt,omitempty"`
	SortOrder   int        `json:"sortOrder"`
}
