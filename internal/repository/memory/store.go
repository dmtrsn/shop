package memory

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"sweetshop/internal/domain"
)

const demoPasswordHash = "$2y$10$QTEU/WwQc6q9ngaHUfFn7uon8VZnVmK5Q6osIfgo.qFoxqLjyWTAm"

type session struct {
	userID    string
	expiresAt time.Time
}

type cartRow struct {
	quantity int
	selected bool
	updated  time.Time
}

type Store struct {
	mu sync.RWMutex

	usersByEmail  map[string]domain.UserCredential
	usersByID     map[string]domain.User
	sessions      map[string]session
	categories    []domain.Category
	manufacturers []domain.Manufacturer
	brands        []domain.Brand
	collections   []domain.Collection
	products      map[string]domain.Product
	productOrder  []string
	carts         map[string]map[string]cartRow
	favorites     map[string]map[string]bool
	orders        map[string][]domain.Order
}

func NewDemo() *Store {
	s := &Store{
		usersByEmail: make(map[string]domain.UserCredential),
		usersByID:    make(map[string]domain.User),
		sessions:     make(map[string]session),
		products:     make(map[string]domain.Product),
		carts:        make(map[string]map[string]cartRow),
		favorites:    make(map[string]map[string]bool),
		orders:       make(map[string][]domain.Order),
	}
	s.seed()
	return s
}

