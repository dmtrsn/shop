package domain

import (
	"errors"
	"fmt"
	"time"
)

// Staff product management (ADR 013). Products are archived, never deleted,
// and every staff change names the revision it was based on.

// ErrStaleRevision: the product changed after the client read it. Nothing
// was written.
var ErrStaleRevision = errors.New("product revision is stale")

// ProductInput is every field staff set on create and replace on update.
// Rating, reviews, currency and media are not staff-editable here.
type ProductInput struct {
	Slug            string
	SKU             string
	Name            string
	ShortName       string
	Description     string
	PriceMinor      int64
	OldPriceMinor   int64
	Unit            string
	WeightGrams     int
	CategoryID      string
	ManufacturerID  string
	BrandID         string
	CollectionIDs   []string
	Series          string
	Tags            []string
	Badges          []string
	Ingredients     string
	Nutrition       string
	ShelfLifeDays   int
	Stock           int
	MinimumQuantity int
	QuantityStep    int
	Featured        bool
}

// NewProduct is a validated ProductInput with its new ID.
type NewProduct struct {
	ID    string
	Input ProductInput
}

type Reference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AdminProduct is the staff view of a product, archived or not. Image and
// gallery are read-only until media management exists.
type AdminProduct struct {
	ID              string      `json:"id"`
	Revision        int64       `json:"revision"`
	Archived        bool        `json:"archived"`
	ArchivedAt      *time.Time  `json:"archivedAt,omitempty"`
	Slug            string      `json:"slug"`
	SKU             string      `json:"sku"`
	Name            string      `json:"name"`
	ShortName       string      `json:"shortName"`
	Description     string      `json:"description"`
	PriceMinor      int64       `json:"priceMinor"`
	OldPriceMinor   int64       `json:"oldPriceMinor"`
	Currency        string      `json:"currency"`
	Unit            string      `json:"unit"`
	WeightGrams     int         `json:"weightGrams"`
	Image           string      `json:"image"`
	Gallery         []string    `json:"gallery"`
	Category        Reference   `json:"category"`
	Manufacturer    Reference   `json:"manufacturer"`
	Brand           Reference   `json:"brand"`
	Collections     []Reference `json:"collections"`
	Series          string      `json:"series"`
	Tags            []string    `json:"tags"`
	Badges          []string    `json:"badges"`
	Ingredients     string      `json:"ingredients"`
	Nutrition       string      `json:"nutrition"`
	ShelfLifeDays   int         `json:"shelfLifeDays"`
	Rating          float64     `json:"rating"`
	ReviewsCount    int         `json:"reviewsCount"`
	Stock           int         `json:"stock"`
	MinimumQuantity int         `json:"minimumQuantity"`
	QuantityStep    int         `json:"quantityStep"`
	Featured        bool        `json:"featured"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
}

// CatalogOptions are the dictionaries a staff product form chooses from: every
// category, manufacturer, brand (with its manufacturer) and collection,
// whether or not products use them yet. Read-only; B1 rechecks every
// reference on write.
type CatalogOptions struct {
	Categories    []CategoryOption `json:"categories"`
	Manufacturers []Reference      `json:"manufacturers"`
	Brands        []BrandOption    `json:"brands"`
	Collections   []Reference      `json:"collections"`
}

type CategoryOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
}

type BrandOption struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	ManufacturerID string `json:"manufacturerId"`
}

// AdminProductStatus filters the staff product list.
type AdminProductStatus string

const (
	AdminProductsActive   AdminProductStatus = "active"
	AdminProductsArchived AdminProductStatus = "archived"
	AdminProductsAll      AdminProductStatus = "all"
)

type AdminProductFilter struct {
	Status AdminProductStatus
	// Query matches the ID or SKU exactly, or a substring of the name, slug
	// or SKU, case-insensitively.
	Query string
	Page  int
	Limit int
}

type AdminProductPage struct {
	Items      []AdminProduct `json:"items"`
	Total      int            `json:"total"`
	Page       int            `json:"page"`
	Limit      int            `json:"limit"`
	TotalPages int            `json:"totalPages"`
}

// QuantityAllowed is the cart and checkout quantity rule: at least the
// minimum, in whole steps above it.
func QuantityAllowed(quantity, minimum, step int) bool {
	minimum, step = max(1, minimum), max(1, step)
	return quantity >= minimum && (quantity-minimum)%step == 0
}

// CartLineIssue explains why a cart line cannot be checked out with the
// product's current state, or returns "". Checkout enforces the same rules
// on the locked rows.
func CartLineIssue(product Product, quantity int) string {
	switch {
	case product.Archived:
		return "Товар больше не продаётся"
	case product.Stock == 0:
		return "Нет в наличии"
	case quantity > product.Stock:
		return fmt.Sprintf("Доступно только %d", product.Stock)
	case !QuantityAllowed(quantity, product.MinimumQuantity, product.QuantityStep):
		return fmt.Sprintf("Минимум %d, шаг %d", product.MinimumQuantity, product.QuantityStep)
	}
	return ""
}
