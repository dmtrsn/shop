package domain

import "time"

const (
	FreeDeliveryThresholdMinor int64 = 350000
	StandardDeliveryMinor      int64 = 29000
)

func DeliveryMinor(subtotalMinor int64) int64 {
	if subtotalMinor > 0 && subtotalMinor < FreeDeliveryThresholdMinor {
		return StandardDeliveryMinor
	}
	return 0
}

func ListPriceMinor(product Product) int64 {
	if product.OldPriceMinor > product.PriceMinor {
		return product.OldPriceMinor
	}
	return product.PriceMinor
}

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type UserCredential struct {
	User         User
	PasswordHash string
}

type Category struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	ParentID     string `json:"parentId,omitempty"`
	Icon         string `json:"icon,omitempty"`
	ProductCount int    `json:"productCount"`
}

type Manufacturer struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Name         string `json:"name"`
	Country      string `json:"country"`
	Description  string `json:"description,omitempty"`
	Logo         string `json:"logo,omitempty"`
	ProductCount int    `json:"productCount"`
}

type Brand struct {
	ID             string `json:"id"`
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	ManufacturerID string `json:"manufacturerId"`
	Manufacturer   string `json:"manufacturer"`
	Description    string `json:"description,omitempty"`
	Logo           string `json:"logo,omitempty"`
	ProductCount   int    `json:"productCount"`
}

type Product struct {
	ID              string       `json:"id"`
	Slug            string       `json:"slug"`
	SKU             string       `json:"sku"`
	Name            string       `json:"name"`
	ShortName       string       `json:"shortName,omitempty"`
	Description     string       `json:"description"`
	PriceMinor      int64        `json:"priceMinor"`
	OldPriceMinor   int64        `json:"oldPriceMinor,omitempty"`
	Currency        string       `json:"currency"`
	Unit            string       `json:"unit"`
	WeightGrams     int          `json:"weightGrams,omitempty"`
	Image           string       `json:"image"`
	Gallery         []string     `json:"gallery,omitempty"`
	Category        Category     `json:"category"`
	Manufacturer    Manufacturer `json:"manufacturer"`
	Brand           Brand        `json:"brand"`
	Series          string       `json:"series,omitempty"`
	Tags            []string     `json:"tags,omitempty"`
	Badges          []string     `json:"badges,omitempty"`
	Ingredients     string       `json:"ingredients,omitempty"`
	Nutrition       string       `json:"nutrition,omitempty"`
	ShelfLifeDays   int          `json:"shelfLifeDays,omitempty"`
	Rating          float64      `json:"rating"`
	ReviewsCount    int          `json:"reviewsCount"`
	Stock           int          `json:"stock"`
	MinimumQuantity int          `json:"minimumQuantity"`
	QuantityStep    int          `json:"quantityStep"`
	Featured        bool         `json:"featured,omitempty"`
	CreatedAt       time.Time    `json:"createdAt"`
}