func (s *Store) seed() {
	created := time.Date(2025, 11, 4, 10, 0, 0, 0, time.UTC)
	user := domain.User{ID: "usr-demo", Name: "Алексей Морозов", Email: "demo@sugar.ru", Phone: "+7 999 123-45-67", CreatedAt: created}
	s.usersByEmail[user.Email] = domain.UserCredential{User: user, PasswordHash: demoPasswordHash}
	s.usersByID[user.ID] = user

	s.categories = []domain.Category{
		{ID: "cat-chocolate", Slug: "chocolate", Name: "Шоколад", Description: "Плитки, драже и шоколадные наборы", Icon: "chocolate"},
		{ID: "cat-bars", Slug: "chocolate-bars", Name: "Плиточный шоколад", ParentID: "cat-chocolate"},
		{ID: "cat-dragee", Slug: "dragee", Name: "Драже в шоколаде", ParentID: "cat-chocolate"},
		{ID: "cat-candy", Slug: "candy", Name: "Конфеты", Description: "Пралине, трюфели, карамель", Icon: "candy"},
		{ID: "cat-praline", Slug: "praline", Name: "Пралине", ParentID: "cat-candy"},
		{ID: "cat-truffles", Slug: "truffles", Name: "Трюфели", ParentID: "cat-candy"},
		{ID: "cat-cookies", Slug: "cookies-wafers", Name: "Печенье и вафли", Icon: "cookie"},
		{ID: "cat-marshmallow", Slug: "marshmallow-pastila", Name: "Зефир и пастила", Icon: "cloud"},
		{ID: "cat-marmalade", Slug: "marmalade", Name: "Мармелад", Icon: "gem"},
		{ID: "cat-gifts", Slug: "gift-boxes", Name: "Подарочные наборы", Icon: "gift"},
	}
	s.manufacturers = []domain.Manufacturer{
		{ID: "m-choco", Slug: "chocoatelier", Name: "Choco Atelier", Country: "Россия", Description: "Bean-to-bar шоколад из Петербурга"},
		{ID: "m-belyov", Slug: "belevskie-sladosti", Name: "Белёвские сладости", Country: "Россия", Description: "Пастила по традиционным рецептам"},
		{ID: "m-caffarel", Slug: "caffarel", Name: "Caffarel", Country: "Италия", Description: "Пьемонтский шоколад и джандуйя"},
		{ID: "m-leonidas", Slug: "leonidas", Name: "Leonidas", Country: "Бельгия", Description: "Бельгийские пралине с 1913 года"},
		{ID: "m-sweetzerland", Slug: "sweetzerland", Name: "Sweetzerland", Country: "Швейцария", Description: "Современная швейцарская шоколадная школа"},
		{ID: "m-kolomna", Slug: "kolomna", Name: "Коломна", Country: "Россия", Description: "Фруктовая пастила и мармелад"},
		{ID: "m-michel", Slug: "michel-augustin", Name: "Michel et Augustin", Country: "Франция", Description: "Французское сливочное печенье"},
		{ID: "m-lakrids", Slug: "lakrids-by-bulow", Name: "Lakrids by Bülow", Country: "Дания", Description: "Лакричное драже ручной работы"},
	}
	s.brands = []domain.Brand{
		{ID: "b-atelier-noir", Slug: "atelier-noir", Name: "Atelier Noir", ManufacturerID: "m-choco", Manufacturer: "Choco Atelier", Description: "Тёмный шоколад и драже"},
		{ID: "b-atelier-maison", Slug: "atelier-maison", Name: "Atelier Maison", ManufacturerID: "m-choco", Manufacturer: "Choco Atelier", Description: "Конфеты и подарочные коллекции"},
		{ID: "b-caffarel", Slug: "caffarel", Name: "Caffarel", ManufacturerID: "m-caffarel", Manufacturer: "Caffarel"},
		{ID: "b-piemonte", Slug: "piemonte-1826", Name: "Piemonte 1826", ManufacturerID: "m-caffarel", Manufacturer: "Caffarel"},
		{ID: "b-leonidas", Slug: "leonidas", Name: "Leonidas", ManufacturerID: "m-leonidas", Manufacturer: "Leonidas"},
		{ID: "b-manon", Slug: "manon", Name: "Manon", ManufacturerID: "m-leonidas", Manufacturer: "Leonidas"},
		{ID: "b-sweetzerland", Slug: "sweetzerland", Name: "Sweetzerland", ManufacturerID: "m-sweetzerland", Manufacturer: "Sweetzerland"},
		{ID: "b-belev", Slug: "belev", Name: "Белёвская классика", ManufacturerID: "m-belyov", Manufacturer: "Белёвские сладости"},
		{ID: "b-kolomna", Slug: "kolomna-posad", Name: "Коломенский посад", ManufacturerID: "m-kolomna", Manufacturer: "Коломна"},
		{ID: "b-michel", Slug: "michel-augustin", Name: "Michel et Augustin", ManufacturerID: "m-michel", Manufacturer: "Michel et Augustin"},
		{ID: "b-lakrids", Slug: "lakrids", Name: "Lakrids", ManufacturerID: "m-lakrids", Manufacturer: "Lakrids by Bülow"},
	}

	type seedProduct struct {
		id, slug, sku, name, short, category, maker, unit, image string
		price, old                                               int64
		weight, stock                                            int
		rating                                                   float64
		tags, badges                                             []string
		featured                                                 bool
	}
	items := []seedProduct{
		{"p01", "dark-chocolate-sea-salt", "CHA-701", "Тёмный шоколад 70% с морской солью", "Тёмный 70% · соль", "cat-bars", "m-choco", "плитка 90 г", "", 48900, 54900, 90, 24, 4.9, []string{"без сахара", "веган"}, []string{"Хит"}, true},
		{"p02", "milk-chocolate-hazelnut", "CAF-122", "Молочный шоколад с цельным фундуком", "Молочный · фундук", "cat-bars", "m-caffarel", "плитка 100 г", "", 62900, 0, 100, 18, 4.8, []string{"орехи"}, []string{"Новинка"}, true},
		{"p03", "raspberry-ruby-chocolate", "SWE-204", "Розовый шоколад с малиной", "Ruby · малина", "cat-bars", "m-sweetzerland", "плитка 85 г", "", 71900, 0, 85, 9, 4.7, []string{"ягоды"}, []string{"Новинка"}, true},
		{"p04", "gianduja-praline-box", "CAF-310", "Джандуйя Classico", "Джандуйя", "cat-praline", "m-caffarel", "коробка 165 г", "", 159000, 179000, 165, 15, 4.9, []string{"орехи", "подарок"}, []string{"−11%"}, true},
		{"p05", "leonidas-assorted-pralines", "LEO-505", "Ассорти бельгийских пралине", "Пралине ассорти", "cat-praline", "m-leonidas", "коробка 250 г", "", 239000, 0, 250, 7, 5.0, []string{"подарок", "орехи"}, []string{"Выбор покупателей"}, true},
		{"p06", "salted-caramel-truffles", "CHA-422", "Трюфели с солёной карамелью", "Трюфели · карамель", "cat-truffles", "m-choco", "коробка 180 г", "", 129000, 0, 180, 13, 4.8, []string{"карамель", "подарок"}, nil, true},
		{"p07", "espresso-truffles", "SWE-418", "Трюфели Espresso", "Трюфели · кофе", "cat-truffles", "m-sweetzerland", "коробка 12 шт.", "", 139000, 0, 160, 4, 4.6, []string{"кофе", "подарок"}, []string{"Осталось мало"}, false},
		{"p08", "cocoa-almond-dragee", "CHA-615", "Миндаль в какао", "Миндаль · какао", "cat-dragee", "m-choco", "банка 140 г", "", 84900, 0, 140, 31, 4.7, []string{"орехи", "без глютена"}, nil, false},
		{"p09", "lakrids-gold", "LAK-011", "Лакричное драже Gold", "Lakrids Gold", "cat-dragee", "m-lakrids", "банка 295 г", "", 249000, 279000, 295, 6, 4.5, []string{"лакрица", "подарок"}, []string{"Лимитированная серия"}, false},
		{"p10", "apple-pastila-classic", "BEL-101", "Белёвская пастила яблочная", "Пастила · яблоко", "cat-marshmallow", "m-belyov", "коробка 200 г", "", 39900, 0, 200, 42, 4.9, []string{"без сахара", "постное"}, []string{"Хит"}, true},
		{"p11", "pear-pastila", "BEL-118", "Пастила с грушей", "Пастила · груша", "cat-marshmallow", "m-belyov", "коробка 180 г", "", 42900, 0, 180, 28, 4.7, []string{"без сахара", "постное"}, nil, false},
		{"p12", "vanilla-marshmallow", "BEL-205", "Зефир ванильный", "Зефир · ваниль", "cat-marshmallow", "m-belyov", "коробка 250 г", "", 37900, 41900, 250, 34, 4.6, []string{"ваниль"}, []string{"−10%"}, false},
		{"p13", "blackcurrant-marshmallow", "KOL-214", "Зефир из чёрной смородины", "Зефир · смородина", "cat-marshmallow", "m-kolomna", "коробка 220 г", "", 46900, 0, 220, 17, 4.8, []string{"ягоды"}, []string{"Новинка"}, false},
		{"p14", "pear-marmalade", "KOL-330", "Мармелад из груши с ванилью", "Мармелад · груша", "cat-marmalade", "m-kolomna", "коробка 160 г", "", 51900, 0, 160, 21, 4.7, []string{"без глютена", "фрукты"}, nil, true},
		{"p15", "berry-marmalade", "KOL-355", "Ягодный мармелад ассорти", "Мармелад · ягоды", "cat-marmalade", "m-kolomna", "коробка 200 г", "", 57900, 0, 200, 19, 4.6, []string{"ягоды", "без глютена"}, nil, false},
		{"p16", "butter-cookies", "MIC-701", "Сливочное печенье Sablés", "Sablés · сливочное", "cat-cookies", "m-michel", "коробка 150 г", "", 68900, 74900, 150, 26, 4.8, []string{"сливочное"}, []string{"−8%"}, true},
		{"p17", "chocolate-cookies", "MIC-730", "Печенье с шоколадной крошкой", "Печенье · шоколад", "cat-cookies", "m-michel", "коробка 180 г", "", 72900, 0, 180, 11, 4.9, []string{"шоколад"}, []string{"Хит"}, true},
		{"p18", "wafer-hazelnut", "CAF-811", "Вафли с кремом из фундука", "Вафли · фундук", "cat-cookies", "m-caffarel", "упаковка 125 г", "", 59900, 0, 125, 0, 4.5, []string{"орехи"}, []string{"Нет в наличии"}, false},
		{"p19", "dark-orange-gift", "LEO-905", "Набор тёмного шоколада с апельсином", "Шоколад · апельсин", "cat-gifts", "m-leonidas", "набор 320 г", "", 289000, 319000, 320, 8, 4.9, []string{"цитрус", "подарок"}, []string{"−9%"}, true},
		{"p20", "atelier-signature-box", "CHA-999", "Signature Box: 24 конфеты", "Signature Box", "cat-gifts", "m-choco", "набор 24 шт.", "", 329000, 0, 420, 5, 5.0, []string{"подарок"}, []string{"Бестселлер"}, true},
		{"p21", "pastila-tea-box", "BEL-920", "Чайная коллекция пастилы", "Пастила · ассорти", "cat-gifts", "m-belyov", "набор 480 г", "", 149000, 0, 480, 12, 4.8, []string{"без сахара", "подарок"}, nil, false},
		{"p22", "white-matcha-chocolate", "SWE-260", "Белый шоколад с матчей", "Белый · матча", "cat-bars", "m-sweetzerland", "плитка 90 г", "", 74900, 0, 90, 16, 4.4, []string{"матча"}, []string{"Новинка"}, false},
		{"p23", "caramel-praline", "LEO-522", "Пралине с хрустящей карамелью", "Пралине · карамель", "cat-praline", "m-leonidas", "коробка 190 г", "", 179000, 0, 190, 14, 4.8, []string{"карамель", "подарок"}, nil, false},
		{"p24", "lemon-sable", "MIC-745", "Лимонное печенье Sablés", "Sablés · лимон", "cat-cookies", "m-michel", "коробка 150 г", "", 69900, 0, 150, 23, 4.6, []string{"цитрус"}, nil, false},
	}
	categoryByID := make(map[string]domain.Category, len(s.categories))
	for _, c := range s.categories {
		categoryByID[c.ID] = c
	}
	makerByID := make(map[string]domain.Manufacturer, len(s.manufacturers))
	for _, m := range s.manufacturers {
		makerByID[m.ID] = m
	}
	brandByID := make(map[string]domain.Brand, len(s.brands))
	for _, b := range s.brands {
		brandByID[b.ID] = b
	}
	brandByProduct := map[string]string{
		"p01": "b-atelier-noir", "p08": "b-atelier-noir", "p06": "b-atelier-maison", "p20": "b-atelier-maison",
		"p02": "b-caffarel", "p04": "b-piemonte", "p18": "b-piemonte", "p05": "b-leonidas", "p19": "b-leonidas", "p23": "b-manon",
		"p03": "b-sweetzerland", "p07": "b-sweetzerland", "p22": "b-sweetzerland", "p09": "b-lakrids",
		"p10": "b-belev", "p11": "b-belev", "p12": "b-belev", "p21": "b-belev", "p13": "b-kolomna", "p14": "b-kolomna", "p15": "b-kolomna",
		"p16": "b-michel", "p17": "b-michel", "p24": "b-michel",
	}
	seriesByProduct := map[string]string{
		"p01": "Atelier Origin", "p08": "Atelier Origin", "p06": "Atelier Ganache", "p20": "Atelier Ganache",
		"p02": "Piemonte Classico", "p04": "Piemonte Classico", "p18": "Piemonte Classico",
		"p05": "Belgian Selection", "p19": "Belgian Selection", "p23": "Belgian Selection",
		"p10": "Белёвская классика", "p11": "Белёвская классика", "p12": "Белёвская классика", "p21": "Белёвская классика",
		"p16": "Les Sablés", "p17": "Les Sablés", "p24": "Les Sablés",
	}
	for i, item := range items {
		image := "/assets/product-chocolate.webp"
		switch item.category {
		case "cat-marshmallow", "cat-marmalade":
			image = "/assets/product-marshmallow.webp"
		case "cat-cookies", "cat-gifts":
			image = "/assets/product-giftbox.webp"
		}
		p := domain.Product{
			ID: item.id, Slug: item.slug, SKU: item.sku, Name: item.name, ShortName: item.short,
			Description: "Выразительный вкус и аккуратно подобранные ингредиенты. Подходит для подарка и домашней коллекции.",
			PriceMinor:  item.price, OldPriceMinor: item.old, Currency: "RUB", Unit: item.unit, WeightGrams: item.weight,
			Image: image, Gallery: []string{image}, Category: categoryByID[item.category], Manufacturer: makerByID[item.maker], Brand: brandByID[brandByProduct[item.id]], Series: seriesByProduct[item.id],
			Tags: item.tags, Badges: item.badges, Ingredients: "Натуральные ингредиенты; подробный состав указан на упаковке.",
			Nutrition: "Пищевая ценность зависит от вида продукта", ShelfLifeDays: 180, Rating: item.rating,
			ReviewsCount: 18 + i*7, Stock: item.stock, MinimumQuantity: 1, QuantityStep: 1, Featured: item.featured,
			CreatedAt: created.Add(time.Duration(i) * 24 * time.Hour),
		}
		s.products[p.ID] = p
		s.productOrder = append(s.productOrder, p.ID)
	}
	s.refreshCounts()
	s.collections = []domain.Collection{
		{ID: "col-new", Slug: "new", Name: "Новинки недели", Description: "Свежие вкусы от любимых мануфактур", Accent: "lime", ProductIDs: []string{"p02", "p03", "p13", "p22"}, ProductCount: 4},
		{ID: "col-bestsellers", Slug: "bestsellers", Name: "Берут чаще всего", Description: "Проверенные фавориты покупателей", Accent: "cocoa", ProductIDs: []string{"p01", "p05", "p10", "p17", "p20"}, ProductCount: 5},
		{ID: "col-gifts", Slug: "gifts-under-2000", Name: "Подарки до 2 000 ₽", Description: "Готовые наборы без долгого выбора", Accent: "coral", ProductIDs: []string{"p04", "p06", "p07", "p21", "p23"}, ProductCount: 5},
		{ID: "col-no-sugar", Slug: "no-added-sugar", Name: "Без добавленного сахара", Description: "Чистый вкус фруктов и какао", Accent: "sky", ProductIDs: []string{"p01", "p10", "p11", "p21"}, ProductCount: 4},
	}
	s.carts[user.ID] = map[string]cartRow{
		"p01": {quantity: 2, selected: true, updated: created},
		"p10": {quantity: 3, selected: true, updated: created},
		"p16": {quantity: 1, selected: false, updated: created},
		"p18": {quantity: 2, selected: false, updated: created},
	}
	s.favorites[user.ID] = map[string]bool{"p02": true, "p05": true, "p09": true, "p14": true, "p20": true}
	oldItems := []domain.OrderItem{
		{ProductID: "p01", SKU: s.products["p01"].SKU, Name: s.products["p01"].Name, Image: s.products["p01"].Image, ManufacturerID: s.products["p01"].Manufacturer.ID, Manufacturer: s.products["p01"].Manufacturer.Name, Quantity: 2, UnitPriceMinor: 45900, LineTotalMinor: 91800},
		orderItem(s.products["p07"], 12),
		orderItem(s.products["p18"], 3),
		{ProductID: "legacy-01", SKU: "OLD-101", Name: "Карамель Coffee Archive", Manufacturer: "Choco Atelier", Quantity: 2, UnitPriceMinor: 39000, LineTotalMinor: 78000, ReplacementProductID: "p06"},
	}
	s.orders[user.ID] = []domain.Order{
		{ID: "ord-2026-002", Number: "#10482", UserID: user.ID, Status: "delivered", StatusLabel: "Доставлен", Items: oldItems, ItemCount: itemCount(oldItems), TotalMinor: orderTotal(oldItems), DeliveryAddress: "Москва, ул. Большая Дмитровка, 12", CreatedAt: time.Date(2026, 7, 18, 12, 30, 0, 0, time.UTC), DeliveredAt: time.Date(2026, 7, 20, 16, 15, 0, 0, time.UTC)},
		{ID: "ord-2026-001", Number: "#10317", UserID: user.ID, Status: "delivered", StatusLabel: "Доставлен", Items: []domain.OrderItem{orderItem(s.products["p05"], 1), orderItem(s.products["p10"], 4), orderItem(s.products["p16"], 2)}, ItemCount: 7, TotalMinor: 528400, DeliveryAddress: "Москва, ул. Большая Дмитровка, 12", CreatedAt: time.Date(2026, 5, 3, 9, 45, 0, 0, time.UTC), DeliveredAt: time.Date(2026, 5, 5, 14, 0, 0, 0, time.UTC)},
	}
}

