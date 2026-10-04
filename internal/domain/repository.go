package domain

import (
	"context"
	"time"
)

type Repository interface {
	FindUserByEmail(context.Context, string) (UserCredential, error)
	// CreateSession inserts the session, purges the user's expired sessions and
	// removes NewSession.ReplacesTokenHash (cookie transport only) atomically.
	// A duplicate token hash is an error and changes nothing.
	CreateSession(context.Context, NewSession, time.Time) error
	// SessionByTokenHash returns ErrUnauthorized unless an unexpired session with
	// the hash exists for the given transport.
	SessionByTokenHash(context.Context, string, SessionTransport, time.Time) (SessionRecord, error)
	// DeleteSession is idempotent.
	DeleteSession(context.Context, string, SessionTransport) error

	ListCategories(context.Context) ([]Category, error)
	ListManufacturers(context.Context) ([]Manufacturer, error)
	ListBrands(context.Context) ([]Brand, error)
	ListCollections(context.Context) ([]Collection, error)
	ListProducts(context.Context, ProductFilter) (ProductPage, error)
	CatalogFacets(context.Context, ProductFilter) (CatalogFacets, error)
	Product(context.Context, string) (Product, error)
	Products(context.Context, []string) (map[string]Product, error)
	Suggestions(context.Context, string, int) ([]SearchSuggestion, error)

	Cart(context.Context, string) (Cart, error)
	SetCartItem(context.Context, string, string, int, bool) (Cart, error)
	RemoveCartItem(context.Context, string, string) (Cart, error)
	ClearCart(context.Context, string, bool) (Cart, error)
	SetCartSelection(context.Context, string, []string, bool) (Cart, error)
	AddCartItems(context.Context, string, []CartMutation) (Cart, error)

	Favorites(context.Context, string) (Favorites, error)
	SetFavorite(context.Context, string, string, bool) (Favorites, error)

	ListOrders(context.Context, string) ([]Order, error)
	Order(context.Context, string, string) (Order, error)
	// CreateOrder consumes the selected cart atomically. With an idempotency key
	// it first returns the committed order for the same key and digest
	// (replayed=true), or ErrIdempotencyKeyReused for a different digest, and
	// stores the key with the new order in the same transaction.
	CreateOrder(context.Context, string, CheckoutInput) (Order, bool, error)
	// CommitRepeatV2 checks the receipt before review state, then atomically
	// validates, adds exact quantities, and records the receipt.
	CommitRepeatV2(context.Context, string, string, RepeatV2Input) (RepeatV2Result, error)
}