type Collection struct {
	ID           string    `json:"id"`
	Slug         string    `json:"slug"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Accent       string    `json:"accent"`
	Image        string    `json:"image,omitempty"`
	ProductIDs   []string  `json:"productIds"`
	ProductCount int       `json:"productCount"`
	StartsAt     time.Time `json:"startsAt,omitempty"`
	EndsAt       time.Time `json:"endsAt,omitempty"`
}

// CollectionVisibleAt mirrors the existing PostgreSQL collection schedule:
// open boundaries are visible, and configured start/end instants are inclusive.
func CollectionVisibleAt(collection Collection, now time.Time) bool {
	return (collection.StartsAt.IsZero() || !collection.StartsAt.After(now)) &&
		(collection.EndsAt.IsZero() || !collection.EndsAt.Before(now))
}

type ProductFilter struct {
	Query         string
	Category      string
	Manufacturer  string
	Brand         string
	Collection    string
	Series        string
	Tags          []string
	MinPriceMinor int64
	MaxPriceMinor int64
	InStock       *bool
	OnSale        bool
	NewOnly       bool
	Sort          string
	Page          int
	Limit         int
}

type ProductPage struct {
	Items      []Product `json:"items"`
	Total      int       `json:"total"`
	Page       int       `json:"page"`
	Limit      int       `json:"limit"`
	TotalPages int       `json:"totalPages"`
}

type FacetValue struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type CatalogFacets struct {
	Categories    []FacetValue `json:"categories"`
	Manufacturers []FacetValue `json:"manufacturers"`
	Brands        []FacetValue `json:"brands"`
	Tags          []FacetValue `json:"tags"`
	Series        []FacetValue `json:"series"`
	MinPriceMinor int64        `json:"minPriceMinor"`
	MaxPriceMinor int64        `json:"maxPriceMinor"`
}

type SearchSuggestion struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Image    string `json:"image,omitempty"`
}

type CartItem struct {
	Product        Product `json:"product"`
	Quantity       int     `json:"quantity"`
	Selected       bool    `json:"selected"`
	Available      int     `json:"available"`
	LineTotalMinor int64   `json:"lineTotalMinor"`
	Issue          string  `json:"issue,omitempty"`
}

type CartGroup struct {
	Manufacturer  Manufacturer `json:"manufacturer"`
	Items         []CartItem   `json:"items"`
	SelectedCount int          `json:"selectedCount"`
	SubtotalMinor int64        `json:"subtotalMinor"`
}

type CartSummary struct {
	ItemCount        int   `json:"itemCount"`
	PositionCount    int   `json:"positionCount"`
	SelectedCount    int   `json:"selectedCount"`
	SubtotalMinor    int64 `json:"subtotalMinor"`
	DiscountMinor    int64 `json:"discountMinor"`
	DeliveryMinor    int64 `json:"deliveryMinor"`
	TotalMinor       int64 `json:"totalMinor"`
	UnavailableCount int   `json:"unavailableCount"`
}

type Cart struct {
	Items     []CartItem  `json:"items"`
	Groups    []CartGroup `json:"groups"`
	Summary   CartSummary `json:"summary"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

type FavoriteGroup struct {
	Manufacturer Manufacturer `json:"manufacturer"`
	Products     []Product    `json:"products"`
}

type Favorites struct {
	Items  []Product       `json:"items"`
	Groups []FavoriteGroup `json:"groups"`
	Total  int             `json:"total"`
}

type OrderItem struct {
	ProductID            string `json:"productId"`
	SKU                  string `json:"sku"`
	Name                 string `json:"name"`
	Image                string `json:"image"`
	ManufacturerID       string `json:"manufacturerId"`
	Manufacturer         string `json:"manufacturer"`
	Quantity             int    `json:"quantity"`
	UnitPriceMinor       int64  `json:"unitPriceMinor"`
	LineTotalMinor       int64  `json:"lineTotalMinor"`
	ReplacementProductID string `json:"-"`
}

type Order struct {
	ID              string      `json:"id"`
	Number          string      `json:"number"`
	UserID          string      `json:"-"`
	Status          string      `json:"status"`
	StatusLabel     string      `json:"statusLabel"`
	Items           []OrderItem `json:"items"`
	ItemCount       int         `json:"itemCount"`
	TotalMinor      int64       `json:"totalMinor"`
	DeliveryAddress string      `json:"deliveryAddress"`
	Comment         string      `json:"comment,omitempty"`
	CreatedAt       time.Time   `json:"createdAt"`
	DeliveredAt     time.Time   `json:"deliveredAt,omitempty"`
}

type RepeatItem struct {
	Product                Product  `json:"product"`
	RequestedQuantity      int      `json:"requestedQuantity"`
	AvailableQuantity      int      `json:"availableQuantity"`
	AddableQuantity        int      `json:"addableQuantity"`
	SuggestedQuantity      int      `json:"suggestedQuantity"`
	ProposedQuantity       int      `json:"proposedQuantity"`
	AlreadyInCartQuantity  int      `json:"alreadyInCartQuantity"`
	OriginalUnitPriceMinor int64    `json:"originalUnitPriceMinor"`
	CurrentUnitPriceMinor  int64    `json:"currentUnitPriceMinor"`
	PriceDeltaMinor        int64    `json:"priceDeltaMinor"`
	PriceChanged           bool     `json:"priceChanged"`
	Selected               bool     `json:"selected"`
	Status                 string   `json:"status"`
	Message                string   `json:"message,omitempty"`
	Replacement            *Product `json:"replacement,omitempty"`
}

type RepeatPreview struct {
	OrderID             string       `json:"orderId"`
	OrderNumber         string       `json:"orderNumber"`
	Items               []RepeatItem `json:"items"`
	RequestedCount      int          `json:"requestedCount"`
	AvailableCount      int          `json:"availableCount"`
	LimitedCount        int          `json:"limitedCount"`
	UnavailableCount    int          `json:"unavailableCount"`
	EstimatedTotalMinor int64        `json:"estimatedTotalMinor"`
}

type RepeatSelection struct {
	ProductID string `json:"productId"`
	Quantity  int    `json:"quantity"`
	Selected  bool   `json:"selected"`
}

type CartMutation struct {
	ProductID string `json:"productId"`
	Quantity  int    `json:"quantity"`
	Selected  bool   `json:"selected"`
}

type RepeatCommitResult struct {
	Cart          Cart `json:"cart"`
	AddedCount    int  `json:"addedCount"`
	AddedQuantity int  `json:"addedQuantity"`
	AdjustedCount int  `json:"adjustedCount"`
	SkippedCount  int  `json:"skippedCount"`
}

type CheckoutInput struct {
	DeliveryAddress string `json:"deliveryAddress"`
	Comment         string `json:"comment"`
}