func (s *Store) refreshCounts() {
	catCounts := map[string]int{}
	makerCounts := map[string]int{}
	brandCounts := map[string]int{}
	parents := map[string]string{}
	for _, c := range s.categories {
		parents[c.ID] = c.ParentID
	}
	for _, p := range s.products {
		catCounts[p.Category.ID]++
		if parent := parents[p.Category.ID]; parent != "" {
			catCounts[parent]++
		}
		makerCounts[p.Manufacturer.ID]++
		brandCounts[p.Brand.ID]++
	}
	for i := range s.categories {
		s.categories[i].ProductCount = catCounts[s.categories[i].ID]
	}
	for i := range s.manufacturers {
		s.manufacturers[i].ProductCount = makerCounts[s.manufacturers[i].ID]
	}
	for i := range s.brands {
		s.brands[i].ProductCount = brandCounts[s.brands[i].ID]
	}
}

func (s *Store) FindUserByEmail(_ context.Context, email string) (domain.UserCredential, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.usersByEmail[strings.ToLower(email)]
	if !ok {
		return domain.UserCredential{}, domain.ErrNotFound
	}
	return u, nil
}

func (s *Store) CreateSession(_ context.Context, userID, hash string, expires time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.usersByID[userID]; !ok {
		return domain.ErrNotFound
	}
	s.sessions[hash] = session{userID: userID, expiresAt: expires}
	return nil
}

