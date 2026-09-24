package domain

import (
	"context"
	"time"
)

type Repository interface {
	FindUserByEmail(context.Context, string) (UserCredential, error)
	CreateSession(context.Context, string, string, time.Time) error
	UserBySession(context.Context, string, time.Time) (User, error)

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
	CreateOrder(context.Context, string, CheckoutInput) (Order, error)
}