func (s *Store) UserBySession(_ context.Context, hash string, now time.Time) (domain.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[hash]
	if !ok || !sess.expiresAt.After(now) {
		return domain.User{}, domain.ErrUnauthorized
	}
	u, ok := s.usersByID[sess.userID]
	if !ok {
		return domain.User{}, domain.ErrUnauthorized
	}
	return u, nil
}

func (s *Store) ListCategories(context.Context) ([]domain.Category, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domain.Category(nil), s.categories...), nil
}

func (s *Store) ListManufacturers(context.Context) ([]domain.Manufacturer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domain.Manufacturer(nil), s.manufacturers...), nil
}

func (s *Store) ListBrands(context.Context) ([]domain.Brand, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]domain.Brand(nil), s.brands...), nil
}

func (s *Store) ListCollections(context.Context) ([]domain.Collection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	visible := make([]domain.Collection, 0, len(s.collections))
	for _, collection := range s.collections {
		if domain.CollectionVisibleAt(collection, now) {
			visible = append(visible, collection)
		}
	}
	return cloneCollections(visible), nil
}

func (s *Store) ListProducts(_ context.Context, filter domain.ProductFilter) (domain.ProductPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := s.filteredProducts(filter)
	sortProducts(items, filter.Sort)
	page, limit := filter.Page, filter.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 24
	}
	total := len(items)
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	result := cloneProducts(items[start:end])
	return domain.ProductPage{Items: result, Total: total, Page: page, Limit: limit, TotalPages: int(math.Ceil(float64(total) / float64(limit)))}, nil
}

func (s *Store) CatalogFacets(_ context.Context, filter domain.ProductFilter) (domain.CatalogFacets, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := s.filteredProducts(filter)
	f := domain.CatalogFacets{Categories: []domain.FacetValue{}, Manufacturers: []domain.FacetValue{}, Brands: []domain.FacetValue{}, Tags: []domain.FacetValue{}, Series: []domain.FacetValue{}}
	cat, makers, brands, tags, series := map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	for _, p := range items {
		cat[p.Category.ID]++
		makers[p.Manufacturer.ID]++
		brands[p.Brand.ID]++
		for _, tag := range p.Tags {
			tags[tag]++
		}
		if p.Series != "" {
			series[p.Series]++
		}
		if f.MinPriceMinor == 0 || p.PriceMinor < f.MinPriceMinor {
			f.MinPriceMinor = p.PriceMinor
		}
		if p.PriceMinor > f.MaxPriceMinor {
			f.MaxPriceMinor = p.PriceMinor
		}
	}
	for _, c := range s.categories {
		if n := cat[c.ID]; n > 0 {
			f.Categories = append(f.Categories, domain.FacetValue{ID: c.ID, Slug: c.Slug, Name: c.Name, Count: n})
		}
	}
	for _, m := range s.manufacturers {
		if n := makers[m.ID]; n > 0 {
			f.Manufacturers = append(f.Manufacturers, domain.FacetValue{ID: m.ID, Slug: m.Slug, Name: m.Name, Count: n})
		}
	}
	for _, b := range s.brands {
		if n := brands[b.ID]; n > 0 {
			f.Brands = append(f.Brands, domain.FacetValue{ID: b.ID, Slug: b.Slug, Name: b.Name, Count: n})
		}
	}
	for name, n := range tags {
		f.Tags = append(f.Tags, domain.FacetValue{ID: name, Slug: name, Name: name, Count: n})
	}
	for name, n := range series {
		f.Series = append(f.Series, domain.FacetValue{ID: name, Slug: name, Name: name, Count: n})
	}
	sort.Slice(f.Tags, func(i, j int) bool { return f.Tags[i].Name < f.Tags[j].Name })
	sort.Slice(f.Series, func(i, j int) bool { return f.Series[i].Name < f.Series[j].Name })
	return f, nil
}

func (s *Store) Product(_ context.Context, idOrSlug string) (domain.Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.findProduct(idOrSlug)
	if !ok {
		return domain.Product{}, domain.ErrNotFound
	}
	return cloneProduct(p), nil
}

func (s *Store) Products(_ context.Context, ids []string) (map[string]domain.Product, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]domain.Product, len(ids))
	for _, id := range ids {
		if product, ok := s.products[id]; ok {
			result[id] = cloneProduct(product)
		}
	}
	return result, nil
}

func (s *Store) Suggestions(_ context.Context, query string, limit int) ([]domain.SearchSuggestion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	q := strings.ToLower(strings.TrimSpace(query))
	result := make([]domain.SearchSuggestion, 0, limit)
	for _, id := range s.productOrder {
		p := s.products[id]
		if containsAny(q, p.Name, p.ShortName, p.SKU, p.Manufacturer.Name, p.Brand.Name, p.Series, p.Category.Name) {
			result = append(result, domain.SearchSuggestion{Type: "product", ID: p.ID, Slug: p.Slug, Title: p.Name, Subtitle: p.Manufacturer.Name + " · " + p.Unit, Image: p.Image})
			if len(result) == limit {
				return result, nil
			}
		}
	}
	for _, m := range s.manufacturers {
		if strings.Contains(strings.ToLower(m.Name), q) {
			result = append(result, domain.SearchSuggestion{Type: "manufacturer", ID: m.ID, Slug: m.Slug, Title: m.Name, Subtitle: m.Country})
		}
		if len(result) == limit {
			return result, nil
		}
	}
	for _, b := range s.brands {
		if strings.Contains(strings.ToLower(b.Name), q) {
			result = append(result, domain.SearchSuggestion{Type: "brand", ID: b.ID, Slug: b.Slug, Title: b.Name, Subtitle: b.Manufacturer})
		}
		if len(result) == limit {
			return result, nil
		}
	}
	for _, c := range s.categories {
		if strings.Contains(strings.ToLower(c.Name), q) {
			result = append(result, domain.SearchSuggestion{Type: "category", ID: c.ID, Slug: c.Slug, Title: c.Name})
		}
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (s *Store) Cart(_ context.Context, userID string) (domain.Cart, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.buildCart(userID), nil
}

func (s *Store) SetCartItem(_ context.Context, userID, productID string, quantity int, selected bool) (domain.Cart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.findProduct(productID)
	if !ok {
		return domain.Cart{}, domain.ErrNotFound
	}
	if quantity > p.Stock {
		return domain.Cart{}, domain.ErrOutOfStock
	}
	if s.carts[userID] == nil {
		s.carts[userID] = map[string]cartRow{}
	}
	s.carts[userID][p.ID] = cartRow{quantity: quantity, selected: selected, updated: time.Now().UTC()}
	return s.buildCart(userID), nil
}

func (s *Store) RemoveCartItem(_ context.Context, userID, productID string) (domain.Cart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.findProduct(productID); ok {
		productID = p.ID
	}
	delete(s.carts[userID], productID)
	return s.buildCart(userID), nil
}

func (s *Store) ClearCart(_ context.Context, userID string, selectedOnly bool) (domain.Cart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !selectedOnly {
		s.carts[userID] = map[string]cartRow{}
	} else {
		for id, row := range s.carts[userID] {
			if row.selected {
				delete(s.carts[userID], id)
			}
		}
	}
	return s.buildCart(userID), nil
}

func (s *Store) SetCartSelection(_ context.Context, userID string, productIDs []string, selected bool) (domain.Cart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	selectAll := len(productIDs) == 0
	ids := make(map[string]bool, len(productIDs))
	for _, id := range productIDs {
		if p, ok := s.findProduct(id); ok {
			ids[p.ID] = true
		}
	}
	for id, row := range s.carts[userID] {
		if selectAll || ids[id] {
			row.selected = selected
			row.updated = time.Now().UTC()
			s.carts[userID][id] = row
		}
	}
	return s.buildCart(userID), nil
}

func (s *Store) AddCartItems(_ context.Context, userID string, mutations []domain.CartMutation) (domain.Cart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, mutation := range mutations {
		p, ok := s.findProduct(mutation.ProductID)
		if !ok {
			return domain.Cart{}, domain.ErrNotFound
		}
		existing := s.carts[userID][p.ID].quantity
		finalQuantity := existing + mutation.Quantity
		minimum := max(1, p.MinimumQuantity)
		step := max(1, p.QuantityStep)
		if mutation.Quantity < 1 || finalQuantity > p.Stock || finalQuantity < minimum || (finalQuantity-minimum)%step != 0 {
			return domain.Cart{}, domain.ErrOutOfStock
		}
	}
	if s.carts[userID] == nil {
		s.carts[userID] = map[string]cartRow{}
	}
	for _, mutation := range mutations {
		p, _ := s.findProduct(mutation.ProductID)
		existing := s.carts[userID][p.ID]
		s.carts[userID][p.ID] = cartRow{quantity: existing.quantity + mutation.Quantity, selected: mutation.Selected || existing.selected, updated: time.Now().UTC()}
	}
	return s.buildCart(userID), nil
}

func (s *Store) Favorites(_ context.Context, userID string) (domain.Favorites, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.buildFavorites(userID), nil
}

func (s *Store) SetFavorite(_ context.Context, userID, productID string, enabled bool) (domain.Favorites, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.findProduct(productID)
	if !ok {
		return domain.Favorites{}, domain.ErrNotFound
	}
	if s.favorites[userID] == nil {
		s.favorites[userID] = map[string]bool{}
	}
	if enabled {
		s.favorites[userID][p.ID] = true
	} else {
		delete(s.favorites[userID], p.ID)
	}
	return s.buildFavorites(userID), nil
}

func (s *Store) ListOrders(_ context.Context, userID string) ([]domain.Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	orders := s.orders[userID]
	result := make([]domain.Order, len(orders))
	for i, order := range orders {
		result[i] = cloneOrder(order)
	}
	return result, nil
}

func (s *Store) Order(_ context.Context, userID, orderID string) (domain.Order, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, order := range s.orders[userID] {
		if order.ID == orderID || order.Number == orderID {
			return cloneOrder(order), nil
		}
	}
	return domain.Order{}, domain.ErrNotFound
}

func (s *Store) CreateOrder(_ context.Context, userID string, input domain.CheckoutInput) (domain.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.carts[userID]
	items := make([]domain.OrderItem, 0, len(rows))
	for id, row := range rows {
		if !row.selected {
			continue
		}
		p, ok := s.products[id]
		if !ok || row.quantity > p.Stock {
			return domain.Order{}, domain.ErrOutOfStock
		}
		items = append(items, orderItem(p, row.quantity))
	}
	if len(items) == 0 {
		return domain.Order{}, domain.ErrConflict
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	now := time.Now().UTC()
	seq := len(s.orders[userID]) + 10483
	subtotal := orderTotal(items)
	order := domain.Order{ID: fmt.Sprintf("ord-%d-%03d", now.Year(), seq), Number: fmt.Sprintf("#%d", seq), UserID: userID, Status: "processing", StatusLabel: "Собираем заказ", Items: items, ItemCount: itemCount(items), TotalMinor: subtotal + domain.DeliveryMinor(subtotal), DeliveryAddress: input.DeliveryAddress, Comment: input.Comment, CreatedAt: now}
	for _, item := range items {
		p := s.products[item.ProductID]
		p.Stock -= item.Quantity
		s.products[p.ID] = p
		delete(rows, item.ProductID)
	}
	s.orders[userID] = append([]domain.Order{order}, s.orders[userID]...)
	return cloneOrder(order), nil
}

func (s *Store) filteredProducts(filter domain.ProductFilter) []domain.Product {
	q := strings.ToLower(filter.Query)
	collectionIDs := map[string]bool{}
	if filter.Collection != "" {
		now := time.Now()
		for _, c := range s.collections {
			if domain.CollectionVisibleAt(c, now) && (c.ID == filter.Collection || c.Slug == filter.Collection) {
				for _, id := range c.ProductIDs {
					collectionIDs[id] = true
				}
			}
		}
	}
	tagSet := map[string]bool{}
	for _, tag := range filter.Tags {
		tagSet[strings.ToLower(tag)] = true
	}
	allowedCategories := map[string]bool{}
	if filter.Category != "" {
		for _, c := range s.categories {
			if c.ID == filter.Category || c.Slug == filter.Category {
				allowedCategories[c.ID] = true
				for _, child := range s.categories {
					if child.ParentID == c.ID {
						allowedCategories[child.ID] = true
					}
				}
			}
		}
	}
	result := make([]domain.Product, 0, len(s.products))
	for _, id := range s.productOrder {
		p := s.products[id]
		if q != "" && !containsAny(q, p.Name, p.ShortName, p.Description, p.SKU, p.Manufacturer.Name, p.Brand.Name, p.Series, p.Category.Name, strings.Join(p.Tags, " ")) {
			continue
		}
		if filter.Category != "" && !allowedCategories[p.Category.ID] {
			continue
		}
		if filter.Manufacturer != "" && p.Manufacturer.ID != filter.Manufacturer && p.Manufacturer.Slug != filter.Manufacturer {
			continue
		}
		if filter.Brand != "" && p.Brand.ID != filter.Brand && p.Brand.Slug != filter.Brand {
			continue
		}
		if filter.Collection != "" && !collectionIDs[p.ID] {
			continue
		}
		if filter.Series != "" && !strings.EqualFold(filter.Series, p.Series) {
			continue
		}
		if filter.MinPriceMinor > 0 && p.PriceMinor < filter.MinPriceMinor {
			continue
		}
		if filter.MaxPriceMinor > 0 && p.PriceMinor > filter.MaxPriceMinor {
			continue
		}
		if filter.InStock != nil && (*filter.InStock != (p.Stock > 0)) {
			continue
		}
		if filter.OnSale && p.OldPriceMinor <= p.PriceMinor {
			continue
		}
		if filter.NewOnly && !containsFold(p.Badges, "Новинка") {
			continue
		}
		matchesTags := true
		for wanted := range tagSet {
			found := false
			for _, tag := range p.Tags {
				if strings.ToLower(tag) == wanted {
					found = true
					break
				}
			}
			if !found {
				matchesTags = false
				break
			}
		}
		if !matchesTags {
			continue
		}
		result = append(result, p)
	}
	return result
}

func (s *Store) findProduct(idOrSlug string) (domain.Product, bool) {
	if p, ok := s.products[idOrSlug]; ok {
		return p, true
	}
	for _, p := range s.products {
		if p.Slug == idOrSlug || strings.EqualFold(p.SKU, idOrSlug) {
			return p, true
		}
	}
	return domain.Product{}, false
}

func (s *Store) buildCart(userID string) domain.Cart {
	rows := s.carts[userID]
	items := make([]domain.CartItem, 0, len(rows))
	var updated time.Time
	for id, row := range rows {
		p, ok := s.products[id]
		if !ok {
			continue
		}
		item := domain.CartItem{Product: cloneProduct(p), Quantity: row.quantity, Selected: row.selected, Available: p.Stock, LineTotalMinor: int64(row.quantity) * p.PriceMinor}
		if p.Stock == 0 {
			item.Issue = "Нет в наличии"
		} else if row.quantity > p.Stock {
			item.Issue = fmt.Sprintf("Доступно только %d", p.Stock)
		}
		if row.updated.After(updated) {
			updated = row.updated
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Product.Manufacturer.Name == items[j].Product.Manufacturer.Name {
			return items[i].Product.Name < items[j].Product.Name
		}
		return items[i].Product.Manufacturer.Name < items[j].Product.Manufacturer.Name
	})
	groupsByID := map[string]int{}
	groups := []domain.CartGroup{}
	summary := domain.CartSummary{PositionCount: len(items)}
	for _, item := range items {
		summary.ItemCount += item.Quantity
		if item.Issue != "" {
			summary.UnavailableCount++
		}
		idx, ok := groupsByID[item.Product.Manufacturer.ID]
		if !ok {
			idx = len(groups)
			groupsByID[item.Product.Manufacturer.ID] = idx
			groups = append(groups, domain.CartGroup{Manufacturer: item.Product.Manufacturer, Items: []domain.CartItem{}})
		}
		groups[idx].Items = append(groups[idx].Items, item)
		if item.Selected {
			summary.SelectedCount += item.Quantity
			if item.Issue == "" {
				originalLineTotal := int64(item.Quantity) * domain.ListPriceMinor(item.Product)
				summary.SubtotalMinor += originalLineTotal
				summary.DiscountMinor += originalLineTotal - item.LineTotalMinor
				groups[idx].SubtotalMinor += item.LineTotalMinor
			}
			groups[idx].SelectedCount += item.Quantity
		}
	}
	summary.DeliveryMinor = domain.DeliveryMinor(summary.SubtotalMinor - summary.DiscountMinor)
	summary.TotalMinor = summary.SubtotalMinor - summary.DiscountMinor + summary.DeliveryMinor
	return domain.Cart{Items: items, Groups: groups, Summary: summary, UpdatedAt: updated}
}

func (s *Store) buildFavorites(userID string) domain.Favorites {
	items := make([]domain.Product, 0, len(s.favorites[userID]))
	for id := range s.favorites[userID] {
		if p, ok := s.products[id]; ok {
			items = append(items, cloneProduct(p))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Manufacturer.Name == items[j].Manufacturer.Name {
			return items[i].Name < items[j].Name
		}
		return items[i].Manufacturer.Name < items[j].Manufacturer.Name
	})
	groups := []domain.FavoriteGroup{}
	index := map[string]int{}
	for _, p := range items {
		idx, ok := index[p.Manufacturer.ID]
		if !ok {
			idx = len(groups)
			index[p.Manufacturer.ID] = idx
			groups = append(groups, domain.FavoriteGroup{Manufacturer: p.Manufacturer, Products: []domain.Product{}})
		}
		groups[idx].Products = append(groups[idx].Products, p)
	}
	return domain.Favorites{Items: items, Groups: groups, Total: len(items)}
}

func sortProducts(items []domain.Product, sortBy string) {
	sort.SliceStable(items, func(i, j int) bool {
		switch sortBy {
		case "price_asc":
			return items[i].PriceMinor < items[j].PriceMinor
		case "price_desc":
			return items[i].PriceMinor > items[j].PriceMinor
		case "rating":
			return items[i].Rating > items[j].Rating
		case "name":
			return items[i].Name < items[j].Name
		case "new":
			return items[i].CreatedAt.After(items[j].CreatedAt)
		default:
			if items[i].Featured != items[j].Featured {
				return items[i].Featured
			}
			return items[i].ReviewsCount > items[j].ReviewsCount
		}
	})
}

func containsAny(query string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), query) {
			return true
		}
	}
	return false
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}

func orderItem(p domain.Product, quantity int) domain.OrderItem {
	return domain.OrderItem{ProductID: p.ID, SKU: p.SKU, Name: p.Name, Image: p.Image, ManufacturerID: p.Manufacturer.ID, Manufacturer: p.Manufacturer.Name, Quantity: quantity, UnitPriceMinor: p.PriceMinor, LineTotalMinor: int64(quantity) * p.PriceMinor}
}

func itemCount(items []domain.OrderItem) int {
	n := 0
	for _, item := range items {
		n += item.Quantity
	}
	return n
}
func orderTotal(items []domain.OrderItem) int64 {
	var n int64
	for _, item := range items {
		n += item.LineTotalMinor
	}
	return n
}
func cloneProduct(p domain.Product) domain.Product {
	p.Tags = append([]string(nil), p.Tags...)
	p.Badges = append([]string(nil), p.Badges...)
	p.Gallery = append([]string(nil), p.Gallery...)
	return p
}
func cloneProducts(items []domain.Product) []domain.Product {
	result := make([]domain.Product, len(items))
	for i, p := range items {
		result[i] = cloneProduct(p)
	}
	return result
}
func cloneCollections(items []domain.Collection) []domain.Collection {
	result := make([]domain.Collection, len(items))
	for i, c := range items {
		c.ProductIDs = append([]string(nil), c.ProductIDs...)
		result[i] = c
	}
	return result
}
func cloneOrder(order domain.Order) domain.Order {
	order.Items = append([]domain.OrderItem(nil), order.Items...)
	return order
}
