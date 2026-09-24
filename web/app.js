(() => {
  "use strict";

  const API_BASE = "/api";
  const CATALOG_LIMIT = 24;
  const qs = (selector, scope = document) => scope.querySelector(selector);
  const qsa = (selector, scope = document) => [...scope.querySelectorAll(selector)];
  const icon = (name, cls = "") => `<svg class="icon ${cls}" aria-hidden="true"><use href="#i-${name}"></use></svg>`;
  const money = value => new Intl.NumberFormat("ru-RU", { style: "currency", currency: "RUB", maximumFractionDigits: 0 }).format((Number(value) || 0) / 100);
  const plural = (n, forms) => forms[n % 10 === 1 && n % 100 !== 11 ? 0 : n % 10 >= 2 && n % 10 <= 4 && (n % 100 < 10 || n % 100 >= 20) ? 1 : 2];
  const escapeHTML = value => String(value ?? "").replace(/[&<>'"]/g, char => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" })[char]);
  const debounce = (fn, wait = 180) => { let timer; return (...args) => { clearTimeout(timer); timer = setTimeout(() => fn(...args), wait); }; };
  const byId = id => state.products.find(product => String(product.id) === String(id));
  const categoryById = id => state.categories.find(category => category.id === id);
  const manufacturerById = id => state.manufacturers.find(manufacturer => manufacturer.id === id);

  const DEMO = {
    categories: [
      { id: "chocolate", name: "Шоколад", glyph: "▤", tint: "#e4d7c8", description: "Плитки, драже и шоколадные конфеты" },
      { id: "cookies", name: "Печенье", glyph: "◌", tint: "#e8dbc3", description: "Сдобное, песочное и овсяное" },
      { id: "waffles", name: "Вафли", glyph: "▦", tint: "#e5dfc5", description: "Классические и в шоколаде" },
      { id: "candy", name: "Конфеты", glyph: "◇", tint: "#d9e4d3", description: "Пралине, суфле и карамель" },
      { id: "marshmallow", name: "Зефир и пастила", glyph: "◍", tint: "#e8dce3", description: "Воздушные и фруктовые" },
      { id: "gifts", name: "Подарки", glyph: "□", tint: "#d6e4e1", description: "Наборы и красивые боксы" }
    ],
    manufacturers: [
      { id: "volga", name: "Volga Cacao", country: "Россия", count: 18, series: ["Origins", "Pure", "Nuts"] },
      { id: "sever", name: "Северная", country: "Россия", count: 24, series: ["Арктика", "Ягоды", "Классика"] },
      { id: "atelier", name: "Atelier 17", country: "Бельгия", count: 14, series: ["Noir", "Praliné", "Carré"] },
      { id: "morand", name: "Maison Morand", country: "Франция", count: 12, series: ["Sablé", "Matin", "Petit"] },
      { id: "greenmill", name: "Green Mill", country: "Россия", count: 21, series: ["Oat", "Free", "Simple"] },
      { id: "torino", name: "Torino Dolci", country: "Италия", count: 16, series: ["Nocciola", "Crispi", "Classico"] },
      { id: "pastila", name: "Пастила Lab", country: "Россия", count: 11, series: ["Сад", "Без сахара", "Ягоды"] },
      { id: "forma", name: "Forma", country: "Россия", count: 9, series: ["Object", "Mono", "Gift"] }
    ],
    collections: [
      { id: "bean-to-bar", name: "Bean-to-bar: вкус какао без лишнего", description: "Редкие сорта какао и короткий состав", color: "#c7d7b8", productIds: [1, 2, 8, 15] },
      { id: "tea", name: "К чаю на каждый день", description: "Печенье, вафли и мягкие конфеты", color: "#dfd2bd", productIds: [3, 4, 9, 14, 17] },
      { id: "no-sugar", name: "Без добавленного сахара", description: "Чистый вкус и понятный состав", color: "#cbdedb", productIds: [2, 6, 11, 15] },
      { id: "gifts", name: "Подарки без повода", description: "Готовые наборы с аккуратной упаковкой", color: "#ded5df", productIds: [7, 10, 16] }
    ],
    products: [
      { id: 1, slug: "volga-amazonas-72", name: "Шоколад тёмный Amazonas 72%", categoryId: "chocolate", manufacturerId: "volga", series: "Origins", weight: "90 г", priceMinor: 48900, oldPriceMinor: 0, stock: 42, tags: ["new", "vegan"], rating: 4.9, art: ["bar", "#d8d0bd", "#34251f", "72"] },
      { id: 2, slug: "volga-pure-85", name: "Шоколад горький Pure 85% без сахара", categoryId: "chocolate", manufacturerId: "volga", series: "Pure", weight: "80 г", priceMinor: 52900, oldPriceMinor: 57900, stock: 15, tags: ["sale", "no-sugar"], rating: 4.8, art: ["bar", "#cbd6cb", "#1e2c26", "85"] },
      { id: 3, slug: "morand-sable-butter", name: "Печенье Sablé сливочное", categoryId: "cookies", manufacturerId: "morand", series: "Sablé", weight: "160 г", priceMinor: 41900, oldPriceMinor: 0, stock: 37, tags: ["hit"], rating: 4.7, art: ["box", "#e5dac4", "#a56e45", "M"] },
      { id: 4, slug: "torino-nocciola-wafer", name: "Вафли Nocciola с фундуком", categoryId: "waffles", manufacturerId: "torino", series: "Nocciola", weight: "180 г", priceMinor: 35900, oldPriceMinor: 39900, stock: 8, tags: ["sale"], rating: 4.8, art: ["box", "#ded4c0", "#7a3e28", "T"] },
      { id: 5, slug: "atelier-noir-truffles", name: "Трюфели Noir с морской солью", categoryId: "candy", manufacturerId: "atelier", series: "Noir", weight: "125 г", priceMinor: 89900, oldPriceMinor: 0, stock: 22, tags: ["hit"], rating: 5, art: ["round", "#d9d4ce", "#242321", "17"] },
      { id: 6, slug: "pastila-apple", name: "Пастила яблочная без сахара", categoryId: "marshmallow", manufacturerId: "pastila", series: "Без сахара", weight: "120 г", priceMinor: 27900, oldPriceMinor: 0, stock: 51, tags: ["no-sugar", "vegan"], rating: 4.6, art: ["pouch", "#dce4d0", "#6c8b5e", "П"] },
      { id: 7, slug: "forma-mono-gift", name: "Набор конфет Mono, 16 вкусов", categoryId: "gifts", manufacturerId: "forma", series: "Mono", weight: "240 г", priceMinor: 159000, oldPriceMinor: 0, stock: 6, tags: ["new"], rating: 4.9, art: ["box", "#d5d8db", "#252d32", "F"] },
      { id: 8, slug: "atelier-carre-milk", name: "Шоколад молочный Carré 45%", categoryId: "chocolate", manufacturerId: "atelier", series: "Carré", weight: "100 г", priceMinor: 64900, oldPriceMinor: 0, stock: 27, tags: [], rating: 4.7, art: ["bar", "#e0d1c2", "#72513d", "45"] },
      { id: 9, slug: "greenmill-oat-cranberry", name: "Печенье овсяное с клюквой", categoryId: "cookies", manufacturerId: "greenmill", series: "Oat", weight: "200 г", priceMinor: 32900, oldPriceMinor: 0, stock: 33, tags: ["vegan"], rating: 4.6, art: ["pouch", "#d8ddc9", "#76834f", "G"] },
      { id: 10, slug: "sever-berry-box", name: "Ассорти конфет Северные ягоды", categoryId: "gifts", manufacturerId: "sever", series: "Ягоды", weight: "320 г", priceMinor: 119000, oldPriceMinor: 135000, stock: 4, tags: ["sale"], rating: 4.9, art: ["box", "#d9d2dc", "#593f63", "С"] },
      { id: 11, slug: "greenmill-free-coconut", name: "Конфеты кокосовые без сахара", categoryId: "candy", manufacturerId: "greenmill", series: "Free", weight: "140 г", priceMinor: 46900, oldPriceMinor: 0, stock: 0, tags: ["no-sugar"], rating: 4.5, art: ["pouch", "#d5dfda", "#477266", "G"] },
      { id: 12, slug: "sever-arctic-marshmallow", name: "Зефир ванильный Арктика", categoryId: "marshmallow", manufacturerId: "sever", series: "Арктика", weight: "250 г", priceMinor: 38900, oldPriceMinor: 0, stock: 18, tags: ["hit"], rating: 4.8, art: ["round", "#dde2e4", "#8ba0aa", "С"] },
      { id: 13, slug: "torino-crispi-dark", name: "Вафли Crispi в тёмном шоколаде", categoryId: "waffles", manufacturerId: "torino", series: "Crispi", weight: "150 г", priceMinor: 39900, oldPriceMinor: 44900, stock: 12, tags: ["sale", "hit"], rating: 4.8, art: ["box", "#d8cdbf", "#483027", "T"] },
      { id: 14, slug: "morand-petit-cacao", name: "Печенье Petit с какао", categoryId: "cookies", manufacturerId: "morand", series: "Petit", weight: "175 г", priceMinor: 37900, oldPriceMinor: 0, stock: 45, tags: [], rating: 4.6, art: ["box", "#dccfc2", "#694b3c", "M"] },
      { id: 15, slug: "volga-nibs-70", name: "Шоколад 70% с какао-крупкой", categoryId: "chocolate", manufacturerId: "volga", series: "Origins", weight: "90 г", priceMinor: 54900, oldPriceMinor: 0, stock: 19, tags: ["new", "vegan"], rating: 4.9, art: ["bar", "#cbd0c3", "#354034", "70"] },
      { id: 16, slug: "forma-object-mini", name: "Мини-набор шоколадных объектов", categoryId: "gifts", manufacturerId: "forma", series: "Object", weight: "180 г", priceMinor: 139000, oldPriceMinor: 0, stock: 9, tags: ["new"], rating: 4.8, art: ["box", "#d4d5ce", "#38403d", "F"] },
      { id: 17, slug: "sever-classic-praline", name: "Конфеты пралине Классика", categoryId: "candy", manufacturerId: "sever", series: "Классика", weight: "210 г", priceMinor: 57900, oldPriceMinor: 0, stock: 29, tags: [], rating: 4.7, art: ["round", "#d9d0c5", "#7d5441", "С"] },
      { id: 18, slug: "pastila-garden-pear", name: "Зефир груша и травы", categoryId: "marshmallow", manufacturerId: "pastila", series: "Сад", weight: "190 г", priceMinor: 43900, oldPriceMinor: 0, stock: 7, tags: ["new", "vegan"], rating: 4.7, art: ["pouch", "#dbe0cf", "#7b885c", "П"] }
    ]
  };

  const defaultCart = [
    [1, 3], [2, 2], [3, 4], [4, 2], [5, 1], [6, 3], [7, 1], [9, 2], [10, 1], [12, 2], [13, 5], [14, 2]
  ].map(([productId, quantity], index) => ({ id: `local-${index + 1}`, productId, quantity, selected: true }));

  const safeParse = (key, fallback) => {
    try { const value = JSON.parse(localStorage.getItem(key)); return value ?? fallback; } catch { return fallback; }
  };

  const state = {
    categories: DEMO.categories,
    manufacturers: DEMO.manufacturers,
    collections: DEMO.collections,
    products: [...DEMO.products],
    homeProductIds: [],
    favorites: safeParse("sloy:favorites", [2, 5, 7, 10, 13, 18]),
    cart: safeParse("sloy:cart", defaultCart),
    orders: [],
    user: null,
    token: localStorage.getItem("sloy:token") || "",
    apiOnline: false,
    loading: true,
    route: "home",
    catalog: { search: "", category: "", manufacturer: "", collection: "", inStock: false, sale: false, newOnly: false, minPrice: "", maxPrice: "", sort: "popular", view: "grid", page: 1 },
    favoriteQuery: "",
    favoriteGroup: "manufacturer",
    cartQuery: "",
    collapsedGroups: new Set(),
    repeatDraft: null,
    searchIndex: -1,
    megaCategory: "chocolate",
    catalogRemote: {
      items: [],
      total: 0,
      totalPages: 1,
      page: 1,
      loading: false,
      error: "",
      requestSeq: 0,
      controller: null,
      queryKey: ""
    }
  };

  const API = {
    async request(path, options = {}) {
      const { timeout = 5000, signal: externalSignal, ...requestOptions } = options;
      const controller = new AbortController();
      const abortFromExternal = () => controller.abort();
      if (externalSignal?.aborted) controller.abort();
      else externalSignal?.addEventListener("abort", abortFromExternal, { once: true });
      const timer = setTimeout(() => controller.abort(), timeout);
      const headers = { Accept: "application/json", ...(requestOptions.body ? { "Content-Type": "application/json" } : {}), ...(requestOptions.headers || {}) };
      if (state.token) headers.Authorization = `Bearer ${state.token}`;
      try {
        const response = await fetch(`${API_BASE}${path}`, { ...requestOptions, headers, signal: controller.signal });
        if (!response.ok) {
          let payload = null;
          try { payload = await response.json(); } catch { /* response has no JSON body */ }
          const error = new Error(payload?.error?.message || `HTTP ${response.status}`);
          error.status = response.status;
          error.payload = payload;
          throw error;
        }
        if (response.status === 204) return null;
        return await response.json();
      } finally {
        clearTimeout(timer);
        externalSignal?.removeEventListener("abort", abortFromExternal);
      }
    },
    get(path, options = {}) { return this.request(path, options); },
    post(path, body) { return this.request(path, { method: "POST", body: JSON.stringify(body) }); },
    put(path, body = {}) { return this.request(path, { method: "PUT", body: JSON.stringify(body) }); },
    patch(path, body) { return this.request(path, { method: "PATCH", body: JSON.stringify(body) }); },
    delete(path) { return this.request(path, { method: "DELETE" }); }
  };

  function unpack(data, keys = []) {
    if (Array.isArray(data)) return data;
    for (const key of [...keys, "items", "data", "results"]) if (Array.isArray(data?.[key])) return data[key];
    return [];
  }

  function cartItemsFromResponse(data) {
    return unpack(data, ["cart", "cartItems"]).map(item => {
      const productId = item.productId ?? item.product_id ?? item.product?.id;
      if (item.product) mergeProductRegistry([item.product]);
      return { id: productId, productId, quantity: Number(item.quantity || 1), selected: item.selected !== false };
    });
  }

  function applyCartResponse(data) {
    state.cart = cartItemsFromResponse(data);
    persist();
  }

  function favoriteIDsFromResponse(data) {
    return unpack(data, ["favorites", "products"]).map(item => {
      const rawProduct = item.product || (item.id && item.name ? item : null);
      const productId = item.productId ?? item.product_id ?? rawProduct?.id ?? item.id;
      if (rawProduct) mergeProductRegistry([rawProduct]);
      return productId;
    });
  }

  function applyFavoritesResponse(data) {
    state.favorites = favoriteIDsFromResponse(data);
    persist();
  }

  function normalizeProduct(raw) {
    const demo = DEMO.products.find(item => String(item.id) === String(raw.id)) || {};
    const category = raw.categoryId ?? raw.category_id ?? raw.category?.id ?? demo.categoryId;
    const manufacturer = raw.manufacturerId ?? raw.manufacturer_id ?? raw.manufacturer?.id ?? demo.manufacturerId;
    const priceMinor = Number(raw.priceMinor ?? raw.price_minor ?? raw.price ?? demo.priceMinor ?? 0);
    const oldPriceMinor = Number(raw.oldPriceMinor ?? raw.old_price_minor ?? demo.oldPriceMinor ?? 0);
    const badges = raw.badges ?? [];
    const tags = [...(raw.tags ?? demo.tags ?? []), ...badges];
    if (badges.some(badge => /нов/i.test(String(badge)))) tags.push("new");
    if (badges.some(badge => /хит|бестселлер|выбор покупателей/i.test(String(badge)))) tags.push("hit");
    if (oldPriceMinor > priceMinor) tags.push("sale");
    return {
      ...demo,
      ...raw,
      id: raw.id ?? demo.id,
      slug: raw.slug ?? demo.slug ?? String(raw.id),
      name: raw.name ?? raw.title ?? demo.name ?? "Товар",
      categoryId: String(category ?? "chocolate"),
      manufacturerId: String(manufacturer ?? "volga"),
      series: raw.series?.name ?? raw.series ?? raw.collectionName ?? demo.series ?? "Основная серия",
      weight: raw.weight ?? (raw.weightGrams ? `${raw.weightGrams} г` : raw.unit) ?? demo.weight ?? "1 шт.",
      priceMinor,
      oldPriceMinor,
      stock: Number(raw.stock ?? raw.stockQuantity ?? raw.stock_quantity ?? demo.stock ?? 0),
      tags: [...new Set(tags)],
      rating: Number(raw.rating ?? demo.rating ?? 4.7),
      art: raw.art ?? demo.art ?? ["box", "#ddd8cc", "#4c433a", "С"],
      brand: raw.brand ?? demo.brand,
      manufacturerName: raw.manufacturer?.name ?? demo.manufacturerName,
      categoryName: raw.category?.name ?? demo.categoryName
    };
  }

  function mergeProductRegistry(rawItems) {
    return rawItems.filter(Boolean).map(raw => {
      const normalized = normalizeProduct(raw);
      const current = byId(normalized.id);
      if (current) {
        Object.assign(current, normalized);
        return current;
      }
      state.products.push(normalized);
      return normalized;
    });
  }

  function discoveryProducts() {
    if (!state.apiOnline || !state.homeProductIds.length) return state.products;
    const ids = new Set(state.homeProductIds.map(String));
    return state.products.filter(product => ids.has(String(product.id)));
  }

  function normalizeNamed(raw, fallback) {
    return { ...fallback, ...raw, id: String(raw.id ?? raw.slug ?? fallback?.id), name: raw.name ?? raw.title ?? fallback?.name };
  }

  function collectionColor(raw, fallback) {
    const accents = { lime: "#d9e7b8", cocoa: "#ded0c2", coral: "#ead0c7", sky: "#cfe0e2" };
    return raw.color ?? accents[raw.accent] ?? fallback?.color ?? "#ddd";
  }

  async function loadInitialData() {
    try {
      const [catalog, categories, manufacturers, collections] = await Promise.all([
        API.get(`/catalog?limit=${CATALOG_LIMIT}`), API.get("/categories"), API.get("/manufacturers"), API.get("/collections")
      ]);
      const products = unpack(catalog, ["products"]);
      const categoryItems = unpack(categories, ["categories"]);
      const manufacturerItems = unpack(manufacturers, ["manufacturers"]);
      const collectionItems = unpack(collections, ["collections"]);
      if (products.length) {
        const initialProducts = mergeProductRegistry(products);
        state.homeProductIds = initialProducts.map(product => product.id);
        if (!state.token) {
          const apiProductIds = new Set(initialProducts.map(product => String(product.id)));
          const validCart = state.cart.filter(item => apiProductIds.has(String(item.productId)));
          state.cart = validCart.length ? validCart : initialProducts.slice(0, 12).map((product, index) => ({ id: product.id, productId: product.id, quantity: [3,2,4,2,1,3,1,2,1,2,5,2][index] || 1, selected: product.stock > 0 }));
          const validFavorites = state.favorites.filter(id => apiProductIds.has(String(id)));
          state.favorites = validFavorites.length ? validFavorites : [1,4,6,9,12,15].map(index => initialProducts[index]?.id).filter(Boolean);
          persist();
        }
      }
      if (categoryItems.length) state.categories = categoryItems.map((item, index) => normalizeNamed(item, DEMO.categories[index] || DEMO.categories[0]));
      if (manufacturerItems.length) state.manufacturers = manufacturerItems.map((item, index) => normalizeNamed(item, DEMO.manufacturers[index] || DEMO.manufacturers[0]));
      if (collectionItems.length) state.collections = collectionItems.map((item, index) => ({ ...normalizeNamed(item, DEMO.collections[index] || DEMO.collections[0]), productIds: item.productIds ?? item.product_ids ?? DEMO.collections[index]?.productIds ?? [], color: collectionColor(item, DEMO.collections[index]) }));
      if (!state.categories.some(category => category.id === state.megaCategory)) state.megaCategory = state.categories.find(category => !category.parentId && productsForCategory(category.id).length)?.id || state.categories.find(category => productsForCategory(category.id).length)?.id || state.categories[0]?.id;
      state.apiOnline = true;
      if (state.token) await loadAccountData();
    } catch (error) {
      state.apiOnline = false;
      console.info("Слой: API пока недоступен, включены локальные данные.", error?.message || error);
    } finally {
      state.loading = false;
      render();
      renderMegaMenu();
      updateBadges();
      if (state.apiOnline && state.route === "catalog") queueCatalogFetch({ immediate: true });
    }
  }

  async function loadAccountData() {
    try {
      const [user, cart, favorites, orders] = await Promise.all([API.get("/me"), API.get("/cart"), API.get("/favorites"), API.get("/orders")]);
      if (user) state.user = user.user || user;
      state.cart = cartItemsFromResponse(cart);
      state.favorites = favoriteIDsFromResponse(favorites);
      const orderItems = unpack(orders, ["orders"]);
      state.orders = orderItems.map(normalizeOrder);
      persist();
      return true;
    } catch (error) {
      if (error?.status === 401) {
        state.token = "";
        state.user = null;
        state.orders = [];
		state.cart = [];
		state.favorites = [];
        localStorage.removeItem("sloy:token");
		persist();
      }
      console.info("Не удалось синхронизировать профиль", error);
      return false;
    }
  }

  function normalizeOrder(order) {
    return {
      ...order,
      apiId: order.id,
      id: order.number ?? order.id,
      createdAt: order.createdAt ?? order.created_at ?? new Date().toISOString(),
      totalMinor: Number(order.totalMinor ?? order.total_minor ?? order.total ?? 0),
      status: order.status ?? "delivered",
		items: (order.items || []).map(item => ({
			...item,
			productId: item.productId ?? item.product_id ?? item.product?.id,
			quantity: Number(item.quantity || 1),
			unitPriceMinor: Number(item.unitPriceMinor ?? item.unit_price_minor ?? item.product?.priceMinor ?? 0),
			lineTotalMinor: Number(item.lineTotalMinor ?? item.line_total_minor ?? 0),
			manufacturerId: item.manufacturerId ?? item.manufacturer_id ?? item.product?.manufacturer?.id ?? "",
			manufacturer: item.manufacturer ?? item.product?.manufacturer?.name ?? "",
			name: item.name ?? item.product?.name ?? "Товар из заказа"
		}))
    };
  }

  function persist() {
    localStorage.setItem("sloy:favorites", JSON.stringify(state.favorites));
    localStorage.setItem("sloy:cart", JSON.stringify(state.cart));
    updateBadges();
  }

  function artVars(product) {
    const [shape, background, pack, mark] = product.art || ["box", "#ddd8cc", "#4c433a", "С"];
    return { shape, style: `--art-bg:${escapeHTML(background)};--pack:${escapeHTML(pack)};--art-mark:'${escapeHTML(mark)}';` };
  }

  function productArt(product) {
    const category = `${product.categoryId} ${categoryName(product)}`.toLocaleLowerCase("ru");
    const src = /зеф|паст|marsh/.test(category) ? "/assets/product-marshmallow.webp" : /подар|gift|печ|cookie/.test(category) ? "/assets/product-giftbox.webp" : "/assets/product-chocolate.webp";
    return `<span class="product-photo" aria-hidden="true" style="background-image:url('${src}')"></span>`;
  }

  function manufacturerName(product) {
    return manufacturerById(product.manufacturerId)?.name || product.manufacturerName || "Мануфактура";
  }

  function categoryName(product) {
    return categoryById(product.categoryId)?.name || product.categoryName || "Сладости";
  }

  function categoryMatches(product, categoryId) {
    if (!categoryId) return true;
    if (product.categoryId === categoryId) return true;
    let current = categoryById(product.categoryId);
    const seen = new Set();
    while (current?.parentId && !seen.has(current.id)) {
      if (current.parentId === categoryId) return true;
      seen.add(current.id);
      current = categoryById(current.parentId);
    }
    return false;
  }

  const productsForCategory = categoryId => discoveryProducts().filter(product => categoryMatches(product, categoryId));

  function tagLabel(tag) {
    return ({ new: "Новинка", sale: "Скидка", hit: "Хит", vegan: "Vegan", "no-sugar": "Без сахара" })[tag] || tag;
  }

  function productCard(product) {
    const favorite = state.favorites.some(id => String(id) === String(product.id));
    const cartItem = state.cart.find(item => String(item.productId) === String(product.id));
    const art = artVars(product);
    const visibleTags = (product.tags || []).filter(tag => ["new", "sale", "hit"].includes(tag)).slice(0, 2);
    return `
      <article class="product-card ${product.stock <= 0 ? "is-unavailable" : ""}" data-product-card="${escapeHTML(product.id)}">
        <div class="product-media" style="--art-bg:${art.style.split("--art-bg:")[1]?.split(";")[0] || "#ddd"}" data-product="${escapeHTML(product.id)}" role="button" tabindex="0" aria-label="Открыть ${escapeHTML(product.name)}">
          ${productArt(product)}
          <div class="product-badges">${visibleTags.map(tag => `<span class="product-badge ${tag}">${tagLabel(tag)}</span>`).join("")}</div>
          <button class="favorite-button ${favorite ? "active" : ""}" data-favorite="${escapeHTML(product.id)}" aria-label="${favorite ? "Убрать из избранного" : "Добавить в избранное"}" aria-pressed="${favorite}">
            ${icon(favorite ? "heart-fill" : "heart")}
          </button>
        </div>
        <div class="product-body">
          <span class="product-brand">${escapeHTML(manufacturerName(product))} · ${escapeHTML(product.series)}</span>
          <strong class="product-name" data-product="${escapeHTML(product.id)}" tabindex="0">${escapeHTML(product.name)}</strong>
          <div class="product-meta"><span>${escapeHTML(product.weight)}</span><span>★ ${product.rating.toFixed(1)}</span>${product.stock > 0 && product.stock < 10 ? `<span><i class="stock-dot low"></i>мало</span>` : ""}</div>
          <div class="product-footer">
            <div class="price">${money(product.priceMinor)}${product.oldPriceMinor ? `<span class="old-price">${money(product.oldPriceMinor)}</span>` : ""}</div>
            <button class="add-button ${cartItem ? "added" : ""}" data-add-cart="${escapeHTML(product.id)}" ${product.stock <= 0 ? "disabled" : ""} aria-label="${product.stock <= 0 ? "Нет в наличии" : cartItem ? `В корзине, ${cartItem.quantity} шт.` : "Добавить в корзину"}">
              ${product.stock <= 0 ? "Нет" : cartItem ? `${icon("check")}<span>${cartItem.quantity} шт.</span>` : `${icon("plus")}<span>В корзину</span>`}
            </button>
          </div>
        </div>
      </article>`;
  }

  function pageFooter() {
    return `
      <footer class="site-footer">
        <div class="shell footer-grid">
          <div class="footer-brand">
            <button class="logo footer-logo" data-route="home" aria-label="Слой, на главную">
              <svg viewBox="0 0 42 42" aria-hidden="true"><rect width="42" height="42" rx="13"/><path d="M10 14.5h22L21 29z"/></svg><span>слой</span>
            </button>
            <p>Кондитерские изделия с понятным составом и честным вкусом.</p>
          </div>
          <div><strong>Покупателям</strong><button data-route="catalog">Каталог</button><button data-route="delivery">Доставка и оплата</button><button data-route="orders">Мои заказы</button></div>
          <div><strong>Каталог</strong>${state.categories.slice(0, 4).map(category => `<button data-category="${category.id}">${escapeHTML(category.name)}</button>`).join("")}</div>
          <div><strong>Поддержка</strong><a href="tel:+74951234567">+7 495 123-45-67</a><a href="mailto:hello@sloy.store">hello@sloy.store</a><span>Ежедневно, 09:00–21:00</span></div>
        </div>
        <div class="shell footer-bottom"><span>© 2026 Слой</span><span>Цены указаны в рублях, включая НДС</span></div>
      </footer>`;
  }

  function renderHome() {
    const popular = [...discoveryProducts()].sort((a, b) => b.rating - a.rating).slice(0, 8);
    return `
      <div class="hero">
        <div class="shell">
          <section class="hero-card" aria-labelledby="hero-title">
            <div class="hero-copy">
              <span class="eyebrow">Кондитерская коллекция</span>
              <h1 id="hero-title">Хороший вкус. Ничего лишнего.</h1>
              <p>Шоколад, печенье и конфеты от ${state.manufacturers.length} ${plural(state.manufacturers.length,["производителя","производителей","производителей"])} — с быстрым поиском по бренду, серии и составу.</p>
              <div class="hero-actions">
                <button class="button-primary" data-route="catalog">Перейти в каталог ${icon("arrow")}</button>
                <button class="button-secondary" data-collection="${escapeHTML(state.collections[0]?.id || "bean-to-bar")}">Выбор редакции</button>
              </div>
            </div>
            <div class="hero-note"><strong>Доставка сегодня</strong>Закажите до 16:00 — привезём с 19:00 до 22:00.</div>
          </section>
          <div class="trust-strip" aria-label="Преимущества магазина">
            <div class="trust-item">${icon("box")}<div><strong>${state.manufacturers.length} ${plural(state.manufacturers.length,["производитель","производителя","производителей"])}</strong><span>От крупных фабрик до мануфактур</span></div></div>
            <div class="trust-item">${icon("check")}<div><strong>Свежие партии</strong><span>Контролируем сроки и хранение</span></div></div>
            <div class="trust-item">${icon("location")}<div><strong>Доставка от 2 часов</strong><span>И точные интервалы по городу</span></div></div>
            <div class="trust-item">${icon("copy")}<div><strong>Повтор за минуту</strong><span>С проверкой цены и наличия</span></div></div>
          </div>
        </div>
      </div>

      <div class="shell">
        <section class="section">
          <div class="section-head"><div><span class="section-kicker">Каталог</span><h2>Найдите свой вкус</h2></div><button class="text-link" data-route="catalog">Все товары <span>→</span></button></div>
          <div class="category-grid">
            ${state.categories.map(category => { const count = Number(category.productCount ?? productsForCategory(category.id).length); return `<button class="category-tile" style="--tile:${escapeHTML(category.tint || "#e4ddd1")}" data-category="${escapeHTML(category.id)}"><span class="category-glyph">${escapeHTML(category.glyph || "◇")}</span><span><strong>${escapeHTML(category.name)}</strong><small>${count} ${plural(count, ["товар", "товара", "товаров"])}</small></span></button>`; }).join("")}
          </div>
        </section>

        <section class="section">
          <div class="section-head"><div><span class="section-kicker">Подборки</span><h2>Выбрали за вас</h2></div></div>
          <div class="collections-grid">
            ${state.collections.slice(0, 3).map((collection, index) => { const count = Number(collection.productCount ?? collection.productIds?.length ?? 0); return `<button class="collection-card" style="--collection-bg:${escapeHTML(collection.color || "#ddd")}" data-collection="${escapeHTML(collection.id)}"><img class="collection-photo" src="/assets/${["product-chocolate.webp","product-giftbox.webp","product-marshmallow.webp"][index]}" alt="" loading="lazy"><span class="eyebrow">${index === 0 ? "Главная подборка" : "Сценарий"}</span><h3>${escapeHTML(collection.name)}</h3><p>${escapeHTML(collection.description || "Товары, собранные по вкусу и случаю")}</p><span class="collection-count">${count} ${plural(count, ["товар", "товара", "товаров"])} →</span></button>`; }).join("")}
          </div>
        </section>

        <section class="section">
          <div class="section-head"><div><span class="section-kicker">Популярное</span><h2>Берут чаще всего</h2></div><button class="text-link" data-route="catalog">Смотреть всё <span>→</span></button></div>
          <div class="horizontal-scroll">${popular.map(productCard).join("")}</div>
        </section>

        <section class="section">
          <div class="section-head"><div><span class="section-kicker">Производители</span><h2>От фабрик до мастерских</h2></div><button class="text-link" data-route="catalog">Все бренды <span>→</span></button></div>
          <div class="manufacturer-rail">${state.manufacturers.map(manufacturer => `<button class="manufacturer-chip" data-manufacturer="${escapeHTML(manufacturer.id)}"><span class="manufacturer-monogram">${escapeHTML(manufacturer.name)}</span><small>${escapeHTML(manufacturer.country || "")} · ${manufacturer.productCount ?? manufacturer.count ?? discoveryProducts().filter(product => product.manufacturerId === manufacturer.id).length} товаров</small></button>`).join("")}</div>
        </section>
      </div>
      ${pageFooter()}`;
  }

  function parseHash() {
    const raw = location.hash.replace(/^#\/?/, "") || "home";
    const [path, query = ""] = raw.split("?");
    const params = new URLSearchParams(query);
    state.route = path || "home";
    if (state.route === "catalog") {
      const page = Number.parseInt(params.get("page") || "1", 10);
      const sort = params.get("sort") || "popular";
      const view = params.get("view") || "grid";
      Object.assign(state.catalog, {
        search: params.get("q") || "",
        category: params.get("category") || "",
        manufacturer: params.get("manufacturer") || "",
        collection: params.get("collection") || "",
        inStock: ["true", "1"].includes(params.get("inStock")),
        sale: ["true", "1"].includes(params.get("sale")),
        newOnly: ["true", "1"].includes(params.get("newOnly") ?? params.get("new")),
        minPrice: /^\d+(?:\.\d{1,2})?$/.test(params.get("minPrice") || "") ? params.get("minPrice") : "",
        maxPrice: /^\d+(?:\.\d{1,2})?$/.test(params.get("maxPrice") || "") ? params.get("maxPrice") : "",
        sort: ["popular", "priceAsc", "priceDesc", "new", "name"].includes(sort) ? sort : "popular",
        view: ["grid", "list"].includes(view) ? view : "grid",
        page: Number.isFinite(page) && page > 0 ? page : 1
      });
    }
  }

  function navigate(route, params = {}) {
    const query = new URLSearchParams(Object.entries(params).filter(([, value]) => value !== "" && value != null)).toString();
    const next = `#/${route}${query ? `?${query}` : ""}`;
    if (location.hash === next) {
      parseHash();
      render();
      if (state.apiOnline && state.route === "catalog") queueCatalogFetch();
      window.scrollTo({ top: 0, behavior: "smooth" });
    }
    else location.hash = next;
    closeOverlays();
  }

  function catalogHash() {
    const filters = state.catalog;
    const params = new URLSearchParams();
    if (filters.search.trim()) params.set("q", filters.search.trim());
    if (filters.category) params.set("category", filters.category);
    if (filters.manufacturer) params.set("manufacturer", filters.manufacturer);
    if (filters.collection) params.set("collection", filters.collection);
    if (filters.inStock) params.set("inStock", "true");
    if (filters.sale) params.set("sale", "true");
    if (filters.newOnly) params.set("newOnly", "true");
    if (filters.minPrice !== "") params.set("minPrice", filters.minPrice);
    if (filters.maxPrice !== "") params.set("maxPrice", filters.maxPrice);
    if (filters.sort !== "popular") params.set("sort", filters.sort);
    if (filters.view !== "grid") params.set("view", filters.view);
    if (filters.page > 1) params.set("page", String(filters.page));
    const query = params.toString();
    return `#/catalog${query ? `?${query}` : ""}`;
  }

  function commitCatalogState({ replace = false, scroll = false } = {}) {
    const next = catalogHash();
    if (location.hash === next) {
      if (state.apiOnline) queueCatalogFetch();
      else render();
    } else if (replace) {
      history.replaceState(null, "", next);
      if (state.apiOnline) queueCatalogFetch();
      else render();
    } else {
      location.hash = next;
    }
    if (scroll) requestAnimationFrame(() => qs(".catalog-heading-row")?.scrollIntoView({ behavior: "smooth", block: "start" }));
  }

  function filteredProducts() {
    const filters = state.catalog;
    const collection = state.collections.find(item => item.id === filters.collection);
    const query = filters.search.trim().toLocaleLowerCase("ru");
    let products = state.products.filter(product => {
      const searchable = `${product.name} ${manufacturerName(product)} ${product.series} ${categoryName(product)} ${(product.tags || []).join(" ")}`.toLocaleLowerCase("ru");
      return (!query || searchable.includes(query)) &&
        categoryMatches(product, filters.category) &&
        (!filters.manufacturer || product.manufacturerId === filters.manufacturer) &&
        (!collection || (collection.productIds || []).some(id => String(id) === String(product.id))) &&
        (!filters.inStock || product.stock > 0) &&
        (!filters.sale || product.oldPriceMinor > product.priceMinor) &&
        (!filters.newOnly || product.tags?.includes("new")) &&
        (!filters.minPrice || product.priceMinor >= Number(filters.minPrice) * 100) &&
        (!filters.maxPrice || product.priceMinor <= Number(filters.maxPrice) * 100);
    });
    products.sort({
      popular: (a, b) => b.rating - a.rating,
      priceAsc: (a, b) => a.priceMinor - b.priceMinor,
      priceDesc: (a, b) => b.priceMinor - a.priceMinor,
      new: (a, b) => Number(b.tags?.includes("new")) - Number(a.tags?.includes("new")),
      name: (a, b) => a.name.localeCompare(b.name, "ru")
    }[filters.sort] || ((a, b) => b.rating - a.rating));
    return products;
  }

  function backendCatalogQuery() {
    const filters = state.catalog;
    const params = new URLSearchParams();
    if (filters.search.trim()) params.set("q", filters.search.trim());
    if (filters.category) params.set("category", filters.category);
    if (filters.manufacturer) params.set("manufacturer", filters.manufacturer);
    if (filters.collection) params.set("collection", filters.collection);
    if (filters.inStock) params.set("inStock", "true");
    if (filters.sale) params.set("sale", "true");
    if (filters.newOnly) params.set("newOnly", "true");
    if (filters.minPrice !== "") params.set("minPriceMinor", String(Math.round(Number(filters.minPrice) * 100)));
    if (filters.maxPrice !== "") params.set("maxPriceMinor", String(Math.round(Number(filters.maxPrice) * 100)));
    params.set("sort", ({ popular: "popular", priceAsc: "price_asc", priceDesc: "price_desc", new: "new", name: "name" })[filters.sort] || "popular");
    params.set("page", String(Math.max(1, filters.page)));
    params.set("limit", String(CATALOG_LIMIT));
    return params.toString();
  }

  const runCatalogFetchDebounced = debounce(sequence => loadRemoteCatalog(sequence), 220);

  function queueCatalogFetch({ immediate = false, force = false } = {}) {
    if (!state.apiOnline || state.route !== "catalog") return;
    const remote = state.catalogRemote;
    const queryKey = backendCatalogQuery();
    if (!force && !remote.loading && !remote.error && remote.queryKey === queryKey) {
      render();
      refreshFilterDrawerButton();
      return;
    }
    remote.controller?.abort();
    remote.requestSeq += 1;
    remote.loading = true;
    remote.error = "";
    remote.items = [];
    remote.queryKey = "";
    render();
    refreshFilterDrawerButton();
    if (immediate) loadRemoteCatalog(remote.requestSeq);
    else runCatalogFetchDebounced(remote.requestSeq);
  }

  async function loadRemoteCatalog(sequence) {
    const remote = state.catalogRemote;
    if (!state.apiOnline || state.route !== "catalog" || sequence !== remote.requestSeq) return;
    const queryKey = backendCatalogQuery();
    const controller = new AbortController();
    remote.controller = controller;
    try {
      const data = await API.get(`/catalog?${queryKey}`, { signal: controller.signal });
      if (sequence !== remote.requestSeq || state.route !== "catalog" || queryKey !== backendCatalogQuery()) return;
      const total = Math.max(0, Number(data?.total ?? 0));
      const totalPages = Math.max(1, Number(data?.totalPages ?? data?.total_pages ?? Math.ceil(total / CATALOG_LIMIT)) || 1);
      if (state.catalog.page > totalPages) {
        remote.loading = false;
        remote.controller = null;
        state.catalog.page = totalPages;
        commitCatalogState({ replace: true });
        return;
      }
      remote.items = mergeProductRegistry(unpack(data, ["products"]));
      remote.total = total;
      remote.totalPages = totalPages;
      remote.page = Math.max(1, Number(data?.page ?? state.catalog.page) || 1);
      remote.queryKey = queryKey;
      remote.error = "";
    } catch (error) {
      if (controller.signal.aborted || sequence !== remote.requestSeq) return;
      remote.items = [];
      remote.total = 0;
      remote.totalPages = 1;
      remote.error = "Не удалось загрузить каталог. Проверьте соединение и повторите запрос.";
    } finally {
      if (sequence === remote.requestSeq) {
        remote.loading = false;
        remote.controller = null;
        if (state.route === "catalog") {
          render();
          refreshFilterDrawerButton();
        }
      }
    }
  }

  function filterOptionsMarkup() {
    const categoryCounts = Object.fromEntries(state.categories.map(category => [category.id, Number(category.productCount ?? productsForCategory(category.id).length)]));
    const manufacturerCounts = Object.fromEntries(state.manufacturers.map(manufacturer => [manufacturer.id, Number(manufacturer.productCount ?? discoveryProducts().filter(product => product.manufacturerId === manufacturer.id).length)]));
    return `
      <div class="filter-block">
        <button class="filter-title" type="button" data-action="toggle-filter-block" aria-expanded="true">Категория ${icon("chevron")}</button>
        <div class="filter-options">
          ${state.categories.map(category => `<label class="check-row ${category.parentId ? "child-option" : ""}"><input type="checkbox" data-filter="category" value="${escapeHTML(category.id)}" ${state.catalog.category === category.id ? "checked" : ""}><span class="fake-check"></span><span>${escapeHTML(category.name)}</span><small>${categoryCounts[category.id] || 0}</small></label>`).join("")}
        </div>
      </div>
      <div class="filter-block">
        <button class="filter-title" type="button" data-action="toggle-filter-block" aria-expanded="true">Производитель ${icon("chevron")}</button>
        <div class="filter-options">
          ${state.manufacturers.map(manufacturer => `<label class="check-row"><input type="checkbox" data-filter="manufacturer" value="${escapeHTML(manufacturer.id)}" ${state.catalog.manufacturer === manufacturer.id ? "checked" : ""}><span class="fake-check"></span><span>${escapeHTML(manufacturer.name)}</span><small>${manufacturerCounts[manufacturer.id] || 0}</small></label>`).join("")}
        </div>
      </div>
      <div class="filter-block">
        <button class="filter-title" type="button" data-action="toggle-filter-block" aria-expanded="true">Цена, ₽ ${icon("chevron")}</button>
        <div class="price-inputs">
          <label class="input-wrap"><span>от</span><input type="number" min="0" inputmode="numeric" data-price="minPrice" value="${escapeHTML(state.catalog.minPrice)}" aria-label="Минимальная цена"></label>
          <label class="input-wrap"><span>до</span><input type="number" min="0" inputmode="numeric" data-price="maxPrice" value="${escapeHTML(state.catalog.maxPrice)}" aria-label="Максимальная цена"></label>
        </div>
      </div>
      <div class="filter-block">
        <div class="filter-options">
          <label class="check-row"><input type="checkbox" data-boolean-filter="inStock" ${state.catalog.inStock ? "checked" : ""}><span class="fake-check"></span><span>Только в наличии</span></label>
          <label class="check-row"><input type="checkbox" data-boolean-filter="sale" ${state.catalog.sale ? "checked" : ""}><span class="fake-check"></span><span>Со скидкой</span></label>
          <label class="check-row"><input type="checkbox" data-boolean-filter="newOnly" ${state.catalog.newOnly ? "checked" : ""}><span class="fake-check"></span><span>Новинки</span></label>
        </div>
      </div>`;
  }

  function activeFilters() {
    const filters = [];
    if (state.catalog.search) filters.push(["search", `Поиск: ${state.catalog.search}`]);
    if (state.catalog.category) filters.push(["category", categoryById(state.catalog.category)?.name]);
    if (state.catalog.manufacturer) filters.push(["manufacturer", manufacturerById(state.catalog.manufacturer)?.name]);
    if (state.catalog.collection) filters.push(["collection", state.collections.find(item => item.id === state.catalog.collection)?.name]);
    if (state.catalog.inStock) filters.push(["inStock", "В наличии"]);
    if (state.catalog.sale) filters.push(["sale", "Со скидкой"]);
    if (state.catalog.newOnly) filters.push(["newOnly", "Новинки"]);
    if (state.catalog.minPrice) filters.push(["minPrice", `От ${state.catalog.minPrice} ₽`]);
    if (state.catalog.maxPrice) filters.push(["maxPrice", `До ${state.catalog.maxPrice} ₽`]);
    return filters.filter(([, value]) => value);
  }

  function paginationMarkup(totalPages, currentPage) {
    if (totalPages <= 1) return "";
    const pages = totalPages <= 7
      ? Array.from({ length: totalPages }, (_, index) => index + 1)
      : [...new Set([1, totalPages, currentPage - 2, currentPage - 1, currentPage, currentPage + 1, currentPage + 2].filter(page => page >= 1 && page <= totalPages))].sort((a, b) => a - b);
    let previous = 0;
    const buttons = pages.map(page => {
      const gap = previous && page - previous > 1 ? `<span aria-hidden="true">…</span>` : "";
      previous = page;
      return `${gap}<button class="${page === currentPage ? "active" : ""}" data-catalog-page="${page}" ${page === currentPage ? 'aria-current="page"' : ""}>${page}</button>`;
    }).join("");
    return `<nav class="pagination" aria-label="Страницы каталога"><button data-catalog-page="${currentPage - 1}" aria-label="Предыдущая страница" ${currentPage === 1 ? "disabled" : ""}>←</button>${buttons}<button data-catalog-page="${currentPage + 1}" aria-label="Следующая страница" ${currentPage === totalPages ? "disabled" : ""}>→</button></nav>`;
  }

  function catalogLoadingMarkup() {
    return `<div class="product-grid catalog-products" aria-busy="true" aria-label="Загружаем товары">${Array.from({ length: 8 }, () => `<div class="skeleton" style="height:390px;border-radius:20px"></div>`).join("")}</div>`;
  }

  function renderCatalog() {
    const isRemote = state.apiOnline;
    const remote = state.catalogRemote;
    const allProducts = isRemote ? [] : filteredProducts();
    const total = isRemote ? remote.total : allProducts.length;
    const totalPages = isRemote ? remote.totalPages : Math.max(1, Math.ceil(total / CATALOG_LIMIT));
    if (!isRemote) state.catalog.page = Math.min(totalPages, Math.max(1, state.catalog.page));
    const products = isRemote
      ? remote.items
      : allProducts.slice((state.catalog.page - 1) * CATALOG_LIMIT, state.catalog.page * CATALOG_LIMIT);
    const filters = activeFilters();
    const heading = (state.catalog.search ? `Поиск: «${state.catalog.search}»` : state.catalog.category ? categoryById(state.catalog.category)?.name : state.catalog.manufacturer ? manufacturerById(state.catalog.manufacturer)?.name : state.catalog.collection ? state.collections.find(item => item.id === state.catalog.collection)?.name : "Каталог") || "Каталог";
    const subtitle = state.catalog.manufacturer ? `${manufacturerById(state.catalog.manufacturer)?.country || ""} · серии ${manufacturerById(state.catalog.manufacturer)?.series?.join(", ") || "в каталоге"}` : "Товары собраны по категориям, производителям и сериям";
    const resultsText = remote.loading && isRemote
      ? "Обновляем…"
      : remote.error && isRemote
        ? "Не удалось загрузить"
        : `${total} ${plural(total, ["товар", "товара", "товаров"])}`;
    let resultsMarkup;
    if (isRemote && remote.loading) {
      resultsMarkup = catalogLoadingMarkup();
    } else if (isRemote && remote.error) {
      resultsMarkup = `<div class="error-box" role="alert"><strong>${escapeHTML(remote.error)}</strong><div style="margin-top:12px"><button class="button-secondary button-small" data-action="retry-catalog">Повторить</button></div></div>`;
    } else {
      resultsMarkup = `${products.length
        ? `<div class="product-grid catalog-products ${state.catalog.view === "list" ? "list-view" : ""}">${products.map(productCard).join("")}</div>`
        : emptyState("search", "Ничего не нашли", "Попробуйте убрать часть фильтров или изменить запрос.", "Сбросить фильтры", "reset-filters")}${paginationMarkup(totalPages, state.catalog.page)}`;
    }
    return `
      <div class="page catalog-page">
        <div class="shell">
          <nav class="breadcrumbs" aria-label="Хлебные крошки"><button data-route="home">Главная</button><span>Каталог</span>${heading !== "Каталог" ? `<span>${escapeHTML(heading)}</span>` : ""}</nav>
          <div class="catalog-heading-row"><div><h1 class="page-title">${escapeHTML(heading)}</h1><p class="page-subtitle">${escapeHTML(subtitle)}</p></div><span class="results-number">${resultsText}</span></div>
          <div class="quick-chips" aria-label="Быстрые фильтры">
            <button class="quick-chip ${state.catalog.inStock ? "active" : ""}" data-quick-filter="inStock">В наличии</button>
            <button class="quick-chip ${state.catalog.newOnly ? "active" : ""}" data-quick-filter="newOnly">Новинки</button>
            <button class="quick-chip ${state.catalog.sale ? "active" : ""}" data-quick-filter="sale">Со скидкой</button>
            ${state.collections.map(collection => `<button class="quick-chip ${state.catalog.collection === collection.id ? "active" : ""}" data-collection-filter="${escapeHTML(collection.id)}">${escapeHTML(collection.name)}</button>`).join("")}
          </div>
          <div class="catalog-toolbar">
            <div class="toolbar-group">
              <button class="toolbar-button mobile-only" data-action="open-filters">${icon("filter")} Фильтры ${filters.length ? `<span class="active-filter-count">${filters.length}</span>` : ""}</button>
              <button class="toolbar-button desktop-only" data-action="reset-filters" ${filters.length ? "" : "disabled"}>Сбросить ${filters.length ? `· ${filters.length}` : ""}</button>
            </div>
            <div class="toolbar-group">
              <select class="sort-select" id="catalog-sort" aria-label="Сортировка">
                <option value="popular" ${state.catalog.sort === "popular" ? "selected" : ""}>Сначала популярные</option>
                <option value="priceAsc" ${state.catalog.sort === "priceAsc" ? "selected" : ""}>Сначала дешевле</option>
                <option value="priceDesc" ${state.catalog.sort === "priceDesc" ? "selected" : ""}>Сначала дороже</option>
                <option value="new" ${state.catalog.sort === "new" ? "selected" : ""}>Сначала новинки</option>
                <option value="name" ${state.catalog.sort === "name" ? "selected" : ""}>По названию</option>
              </select>
              <div class="view-toggle" aria-label="Вид каталога"><button class="${state.catalog.view === "grid" ? "active" : ""}" data-view="grid" aria-label="Плитка">${icon("grid")}</button><button class="${state.catalog.view === "list" ? "active" : ""}" data-view="list" aria-label="Список">${icon("menu")}</button></div>
            </div>
          </div>
          <div class="catalog-layout">
            <aside class="filter-sidebar" aria-label="Фильтры каталога">${filterOptionsMarkup()}</aside>
            <div>
              ${filters.length ? `<div class="active-filters">${filters.map(([key, label]) => `<button class="filter-pill" data-remove-filter="${key}">${escapeHTML(label)} ${icon("close")}</button>`).join("")}<button class="filter-pill" data-action="reset-filters">Очистить всё</button></div>` : ""}
              ${resultsMarkup}
            </div>
          </div>
        </div>
      </div>
      ${pageFooter()}`;
  }

  function emptyState(iconName, title, text, actionLabel, action) {
    return `<div class="empty-state"><div><div class="empty-state-icon">${icon(iconName)}</div><h2>${escapeHTML(title)}</h2><p>${escapeHTML(text)}</p>${actionLabel ? `<button class="button-dark" data-action="${escapeHTML(action)}">${escapeHTML(actionLabel)}</button>` : ""}</div></div>`;
  }

  function renderFavorites() {
    const query = state.favoriteQuery.trim().toLocaleLowerCase("ru");
    const favorites = state.products.filter(product => state.favorites.some(id => String(id) === String(product.id))).filter(product => !query || `${product.name} ${manufacturerName(product)} ${product.series}`.toLocaleLowerCase("ru").includes(query));
    const keyFor = product => state.favoriteGroup === "category" ? product.categoryId : state.favoriteGroup === "manufacturer" ? product.manufacturerId : "all";
    const groups = favorites.reduce((result, product) => { const key = keyFor(product); (result[key] ||= []).push(product); return result; }, {});
    const groupTitle = key => state.favoriteGroup === "category" ? categoryById(key)?.name : state.favoriteGroup === "manufacturer" ? manufacturerById(key)?.name : "Все сохранённые товары";
    return `
      <div class="page">
        <div class="shell">
          <header class="page-header"><div><h1 class="page-title">Избранное</h1><p class="page-subtitle">${favorites.length} ${plural(favorites.length, ["сохранённый товар", "сохранённых товара", "сохранённых товаров"])} — легко найти по бренду или категории</p></div>${favorites.length ? `<button class="button-secondary button-small" data-action="favorites-to-cart">Добавить доступные в корзину</button>` : ""}</header>
          ${state.favorites.length ? `
            <div class="favorites-toolbar">
              <label class="inline-search">${icon("search")}<input id="favorite-search" type="search" value="${escapeHTML(state.favoriteQuery)}" placeholder="Поиск в избранном" aria-label="Поиск в избранном"></label>
            </div>
            <div class="tabs" role="tablist" aria-label="Группировка избранного">
              <button role="tab" aria-selected="${state.favoriteGroup === "manufacturer"}" class="tab ${state.favoriteGroup === "manufacturer" ? "active" : ""}" data-favorite-group="manufacturer">По производителям</button>
              <button role="tab" aria-selected="${state.favoriteGroup === "category"}" class="tab ${state.favoriteGroup === "category" ? "active" : ""}" data-favorite-group="category">По категориям</button>
              <button role="tab" aria-selected="${state.favoriteGroup === "all"}" class="tab ${state.favoriteGroup === "all" ? "active" : ""}" data-favorite-group="all">Без группировки</button>
            </div>
            ${favorites.length ? Object.entries(groups).map(([key, products]) => `<section class="favorite-group"><div class="group-title"><h2>${escapeHTML(groupTitle(key) || "Другие")}</h2><span>${products.length} ${plural(products.length, ["товар", "товара", "товаров"])}</span></div><div class="product-grid">${products.map(productCard).join("")}</div></section>`).join("") : emptyState("search", "В избранном такого нет", "Измените запрос или очистите строку поиска.", "Очистить поиск", "clear-favorite-search")}
          ` : emptyState("heart", "Сохраняйте то, что нравится", "Нажмите на сердечко в карточке товара — здесь появится ваша персональная коллекция.", "Перейти в каталог", "go-catalog")}
        </div>
      </div>
      ${pageFooter()}`;
  }

  function cartItemsVisible() {
    const query = state.cartQuery.trim().toLocaleLowerCase("ru");
    return state.cart.filter(item => {
      const product = byId(item.productId);
      return product && (!query || `${product.name} ${manufacturerName(product)} ${product.series}`.toLocaleLowerCase("ru").includes(query));
    });
  }

  function cartItemMarkup(item) {
    const product = byId(item.productId);
    if (!product) return "";
    const art = artVars(product);
    const minimum = Math.max(1, Number(product.minimumQuantity || 1));
    const step = Math.max(1, Number(product.quantityStep || 1));
    const availability = product.stock <= 0 ? `<span class="availability out">Нет в наличии</span>` : item.quantity > product.stock ? `<span class="availability low">Доступно только ${product.stock} шт.</span>` : product.stock < 10 ? `<span class="availability low">Осталось ${product.stock} шт.</span>` : `<span class="availability">В наличии</span>`;
    return `<div class="cart-item" data-cart-row="${escapeHTML(item.id)}">
      <label class="item-checkbox" aria-label="Выбрать ${escapeHTML(product.name)}"><input type="checkbox" data-cart-select="${escapeHTML(item.id)}" ${item.selected ? "checked" : ""} ${product.stock <= 0 && !item.selected ? "disabled" : ""}><span class="fake-check"></span></label>
      <button class="cart-thumb" style="${art.style}" data-product="${escapeHTML(product.id)}" aria-label="Открыть товар">${productArt(product)}</button>
      <div class="cart-item-info"><span class="product-brand">${escapeHTML(manufacturerName(product))} · ${escapeHTML(product.series)}</span><strong>${escapeHTML(product.name)}</strong><small>${escapeHTML(product.weight)} · ${money(product.priceMinor)} / шт.</small>${availability}</div>
      <div class="qty-control" aria-label="Количество ${escapeHTML(product.name)}">
        <button data-cart-qty="${escapeHTML(item.id)}" data-delta="-${step}" aria-label="Уменьшить" ${product.stock < minimum ? "disabled" : ""}>${icon("minus")}</button>
        <input type="number" min="${minimum}" max="${Math.max(minimum, product.stock)}" step="${step}" value="${item.quantity}" data-cart-qty-input="${escapeHTML(item.id)}" aria-label="Количество" ${product.stock < minimum ? "disabled" : ""}>
        <button data-cart-qty="${escapeHTML(item.id)}" data-delta="${step}" aria-label="Увеличить" ${product.stock < minimum ? "disabled" : ""}>${icon("plus")}</button>
      </div>
      <span class="cart-item-price">${money(product.priceMinor * item.quantity)}</span>
      <button class="remove-button" data-remove-cart="${escapeHTML(item.id)}" aria-label="Удалить ${escapeHTML(product.name)}">${icon("trash")}</button>
    </div>`;
  }

  function cartTotals() {
    const selectedItems = state.cart.filter(item => item.selected);
    const invalid = selectedItems.filter(item => !byId(item.productId) || byId(item.productId).stock <= 0 || item.quantity > byId(item.productId).stock);
    const selected = selectedItems.filter(item => !invalid.includes(item));
    const subtotal = selected.reduce((sum, item) => sum + byId(item.productId).priceMinor * item.quantity, 0);
    const oldSubtotal = selected.reduce((sum, item) => sum + (byId(item.productId).oldPriceMinor || byId(item.productId).priceMinor) * item.quantity, 0);
    const discount = Math.max(0, oldSubtotal - subtotal);
    const delivery = subtotal >= 350000 || subtotal === 0 ? 0 : 29000;
    return { selected, invalid, subtotal, discount, delivery, total: subtotal + delivery };
  }

  function renderCart() {
    const visible = cartItemsVisible();
    const groups = visible.reduce((result, item) => { const product = byId(item.productId); const key = product.manufacturerId; (result[key] ||= []).push(item); return result; }, {});
    const totals = cartTotals();
    const selectedCount = state.cart.filter(item => item.selected).length;
    const selectable = state.cart.filter(item => byId(item.productId)?.stock > 0);
    const allSelected = selectable.length > 0 && selectable.every(item => item.selected);
    return `
      <div class="page">
        <div class="shell">
          <header class="page-header"><div><h1 class="page-title">Корзина</h1><p class="page-subtitle">${state.cart.length} ${plural(state.cart.length, ["позиция", "позиции", "позиций"])} · ${state.cart.reduce((sum,item) => sum + item.quantity, 0)} единиц товара</p></div>${state.cart.length ? `<button class="button-secondary button-small" data-route="catalog">Продолжить покупки</button>` : ""}</header>
          ${state.cart.length ? `<div class="cart-layout">
            <section class="cart-main" aria-label="Товары в корзине">
              <div class="cart-toolbar">
                <div class="cart-toolbar-left">
                  <label class="select-checkbox"><input type="checkbox" id="cart-select-all" ${allSelected ? "checked" : ""} ${selectable.length ? "" : "disabled"}><span class="fake-check"></span><span>Все</span></label>
                  <button class="bulk-button" data-action="selected-to-favorites" ${selectedCount ? "" : "disabled"}>${icon("heart")} В избранное</button>
                  <button class="bulk-button" data-action="delete-selected" ${selectedCount ? "" : "disabled"}>${icon("trash")} Удалить</button>
                </div>
                <div class="cart-toolbar-right"><label class="inline-search cart-search">${icon("search")}<input id="cart-search" type="search" value="${escapeHTML(state.cartQuery)}" placeholder="Найти в корзине" aria-label="Поиск в корзине"></label></div>
              </div>
              ${visible.length ? Object.entries(groups).map(([manufacturerId, items]) => {
                const manufacturer = manufacturerById(manufacturerId);
                const collapsed = state.collapsedGroups.has(manufacturerId);
                return `<section class="cart-group ${collapsed ? "collapsed" : ""}"><button class="cart-group-head" data-collapse-group="${escapeHTML(manufacturerId)}" aria-expanded="${!collapsed}"><strong>${escapeHTML(manufacturer?.name || "Другой производитель")} <small>${items.length} ${plural(items.length,["позиция","позиции","позиций"])}</small></strong>${icon("chevron")}</button><div class="cart-items">${items.map(cartItemMarkup).join("")}</div></section>`;
              }).join("") : emptyState("search", "В корзине такого нет", "Попробуйте другой запрос по названию, бренду или серии.", "Очистить поиск", "clear-cart-search")}
            </section>
            <aside class="cart-summary" aria-label="Итог заказа">
              <h2>Ваш заказ</h2>
              <div class="summary-row"><span>${totals.selected.length} ${plural(totals.selected.length,["позиция","позиции","позиций"])}</span><strong>${money(totals.subtotal + totals.discount)}</strong></div>
              ${totals.discount ? `<div class="summary-row discount"><span>Скидка</span><strong>−${money(totals.discount)}</strong></div>` : ""}
              <div class="summary-row"><span>Доставка</span><strong>${totals.delivery ? money(totals.delivery) : "Бесплатно"}</strong></div>
              <div class="summary-total"><span>К оплате</span><strong>${money(totals.total)}</strong></div>
              ${totals.invalid.length ? `<p class="summary-caption" role="alert">Скорректируйте ${totals.invalid.length} ${plural(totals.invalid.length,["выбранную позицию","выбранные позиции","выбранных позиций"])} с изменившимся остатком.</p>` : ""}
              <button class="button-primary button-wide" data-action="checkout" ${!totals.selected.length || totals.invalid.length ? "disabled" : ""}>Перейти к оформлению ${icon("arrow")}</button>
              <div class="progress-label"><span>${totals.subtotal >= 350000 ? "Доставка бесплатная" : `Ещё ${money(350000 - totals.subtotal)} до бесплатной доставки`}</span><span>${Math.min(100, Math.round(totals.subtotal / 350000 * 100))}%</span></div>
              <div class="progress"><span style="width:${Math.min(100, totals.subtotal / 350000 * 100)}%"></span></div>
              <div class="promo-input"><input id="promo-code" placeholder="Промокод" aria-label="Промокод"><button data-action="apply-promo">Применить</button></div>
              <p class="summary-caption">На следующем шаге выберете адрес, интервал и способ оплаты</p>
            </aside>
          </div>` : emptyState("bag", "Корзина пока пуста", "Добавляйте товары из каталога — мы сгруппируем их по производителям, чтобы ничего не потерялось.", "Перейти в каталог", "go-catalog")}
        </div>
      </div>
      ${pageFooter()}`;
  }

  function accountNav(active) {
    return `<nav class="account-nav" aria-label="Разделы профиля">
      <button class="${active === "orders" ? "active" : ""}" data-route="orders">${icon("receipt")} Заказы</button>
      <button class="${active === "profile" ? "active" : ""}" data-route="profile">${icon("user")} Профиль</button>
      <button data-route="favorites">${icon("heart")} Избранное</button>
      <button data-route="delivery">${icon("location")} Доставка</button>
    </nav>`;
  }

  function orderStatus(status) {
    return ({ delivered: ["Доставлен", ""], transit: ["В пути", "transit"], processing: ["Собираем", "transit"], cancelled: ["Отменён", ""] })[status] || [status, ""];
  }

  function orderItemProduct(item) {
    return byId(item.productId) || {
      id: item.productId,
      name: item.name || "Товар из заказа",
      categoryId: "",
      manufacturerId: item.manufacturerId || "",
      manufacturerName: item.manufacturer || "Производитель",
      series: "Архивная позиция",
      weight: "",
      priceMinor: Number(item.unitPriceMinor || 0),
      oldPriceMinor: 0,
      stock: 0,
      tags: [],
      rating: 0,
      art: ["box", "#ddd8cc", "#4c433a", "С"]
    };
  }

  function orderCard(order) {
    const [label, cls] = orderStatus(order.status);
    const products = order.items.map(orderItemProduct);
    const calculated = order.items.reduce((sum, item) => sum + (item.lineTotalMinor || item.unitPriceMinor * item.quantity), 0);
    return `<article class="order-card">
      <div class="order-head">
        <div><strong class="order-id">Заказ ${escapeHTML(order.id)}</strong><span class="order-date">${new Intl.DateTimeFormat("ru-RU", { day: "numeric", month: "long", year: "numeric" }).format(new Date(order.createdAt))}</span></div>
        <span class="order-status ${cls}">${escapeHTML(label)}</span>
        <span class="order-total">${money(order.totalMinor || calculated)}</span>
      </div>
      <div class="order-body">
        <div class="order-thumbs" aria-label="${order.items.length} товаров">${products.slice(0, 5).map(product => { const art = artVars(product); return `<span class="order-mini" style="${art.style}">${productArt(product)}</span>`; }).join("")}${products.length > 5 ? `<span class="order-mini more-mini">+${products.length - 5}</span>` : ""}</div>
        <div class="order-actions"><button class="button-secondary button-small" data-order-details="${escapeHTML(order.id)}">Подробнее</button><button class="button-dark button-small" data-repeat-order="${escapeHTML(order.id)}">${icon("copy")} Повторить</button></div>
      </div>
    </article>`;
  }

  function renderOrders() {
    if (!state.user) return renderLogin();
    return `<div class="page"><div class="shell">
      <header class="page-header"><div><h1 class="page-title">Мои заказы</h1><p class="page-subtitle">История покупок и быстрый повтор с проверкой актуального наличия</p></div></header>
      <div class="orders-layout">${accountNav("orders")}<section aria-label="История заказов">${state.orders.length ? state.orders.map(orderCard).join("") : emptyState("receipt", "Заказов пока нет", "После первой покупки здесь появится история и быстрый повтор заказа.", "Перейти в каталог", "go-catalog")}</section></div>
    </div></div>${pageFooter()}`;
  }

  function renderProfile() {
    if (!state.user) return renderLogin();
    const initials = String(state.user.name || "Пользователь").split(" ").map(part => part[0]).slice(0, 2).join("");
    return `<div class="page"><div class="shell">
      <header class="page-header"><div><h1 class="page-title">Профиль</h1><p class="page-subtitle">Личные данные, адреса и настройки уведомлений</p></div><button class="button-secondary button-small" data-action="logout">Выйти</button></header>
      <div class="orders-layout">${accountNav("profile")}<div>
        <div class="profile-grid">
          <section class="profile-card"><h2>Личные данные</h2><div class="profile-person"><span class="avatar">${escapeHTML(initials)}</span><div><strong>${escapeHTML(state.user.name || "Пользователь")}</strong><span>${escapeHTML(state.user.email || "")}</span></div></div><div style="margin-top:20px"><div class="profile-row"><span>Телефон</span><strong>${escapeHTML(state.user.phone || "Не указан")}</strong></div><div class="profile-row"><span>Дата рождения</span><strong>18 апреля</strong></div></div><button class="button-secondary button-small" style="margin-top:17px" data-action="edit-profile">Изменить</button></section>
          <section class="profile-card"><h2>Бонусы</h2><div class="profile-person"><span class="avatar">${escapeHTML(String(state.user.bonus || 1260))}</span><div><strong>${new Intl.NumberFormat("ru-RU").format(state.user.bonus || 1260)} баллов</strong><span>Можно оплатить до 20% заказа</span></div></div><div class="progress" style="background:#e6e4dd;margin-top:25px"><span style="width:68%;background:var(--surface-dark)"></span></div><p class="muted">До следующего уровня — 2 740 ₽</p></section>
          <section class="profile-card"><h2>Основной адрес</h2><div class="profile-row"><span>Город</span><strong>Москва</strong></div><div class="profile-row"><span>Адрес</span><strong>ул. Хлебная, 12, кв. 48</strong></div><div class="profile-row"><span>Комментарий</span><strong>Домофон 48</strong></div><button class="button-secondary button-small" style="margin-top:17px" data-action="edit-address">Изменить</button></section>
          <section class="profile-card"><h2>Уведомления</h2><label class="check-row"><input type="checkbox" checked><span class="fake-check"></span><span>Статусы заказов</span></label><label class="check-row" style="margin-top:13px"><input type="checkbox" checked><span class="fake-check"></span><span>Персональные подборки</span></label><label class="check-row" style="margin-top:13px"><input type="checkbox"><span class="fake-check"></span><span>Скидки производителей</span></label></section>
        </div>
      </div></div>
    </div></div>${pageFooter()}`;
  }

  function renderLogin() {
    return `<div class="login-layout"><form class="login-card" id="login-form"><h1>Вход в профиль</h1><p>Заказы, избранное и адреса будут доступны на любом устройстве.</p><div class="field"><label for="login-email">Электронная почта</label><input id="login-email" name="email" type="email" autocomplete="email" value="demo@sugar.ru" required></div><div class="field"><label for="login-password">Пароль</label><input id="login-password" name="password" type="password" autocomplete="current-password" value="Sweet2026!" required></div><p class="login-error" id="login-error" hidden></p><button class="button-dark button-wide" type="submit">Войти</button><div class="demo-credentials">Тестовый аккаунт: <strong>demo@sugar.ru</strong><br>Пароль: <strong>Sweet2026!</strong></div></form></div>`;
  }

  function renderDelivery() {
    return `<div class="page"><div class="shell"><header class="page-header"><div><h1 class="page-title">Доставка и оплата</h1><p class="page-subtitle">Точные интервалы, бережная упаковка и понятная стоимость до оформления.</p></div></header>
      <div class="profile-grid">
        <section class="profile-card"><h2>По Москве</h2><div class="profile-row"><span>Сегодня, 19:00–22:00</span><strong>290 ₽</strong></div><div class="profile-row"><span>Завтра, интервал 2 часа</span><strong>190 ₽</strong></div><div class="profile-row"><span>Заказ от 3 500 ₽</span><strong>Бесплатно</strong></div></section>
        <section class="profile-card"><h2>Оплата</h2><div class="profile-row"><span>Банковская карта</span><strong>Онлайн</strong></div><div class="profile-row"><span>СБП</span><strong>Онлайн</strong></div><div class="profile-row"><span>Бонусные баллы</span><strong>До 20%</strong></div></section>
        <section class="profile-card"><h2>Как упаковываем</h2><p class="muted">Шоколад и конфеты перевозим в термобоксах. Хрупкие упаковки фиксируем бумажным наполнителем, подарочные наборы не заклеиваем стикерами.</p></section>
        <section class="profile-card"><h2>Нужна помощь?</h2><p class="muted">Операторы отвечают ежедневно с 09:00 до 21:00.</p><a class="button-secondary button-small" href="tel:+74951234567">+7 495 123-45-67</a></section>
      </div>
    </div></div>${pageFooter()}`;
  }

  function renderNotFound() {
    return `<div class="page"><div class="shell">${emptyState("search", "Страница не найдена", "Кажется, этот адрес устарел. Вернитесь в каталог — там точно есть что выбрать.", "На главную", "go-home")}</div></div>`;
  }

  function render() {
    const main = qs("#app-main");
    const renderers = { home: renderHome, catalog: renderCatalog, favorites: renderFavorites, cart: renderCart, orders: renderOrders, profile: renderProfile, login: renderLogin, delivery: renderDelivery };
    main.innerHTML = state.loading ? `<div class="page"><div class="shell"><div class="skeleton" style="height:560px;border-radius:30px"></div></div></div>` : (renderers[state.route] || renderNotFound)();
    const titles = { home: "Слой — кондитерские изделия", catalog: "Каталог — Слой", favorites: "Избранное — Слой", cart: "Корзина — Слой", orders: "Заказы — Слой", profile: "Профиль — Слой", delivery: "Доставка и оплата — Слой" };
    document.title = titles[state.route] || "Слой";
    qsa(".mobile-nav [data-route]").forEach(button => button.classList.toggle("active", button.dataset.route === state.route));
    updateBadges();
  }

  function updateBadges() {
    qsa("[data-fav-count]").forEach(badge => { badge.textContent = state.favorites.length > 99 ? "99+" : state.favorites.length; badge.hidden = state.favorites.length === 0; });
    const count = state.cart.reduce((sum, item) => sum + item.quantity, 0);
    qsa("[data-cart-count]").forEach(badge => { badge.textContent = count > 99 ? "99+" : count; badge.hidden = count === 0; });
  }

  function renderMegaMenu() {
    const categories = qs("#mega-categories");
    const content = qs("#mega-content");
    if (!categories || !content) return;
    categories.innerHTML = state.categories.map(category => `<button class="mega-category ${category.id === state.megaCategory ? "active" : ""}" role="tab" aria-selected="${category.id === state.megaCategory}" data-mega-category="${escapeHTML(category.id)}"><span>${escapeHTML(category.name)}</span><span class="count">${Number(category.productCount ?? productsForCategory(category.id).length)}</span></button>`).join("");
    const relevantProducts = productsForCategory(state.megaCategory);
    const manufacturers = [...new Set(relevantProducts.map(product => product.manufacturerId))].map(manufacturerById).filter(Boolean);
    const category = categoryById(state.megaCategory);
    content.innerHTML = `<h3>${escapeHTML(category?.name || "Каталог")}</h3><div class="mega-manufacturers">${manufacturers.map(manufacturer => {
      const products = relevantProducts.filter(product => product.manufacturerId === manufacturer.id);
      const brands = [...new Set(products.map(product => product.brand?.name).filter(Boolean))];
      const series = [...new Set(products.map(product => product.series).filter(Boolean))];
      return `<div class="mega-manufacturer"><strong>${escapeHTML(manufacturer.name)}</strong>${brands.length ? `<span class="mega-label">Бренды</span>${brands.slice(0,2).map(brand => `<button data-series="${escapeHTML(brand)}" data-series-category="${escapeHTML(state.megaCategory)}">${escapeHTML(brand)}</button>`).join("")}` : ""}${series.length ? `<span class="mega-label">Серии</span>${series.slice(0,3).map(seriesName => `<button data-series="${escapeHTML(seriesName)}" data-series-category="${escapeHTML(state.megaCategory)}">${escapeHTML(seriesName)}</button>`).join("")}` : ""}</div>`;
    }).join("")}</div><button class="mega-all" data-category="${escapeHTML(state.megaCategory)}">Все товары категории <span>→</span></button>`;
  }

  let lastFocused = null;

  function showScrim() {
    qs("#scrim").hidden = false;
    document.body.classList.add("no-scroll");
  }

  function closeOverlays({ restoreFocus = true } = {}) {
    const mega = qs("#catalog-mega");
    const trigger = qs(".catalog-trigger");
    if (mega) mega.hidden = true;
    if (trigger) trigger.setAttribute("aria-expanded", "false");
    qs("#drawer-root").innerHTML = "";
    qs("#modal-root").innerHTML = "";
    qs("#scrim").hidden = true;
    document.body.classList.remove("no-scroll");
    if (restoreFocus && lastFocused?.isConnected) lastFocused.focus({ preventScroll: true });
    lastFocused = null;
  }

  function toggleCatalog() {
    const mega = qs("#catalog-mega");
    const opening = mega.hidden;
    closeOverlays({ restoreFocus: false });
    if (opening) {
      lastFocused = document.activeElement;
      mega.hidden = false;
      qs(".catalog-trigger")?.setAttribute("aria-expanded", "true");
      renderMegaMenu();
      showScrim();
    }
  }

  function openModal(content, extraClass = "") {
    lastFocused = document.activeElement;
    qs("#modal-root").innerHTML = `<div class="modal-backdrop" role="presentation"><section class="modal ${extraClass}" role="dialog" aria-modal="true">${content}</section></div>`;
    const dialog = qs("#modal-root .modal");
    const heading = qs("h1, h2", dialog);
    if (heading) {
      heading.id ||= `dialog-title-${Date.now()}`;
      dialog.setAttribute("aria-labelledby", heading.id);
    } else dialog.setAttribute("aria-label", "Диалог");
    showScrim();
    requestAnimationFrame(() => qs("#modal-root button, #modal-root input, #modal-root select")?.focus({ preventScroll: true }));
  }

  function openDrawer(title, content, footer = "") {
    lastFocused = document.activeElement;
    qs("#drawer-root").innerHTML = `<aside class="drawer" role="dialog" aria-modal="true" aria-label="${escapeHTML(title)}"><div class="drawer-head"><h2>${escapeHTML(title)}</h2><button class="close-button" data-action="close-overlay" aria-label="Закрыть">${icon("close")}</button></div><div class="drawer-body">${content}</div>${footer ? `<div class="drawer-foot">${footer}</div>` : ""}</aside>`;
    showScrim();
    requestAnimationFrame(() => qs("#drawer-root button, #drawer-root input")?.focus({ preventScroll: true }));
  }

  function showToast(message, actionLabel = "", action = "") {
    const id = `toast-${Date.now()}`;
    qs("#toast-region").insertAdjacentHTML("beforeend", `<div class="toast" id="${id}">${icon("check")}<span>${escapeHTML(message)}</span>${actionLabel ? `<button data-toast-action="${escapeHTML(action)}">${escapeHTML(actionLabel)}</button>` : ""}</div>`);
    setTimeout(() => qs(`#${id}`)?.remove(), 4200);
  }

  function openProduct(productId) {
    const product = byId(productId);
    if (!product) return;
    const art = artVars(product);
    const favorite = state.favorites.some(id => String(id) === String(product.id));
    const cartItem = state.cart.find(item => String(item.productId) === String(product.id));
    openModal(`<div class="modal-head"><div><span class="product-brand">${escapeHTML(manufacturerName(product))} · ${escapeHTML(product.series)}</span><h2>Карточка товара</h2></div><button class="close-button" data-action="close-overlay" aria-label="Закрыть">${icon("close")}</button></div><div class="modal-body"><div class="product-detail"><div class="product-detail-art" style="${art.style}">${productArt(product)}${(product.tags || []).filter(tag => ["new", "sale", "hit"].includes(tag)).map(tag => `<span class="product-badge ${tag}" style="position:relative;z-index:2;display:inline-block;margin:14px 0 0 14px">${escapeHTML(tagLabel(tag))}</span>`).join("")}</div><div class="product-detail-info"><span class="eyebrow">${escapeHTML(categoryName(product))}</span><h2>${escapeHTML(product.name)}</h2><p>${escapeHTML(product.description || "Выразительный вкус, аккуратная текстура и понятный состав. Свежая партия напрямую от производителя.")}</p><div class="detail-facts"><div class="detail-fact"><strong>${escapeHTML(product.weight)}</strong><span>Вес упаковки</span></div><div class="detail-fact"><strong>★ ${product.rating.toFixed(1)}</strong><span>Оценка покупателей</span></div><div class="detail-fact"><strong>${product.stock > 0 ? `${product.stock} шт.` : "Нет"}</strong><span>В наличии</span></div></div><div class="profile-row"><span>Производитель</span><button class="text-link" data-manufacturer="${escapeHTML(product.manufacturerId)}">${escapeHTML(manufacturerName(product))} →</button></div>${product.brand?.name ? `<div class="profile-row"><span>Бренд</span><strong>${escapeHTML(product.brand.name)}</strong></div>` : ""}<div class="profile-row"><span>Серия</span><strong>${escapeHTML(product.series)}</strong></div><div class="detail-buy"><div class="price">${money(product.priceMinor)}${product.oldPriceMinor ? `<span class="old-price">${money(product.oldPriceMinor)}</span>` : ""}</div><div style="display:flex;gap:8px"><button class="favorite-button ${favorite ? "active" : ""}" style="position:static;border:1px solid var(--line)" data-favorite="${escapeHTML(product.id)}" aria-label="${favorite ? "Убрать из избранного" : "Добавить в избранное"}" aria-pressed="${favorite}">${icon(favorite ? "heart-fill" : "heart")}</button><button class="button-dark" data-add-cart="${escapeHTML(product.id)}" ${product.stock <= 0 ? "disabled" : ""}>${cartItem ? `${icon("check")} В корзине · ${cartItem.quantity}` : `${icon("bag")} В корзину`}</button></div></div></div></div></div>`, "product-modal");
  }

  function openFilters() {
    const count = state.apiOnline ? state.catalogRemote.total : filteredProducts().length;
    const label = state.apiOnline && state.catalogRemote.loading ? "Обновляем…" : `Показать ${count}`;
    openDrawer("Фильтры", filterOptionsMarkup(), `<button class="button-secondary" data-action="reset-filters">Сбросить</button><button class="button-dark" data-action="apply-filters">${label}</button>`);
  }

  function refreshFilterDrawerButton() {
    const button = qs('#drawer-root [data-action="apply-filters"]');
    if (!button) return;
    button.textContent = state.apiOnline && state.catalogRemote.loading ? "Обновляем…" : `Показать ${state.apiOnline ? state.catalogRemote.total : filteredProducts().length}`;
  }

  async function openRepeatOrder(orderId) {
    const order = state.orders.find(item => String(item.id) === String(orderId));
    if (!order) return;
    if (!state.token) {
      navigate("profile");
      showToast("Войдите, чтобы повторить заказ");
      return;
    }
    const apiOrderId = order.apiId || orderId;
    let previewItems;
    try {
      const response = await API.post(`/orders/${encodeURIComponent(apiOrderId)}/repeat/preview`, {});
      previewItems = unpack(response, ["items", "preview"]);
    } catch (error) {
      showToast("Не удалось проверить актуальные остатки. Попробуйте ещё раз");
      return;
    }
    state.repeatDraft = {
      orderId,
      apiOrderId,
      items: previewItems.map(raw => {
        const productId = raw.productId ?? raw.product_id ?? raw.product?.id;
        const product = byId(productId) || normalizeProduct(raw.product || { id: productId, name: raw.name, priceMinor: raw.priceMinor });
        const requested = Number(raw.requestedQuantity ?? raw.requested_quantity ?? raw.quantity ?? 1);
        const available = Number(raw.addableQuantity ?? raw.addable_quantity ?? raw.availableQuantity ?? raw.available_quantity ?? raw.stock ?? product.stock ?? 0);
        const proposed = Number(raw.proposedQuantity ?? raw.proposed_quantity ?? requested);
        return { productId, product, requested, quantity: proposed, available, alreadyInCart: Number(raw.alreadyInCartQuantity ?? raw.already_in_cart_quantity ?? 0), selected: raw.selected ?? available > 0, status: raw.status ?? (available <= 0 ? "unavailable" : available < requested ? "limited" : "available"), message: raw.message ?? "", replacement: raw.replacement ?? null, priceMinor: Number(raw.currentUnitPriceMinor ?? raw.current_unit_price_minor ?? raw.currentPriceMinor ?? raw.current_price_minor ?? raw.priceMinor ?? product.priceMinor), changedPrice: Boolean(raw.priceChanged ?? raw.price_changed) };
      }).sort((a, b) => Number(b.available < b.requested) - Number(a.available < a.requested))
    };
    renderRepeatModal();
  }

  function renderRepeatModal() {
    const draft = state.repeatDraft;
    if (!draft) return;
    const selected = draft.items.filter(item => item.selected);
    const invalid = selected.filter(item => item.quantity > item.available || item.available <= 0);
    const problemCount = draft.items.filter(item => item.available < item.requested || item.status !== "available" || item.changedPrice).length;
    const total = selected.reduce((sum, item) => sum + item.priceMinor * Math.min(item.quantity, item.available), 0);
    const content = `<div class="modal-head"><div><h2>Повторить заказ ${escapeHTML(draft.orderId)}</h2><p>Проверьте наличие и количество перед добавлением в корзину</p></div><button class="close-button" data-action="close-overlay" aria-label="Закрыть">${icon("close")}</button></div>
      <div class="modal-body">
        ${problemCount ? `<div class="repeat-notice">${icon("info")}<div><strong>${problemCount} ${plural(problemCount,["позиция требует","позиции требуют","позиций требуют"])} внимания.</strong> Проблемные товары показаны первыми. Их можно исключить или изменить количество.${invalid.length ? ` <button class="text-link" data-action="auto-fix-repeat">Исправить автоматически →</button>` : ""}</div></div>` : `<div class="demo-banner">${icon("check")}<div><strong>Все товары доступны</strong><p>Цены и остатки проверены прямо сейчас.</p></div></div>`}
        <div class="repeat-list">${draft.items.map(item => {
          const product = item.product;
          const art = artVars(product);
          const unavailable = item.available <= 0 || ["unavailable", "discontinued"].includes(item.status);
          const shortage = !unavailable && (item.quantity > item.available || item.status === "limited");
          return `<div class="repeat-item ${unavailable ? "unavailable" : shortage ? "problem" : ""}">
            <label class="item-checkbox" aria-label="Включить ${escapeHTML(product.name)}"><input type="checkbox" data-repeat-select="${escapeHTML(item.productId)}" ${item.selected ? "checked" : ""} ${unavailable ? "disabled" : ""}><span class="fake-check"></span></label>
            <div class="repeat-thumb" style="${art.style}">${productArt(product)}</div>
            <div class="repeat-info"><strong>${escapeHTML(product.name)}</strong><span>${escapeHTML(manufacturerName(product))} · было ${item.requested} шт.${item.alreadyInCart ? ` · уже в корзине ${item.alreadyInCart}` : ""}</span><em class="repeat-status ${unavailable ? "danger" : shortage ? "warning" : ""}">${escapeHTML(item.message || (unavailable ? item.status === "discontinued" ? "Снят с продажи" : "Нет в наличии" : shortage ? `Можно добавить ${item.available} шт.` : item.changedPrice ? "Цена изменилась" : "В наличии"))}</em></div>
            <div class="qty-control"><button data-repeat-qty="${escapeHTML(item.productId)}" data-delta="-1" ${unavailable ? "disabled" : ""} aria-label="Уменьшить количество ${escapeHTML(product.name)}">${icon("minus")}</button><input type="number" min="1" value="${item.quantity}" data-repeat-qty-input="${escapeHTML(item.productId)}" ${unavailable ? "disabled" : ""} aria-label="Количество ${escapeHTML(product.name)}"><button data-repeat-qty="${escapeHTML(item.productId)}" data-delta="1" ${unavailable ? "disabled" : ""} aria-label="Увеличить количество ${escapeHTML(product.name)}">${icon("plus")}</button></div>
            <span class="repeat-price">${money(item.priceMinor * Math.min(item.quantity, item.available))}</span>
          </div>`;
        }).join("")}</div>
      </div>
      <div class="modal-foot"><div class="modal-total"><strong>${money(total)}</strong><span>${selected.length} ${plural(selected.length,["позиция выбрана","позиции выбраны","позиций выбрано"])}</span></div><div class="modal-actions"><button class="button-secondary" data-action="close-overlay">Отмена</button><button class="button-dark" data-action="commit-repeat" ${!selected.length || invalid.length ? "disabled" : ""}>Добавить в корзину</button></div></div>`;
    if (qs("#modal-root .modal")) qs("#modal-root .modal").innerHTML = content;
    else openModal(content);
  }

  async function commitRepeat() {
    const draft = state.repeatDraft;
    if (!draft) return;
    const selected = draft.items.filter(item => item.selected && item.available > 0 && item.quantity <= item.available);
    if (!selected.length || selected.length !== draft.items.filter(item => item.selected).length) return;
    const button = qs('[data-action="commit-repeat"]');
    if (button) { button.disabled = true; button.textContent = "Проверяем…"; }
    try {
      const result = await API.post(`/orders/${encodeURIComponent(draft.apiOrderId || draft.orderId)}/repeat/commit`, { items: draft.items.map(item => ({ productId: item.productId, quantity: item.quantity, selected: item.selected })) });
      applyCartResponse(result?.cart || result);
    } catch (error) {
      showToast(error?.status === 422 ? "Проверьте количество: остатки могли измениться" : "Не удалось добавить товары. Попробуйте ещё раз");
      renderRepeatModal();
      return;
    }
    state.repeatDraft = null;
    closeOverlays({ restoreFocus: false });
    showToast(`${selected.length} ${plural(selected.length,["позиция добавлена","позиции добавлены","позиций добавлено"])} в корзину`);
    navigate("cart");
  }

  function openOrderDetails(orderId) {
    const order = state.orders.find(item => String(item.id) === String(orderId));
    if (!order) return;
    openModal(`<div class="modal-head"><div><h2>Заказ ${escapeHTML(order.id)}</h2><p>${new Intl.DateTimeFormat("ru-RU", { dateStyle: "long", timeStyle: "short" }).format(new Date(order.createdAt))}</p></div><button class="close-button" data-action="close-overlay" aria-label="Закрыть">${icon("close")}</button></div><div class="modal-body"><div class="repeat-list">${order.items.map(item => { const product = orderItemProduct(item); const art = artVars(product); const lineTotal = item.lineTotalMinor || item.unitPriceMinor * item.quantity || product.priceMinor * item.quantity; return `<div class="repeat-item"><span></span><div class="repeat-thumb" style="${art.style}">${productArt(product)}</div><div class="repeat-info"><strong>${escapeHTML(item.name || product.name)}</strong><span>${escapeHTML(item.manufacturer || manufacturerName(product))} · ${item.quantity} шт.</span></div><span></span><span class="repeat-price">${money(lineTotal)}</span></div>`; }).join("")}</div></div><div class="modal-foot"><span class="modal-total"><strong>${money(order.totalMinor)}</strong><span>Сумма заказа</span></span><div class="modal-actions"><button class="button-secondary" data-action="close-overlay">Закрыть</button><button class="button-dark" data-repeat-order="${escapeHTML(order.id)}">Повторить заказ</button></div></div>`);
  }

  function openCheckout() {
    const totals = cartTotals();
    openModal(`<form id="checkout-form"><div class="modal-head"><div><h2>Оформление заказа</h2><p>${totals.selected.length} ${plural(totals.selected.length,["позиция","позиции","позиций"])} · доставка сегодня</p></div><button type="button" class="close-button" data-action="close-overlay">${icon("close")}</button></div><div class="modal-body"><div class="profile-grid"><div><div class="field"><label for="checkout-name">Получатель</label><input id="checkout-name" name="name" required value="${escapeHTML(state.user?.name || "")}"></div><div class="field"><label for="checkout-phone">Телефон</label><input id="checkout-phone" name="phone" type="tel" required value="${escapeHTML(state.user?.phone || "")}"></div><div class="field"><label for="checkout-address">Адрес доставки</label><input id="checkout-address" name="address" required value="ул. Хлебная, 12, кв. 48"></div></div><div><div class="field"><label for="checkout-slot">Интервал</label><select id="checkout-slot" name="slot" class="sort-select" style="width:100%"><option>Сегодня, 19:00–22:00</option><option>Завтра, 10:00–12:00</option><option>Завтра, 12:00–14:00</option></select></div><div class="field"><label>Оплата</label><label class="check-row"><input type="radio" name="payment" checked value="card"><span class="fake-check"></span><span>Картой онлайн</span></label><label class="check-row"><input type="radio" name="payment" value="sbp"><span class="fake-check"></span><span>Через СБП</span></label></div></div></div></div><div class="modal-foot"><div class="modal-total"><strong>${money(totals.total)}</strong><span>К оплате</span></div><div class="modal-actions"><button type="button" class="button-secondary" data-action="close-overlay">Назад</button><button type="submit" class="button-primary">Оформить заказ</button></div></div></form>`);
  }

  async function toggleFavorite(productId) {
    const index = state.favorites.findIndex(id => String(id) === String(productId));
    const adding = index < 0;
    const previous = [...state.favorites];
    if (adding) state.favorites.push(productId); else state.favorites.splice(index, 1);
    persist();
    if (state.token) {
      try {
        const response = adding ? await API.put(`/favorites/${encodeURIComponent(productId)}`, {}) : await API.delete(`/favorites/${encodeURIComponent(productId)}`);
        applyFavoritesResponse(response);
      } catch (error) {
        state.favorites = previous;
        persist();
        if (["favorites", "home", "catalog"].includes(state.route)) render();
        showToast("Не удалось изменить избранное. Попробуйте ещё раз");
        return;
      }
    }
    showToast(adding ? "Добавлено в избранное" : "Удалено из избранного");
    if (["favorites", "home", "catalog"].includes(state.route)) render();
    qsa(`[data-favorite="${CSS.escape(String(productId))}"]`).forEach(button => { button.classList.toggle("active", adding); button.innerHTML = icon(adding ? "heart-fill" : "heart"); button.setAttribute("aria-pressed", String(adding)); });
  }

  async function addToCart(productId, quantity = 1) {
    const product = byId(productId);
    if (!product || product.stock <= 0) return;
    let item = state.cart.find(entry => String(entry.productId) === String(productId));
    const minimum = Math.max(1, Number(product.minimumQuantity || 1));
    const step = Math.max(1, Number(product.quantityStep || 1));
    const nextQuantity = Math.min(product.stock, item ? item.quantity + Math.max(step, Number(quantity) || step) : Math.max(minimum, Number(quantity) || minimum));
    if (item && nextQuantity <= item.quantity) {
      showToast("В корзине уже весь доступный остаток");
      return;
    }
    if (state.token) {
      try {
        const response = await API.post("/cart/items", { productId, quantity: nextQuantity, selected: item?.selected !== false });
        applyCartResponse(response);
        item = state.cart.find(entry => String(entry.productId) === String(productId));
      } catch (error) {
        showToast(error?.status === 409 ? "Доступный остаток изменился" : "Не удалось обновить корзину");
        return;
      }
    } else {
      if (item) item.quantity = nextQuantity;
      else { item = { id: `local-${Date.now()}`, productId, quantity: nextQuantity, selected: true }; state.cart.push(item); }
      persist();
    }
    showToast(`${product.name} — в корзине`, "Открыть", "open-cart");
    if (["home", "catalog", "favorites"].includes(state.route)) render();
    else if (state.route === "cart") render();
    qsa(`[data-add-cart="${CSS.escape(String(productId))}"]`).forEach(button => { if (button.closest("#modal-root")) button.innerHTML = `${icon("check")} В корзине · ${item.quantity}`; });
  }

  async function changeCartQuantity(itemId, nextQuantity) {
    const item = state.cart.find(entry => String(entry.id) === String(itemId));
    if (!item) return;
    const product = byId(item.productId);
    const minimum = Math.max(1, Number(product?.minimumQuantity || 1));
    if (!product || product.stock < minimum) {
      showToast("Количество недоступного товара изменить нельзя");
      return;
    }
    const step = Math.max(1, Number(product.quantityStep || 1));
    const requested = Math.max(minimum, Number(nextQuantity) || minimum);
    const aligned = minimum + Math.round((requested - minimum) / step) * step;
    const maximum = minimum + Math.floor(Math.max(0, product.stock - minimum) / step) * step;
    const quantity = Math.min(Math.max(minimum, aligned), maximum);
    if (state.token) {
      try {
        const response = await API.patch(`/cart/items/${encodeURIComponent(item.productId)}`, { quantity });
        applyCartResponse(response);
      } catch (error) {
        showToast(error?.status === 409 ? "Доступный остаток изменился" : "Не удалось изменить количество");
        render();
        return;
      }
    } else {
      item.quantity = quantity;
      persist();
    }
    render();
  }

  let removedCartItem = null;
  async function removeCartItem(itemId) {
    const index = state.cart.findIndex(entry => String(entry.id) === String(itemId));
    if (index < 0) return;
    const snapshot = { ...state.cart[index] };
    if (state.token) {
      try {
        const response = await API.delete(`/cart/items/${encodeURIComponent(snapshot.productId)}`);
        applyCartResponse(response);
      } catch (error) {
        showToast("Не удалось удалить позицию");
        return;
      }
    } else {
      state.cart.splice(index, 1);
      persist();
    }
    removedCartItem = { item: snapshot, index };
    render();
    showToast("Позиция удалена из корзины", "Вернуть", "undo-remove");
  }

  async function undoRemoveCartItem() {
    if (!removedCartItem) return;
    const removed = removedCartItem;
    removedCartItem = null;
    if (state.token) {
      try {
        const response = await API.post("/cart/items", { productId: removed.item.productId, quantity: removed.item.quantity, selected: removed.item.selected });
        applyCartResponse(response);
      } catch (error) {
        showToast("Не удалось вернуть позицию: остаток мог измениться");
        return;
      }
    } else {
      state.cart.splice(removed.index, 0, removed.item);
      persist();
    }
    render();
    showToast("Позиция возвращена в корзину");
  }

  async function addFavoritesToCart() {
    const products = state.favorites.map(byId).filter(product => {
      const minimum = Math.max(1, Number(product?.minimumQuantity || 1));
      return product && product.stock >= minimum && !state.cart.some(item => String(item.productId) === String(product.id));
    });
    if (!products.length) {
      showToast("Все доступные товары уже в корзине");
      return;
    }
    let added = products.length;
    if (state.token) {
      const results = await Promise.allSettled(products.map(product => API.post("/cart/items", {
        productId: product.id,
        quantity: Math.max(1, Number(product.minimumQuantity || 1)),
        selected: true
      })));
      added = results.filter(result => result.status === "fulfilled").length;
      try { applyCartResponse(await API.get("/cart")); }
      catch { showToast("Не удалось обновить корзину"); return; }
    } else {
      products.forEach(product => state.cart.push({ id: `fav-${Date.now()}-${product.id}`, productId: product.id, quantity: Math.max(1, Number(product.minimumQuantity || 1)), selected: true }));
      persist();
    }
    render();
    showToast(added === products.length ? `${added} ${plural(added,["товар добавлен","товара добавлены","товаров добавлено"])} в корзину` : `Добавлено ${added} из ${products.length}: часть остатков изменилась`);
  }

  async function saveSelectedToFavorites() {
    const selectedProducts = [...new Set(state.cart.filter(item => item.selected).map(item => item.productId))];
    const additions = selectedProducts.filter(id => !state.favorites.some(favoriteId => String(favoriteId) === String(id)));
	let saved = additions.length;
    if (state.token) {
	  const results = await Promise.allSettled(additions.map(id => API.put(`/favorites/${encodeURIComponent(id)}`, {})));
	  saved = results.filter(result => result.status === "fulfilled").length;
      try { applyFavoritesResponse(await API.get("/favorites")); }
      catch { showToast("Не удалось обновить избранное"); return; }
    } else {
      state.favorites.push(...additions);
      persist();
    }
    render();
	showToast(!additions.length ? "Все выбранные товары уже в избранном" : saved === additions.length ? "Выбранные товары сохранены в избранном" : `Сохранено ${saved} из ${additions.length}: часть товаров недоступна`);
  }

  async function deleteSelectedCartItems() {
    const count = state.cart.filter(item => item.selected).length;
    if (!count) return;
    if (state.token) {
      try { applyCartResponse(await API.delete("/cart?selectedOnly=true")); }
      catch { showToast("Не удалось удалить выбранные позиции"); return; }
    } else {
      state.cart = state.cart.filter(item => !item.selected);
      persist();
    }
    render();
    showToast(`${count} ${plural(count,["позиция удалена","позиции удалены","позиций удалено"])}`);
  }

  async function setAllCartSelection(productIds, selected) {
    if (state.token) {
      try { applyCartResponse(await API.patch("/cart/selection", { productIds, selected })); }
      catch { showToast("Не удалось изменить выбор товаров"); render(); return; }
    } else {
      const ids = new Set(productIds.map(String));
      state.cart.forEach(item => { if (!ids.size || ids.has(String(item.productId))) item.selected = selected; });
      persist();
    }
    render();
  }

  async function setCartItemSelection(item, selected) {
    if (state.token) {
      try { applyCartResponse(await API.patch(`/cart/items/${encodeURIComponent(item.productId)}`, { selected })); }
      catch { showToast("Не удалось изменить выбор товара"); render(); return; }
    } else {
      item.selected = selected;
      persist();
    }
    render();
  }

  function resetFilters() {
    Object.assign(state.catalog, { search: "", category: "", manufacturer: "", collection: "", inStock: false, sale: false, newOnly: false, minPrice: "", maxPrice: "", page: 1 });
    closeOverlays({ restoreFocus: false });
    commitCatalogState();
  }

  async function fetchSuggestions(query) {
    if (state.apiOnline && query.length >= 2) {
      try {
        const result = await API.get(`/search/suggest?q=${encodeURIComponent(query)}`);
        const suggestions = unpack(result, ["suggestions", "products"]);
        if (suggestions.length) return suggestions.map(item => ({ type: item.type || (item.priceMinor || item.price_minor ? "product" : "query"), id: item.id ?? item.productId, title: item.title ?? item.name ?? item.query, subtitle: item.subtitle ?? item.manufacturerName ?? item.categoryName ?? "", priceMinor: item.priceMinor ?? item.price_minor, query: item.query ?? item.title ?? item.name }));
      } catch (error) { console.info("Подсказки сформированы локально", error); }
    }
    const normalized = query.toLocaleLowerCase("ru");
    const products = discoveryProducts().filter(product => `${product.name} ${manufacturerName(product)} ${product.series}`.toLocaleLowerCase("ru").includes(normalized)).slice(0, 5).map(product => ({ type: "product", id: product.id, title: product.name, subtitle: `${manufacturerName(product)} · ${product.weight}`, priceMinor: product.priceMinor }));
    const categories = state.categories.filter(item => item.name.toLocaleLowerCase("ru").includes(normalized)).slice(0, 2).map(item => ({ type: "category", id: item.id, title: item.name, subtitle: "Категория" }));
    const manufacturers = state.manufacturers.filter(item => item.name.toLocaleLowerCase("ru").includes(normalized)).slice(0, 2).map(item => ({ type: "manufacturer", id: item.id, title: item.name, subtitle: `${item.country} · производитель` }));
    return [...products, ...categories, ...manufacturers];
  }

  const updateSearchSuggestions = debounce(async query => {
    const input = qs("#global-search-input");
    const panel = qs("#search-panel");
    if (!input || !panel || input.value !== query) return;
    const trimmed = query.trim();
    if (!trimmed) {
      panel.innerHTML = `<div class="search-panel-header"><span>Быстрый выбор</span><span>По товару, бренду и серии</span></div>${state.categories.slice(0,4).map(category => `<button class="suggestion" role="option" data-suggestion-type="category" data-suggestion-id="${escapeHTML(category.id)}"><span class="suggestion-icon">${escapeHTML(category.glyph || "◇")}</span><span class="suggestion-main"><strong>${escapeHTML(category.name)}</strong><span>${Number(category.productCount ?? productsForCategory(category.id).length)} товаров</span></span><span>→</span></button>`).join("")}`;
      panel.hidden = false;
      input.setAttribute("aria-expanded", "true");
      return;
    }
    const suggestions = await fetchSuggestions(trimmed);
    state.searchIndex = -1;
    panel.innerHTML = suggestions.length ? `<div class="search-panel-header"><span>Нашли сразу</span><span>${suggestions.length} подсказок</span></div>${suggestions.map((item, index) => `<button class="suggestion" role="option" aria-selected="false" data-search-index="${index}" data-suggestion-type="${escapeHTML(item.type)}" data-suggestion-id="${escapeHTML(item.id || "")}" data-suggestion-query="${escapeHTML(item.query || item.title)}"><span class="suggestion-icon">${item.type === "product" ? "◇" : item.type === "manufacturer" ? "М" : "▦"}</span><span class="suggestion-main"><strong>${escapeHTML(item.title)}</strong><span>${escapeHTML(item.subtitle || "")}</span></span>${item.priceMinor ? `<span class="suggestion-price">${money(item.priceMinor)}</span>` : "<span>→</span>"}</button>`).join("")}<button class="suggestion-all" data-search-all="${escapeHTML(trimmed)}">Показать все результаты по запросу «${escapeHTML(trimmed)}» →</button>` : `<div class="search-panel-header"><span>Совпадений нет</span></div><button class="suggestion-all" data-search-all="${escapeHTML(trimmed)}">Искать «${escapeHTML(trimmed)}» в каталоге →</button>`;
    panel.hidden = false;
    input.setAttribute("aria-expanded", "true");
  }, 160);

  function hideSearch() {
    const panel = qs("#search-panel");
    const input = qs("#global-search-input");
    if (panel) panel.hidden = true;
    input?.setAttribute("aria-expanded", "false");
    state.searchIndex = -1;
  }

  document.addEventListener("click", event => {
    const target = event.target;
    const routeButton = target.closest("[data-route]");
    if (routeButton) { navigate(routeButton.dataset.route); return; }

    const actionButton = target.closest("[data-action]");
    if (actionButton) {
      const action = actionButton.dataset.action;
      if (action === "toggle-catalog") toggleCatalog();
      else if (action === "close-overlay") closeOverlays();
      else if (action === "open-filters") openFilters();
      else if (action === "apply-filters") { closeOverlays({ restoreFocus: false }); commitCatalogState(); }
      else if (action === "reset-filters") resetFilters();
      else if (action === "retry-catalog") queueCatalogFetch({ immediate: true, force: true });
      else if (action === "go-catalog") navigate("catalog");
      else if (action === "go-home") navigate("home");
      else if (action === "clear-search") { const input = qs("#global-search-input"); input.value = ""; input.focus(); qs(".search-clear").hidden = true; updateSearchSuggestions(""); }
      else if (action === "clear-favorite-search") { state.favoriteQuery = ""; render(); }
      else if (action === "clear-cart-search") { state.cartQuery = ""; render(); }
      else if (action === "checkout") {
        if (!state.token || !state.user) { navigate("profile"); showToast("Войдите в тестовый аккаунт для оформления заказа"); }
        else openCheckout();
      }
      else if (action === "apply-promo") showToast(qs("#promo-code")?.value.trim() ? "Промокод проверен" : "Введите промокод");
      else if (action === "favorites-to-cart") addFavoritesToCart();
      else if (action === "selected-to-favorites") saveSelectedToFavorites();
      else if (action === "delete-selected") deleteSelectedCartItems();
      else if (action === "auto-fix-repeat") { state.repeatDraft.items.forEach(item => { if (item.selected) item.quantity = Math.max(1, Math.min(item.quantity, item.available)); }); renderRepeatModal(); }
      else if (action === "commit-repeat") commitRepeat();
      else if (action === "logout") { state.token = ""; state.user = null; state.orders = []; state.cart = []; state.favorites = []; localStorage.removeItem("sloy:token"); persist(); render(); showToast("Вы вышли из профиля"); }
      else if (action === "edit-profile") openEditProfile();
      else if (action === "edit-address") openEditAddress();
      else if (action === "set-city") openCityPicker();
      else if (action === "toggle-filter-block") { const block = actionButton.closest(".filter-block"); const collapsed = block.classList.toggle("is-collapsed"); actionButton.setAttribute("aria-expanded", String(!collapsed)); }
      return;
    }

    const category = target.closest("[data-category]");
    if (category) { navigate("catalog", { category: category.dataset.category }); return; }
    const manufacturer = target.closest("[data-manufacturer]");
    if (manufacturer) { navigate("catalog", { manufacturer: manufacturer.dataset.manufacturer }); return; }
    const collection = target.closest("[data-collection]");
    if (collection) { navigate("catalog", { collection: collection.dataset.collection }); return; }
    const series = target.closest("[data-series]");
    if (series) { navigate("catalog", { q: series.dataset.series, category: series.dataset.seriesCategory }); return; }

    const megaCategory = target.closest("[data-mega-category]");
    if (megaCategory) { state.megaCategory = megaCategory.dataset.megaCategory; renderMegaMenu(); return; }

    const favorite = target.closest("[data-favorite]");
    if (favorite) { event.stopPropagation(); toggleFavorite(favorite.dataset.favorite); return; }
    const add = target.closest("[data-add-cart]");
    if (add) { addToCart(add.dataset.addCart); return; }
    const product = target.closest("[data-product]");
    if (product) { openProduct(product.dataset.product); return; }

    const quick = target.closest("[data-quick-filter]");
    if (quick) { state.catalog[quick.dataset.quickFilter] = !state.catalog[quick.dataset.quickFilter]; state.catalog.page = 1; commitCatalogState(); return; }
    const collectionFilter = target.closest("[data-collection-filter]");
    if (collectionFilter) { state.catalog.collection = state.catalog.collection === collectionFilter.dataset.collectionFilter ? "" : collectionFilter.dataset.collectionFilter; state.catalog.page = 1; commitCatalogState(); return; }
    const removeFilter = target.closest("[data-remove-filter]");
    if (removeFilter) { const key = removeFilter.dataset.removeFilter; state.catalog[key] = typeof state.catalog[key] === "boolean" ? false : ""; state.catalog.page = 1; commitCatalogState(); return; }
    const view = target.closest("[data-view]");
    if (view) { state.catalog.view = view.dataset.view; commitCatalogState(); return; }
    const catalogPage = target.closest("[data-catalog-page]");
    if (catalogPage && !catalogPage.disabled) { state.catalog.page = Number(catalogPage.dataset.catalogPage); commitCatalogState({ scroll: true }); return; }
    const favoriteGroup = target.closest("[data-favorite-group]");
    if (favoriteGroup) { state.favoriteGroup = favoriteGroup.dataset.favoriteGroup; render(); return; }

    const collapse = target.closest("[data-collapse-group]");
    if (collapse) { const key = collapse.dataset.collapseGroup; state.collapsedGroups.has(key) ? state.collapsedGroups.delete(key) : state.collapsedGroups.add(key); render(); return; }
    const cartQty = target.closest("[data-cart-qty]");
    if (cartQty) { const item = state.cart.find(entry => String(entry.id) === cartQty.dataset.cartQty); if (item) changeCartQuantity(item.id, item.quantity + Number(cartQty.dataset.delta)); return; }
    const removeCart = target.closest("[data-remove-cart]");
    if (removeCart) { removeCartItem(removeCart.dataset.removeCart); return; }

    const repeatQty = target.closest("[data-repeat-qty]");
    if (repeatQty) { const item = state.repeatDraft?.items.find(entry => String(entry.productId) === repeatQty.dataset.repeatQty); if (item) { item.quantity = Math.max(1, item.quantity + Number(repeatQty.dataset.delta)); renderRepeatModal(); } return; }
    const repeatOrder = target.closest("[data-repeat-order]");
    if (repeatOrder) { openRepeatOrder(repeatOrder.dataset.repeatOrder); return; }
    const orderDetails = target.closest("[data-order-details]");
    if (orderDetails) { openOrderDetails(orderDetails.dataset.orderDetails); return; }

    const suggestion = target.closest("[data-suggestion-type]");
    if (suggestion) {
      const type = suggestion.dataset.suggestionType;
      if (type === "product") openProduct(suggestion.dataset.suggestionId);
      else if (type === "category") navigate("catalog", { category: suggestion.dataset.suggestionId });
      else if (type === "manufacturer") navigate("catalog", { manufacturer: suggestion.dataset.suggestionId });
      else navigate("catalog", { q: suggestion.dataset.suggestionQuery });
      hideSearch(); return;
    }
    const searchAll = target.closest("[data-search-all]");
    if (searchAll) { navigate("catalog", { q: searchAll.dataset.searchAll }); hideSearch(); return; }

    const toastAction = target.closest("[data-toast-action]");
    if (toastAction) {
      if (toastAction.dataset.toastAction === "open-cart") navigate("cart");
      if (toastAction.dataset.toastAction === "undo-remove") undoRemoveCartItem();
      target.closest(".toast")?.remove(); return;
    }

    if (target.id === "scrim" || target.classList.contains("modal-backdrop")) closeOverlays();
  });

  const updateFavoriteSearch = debounce(value => { state.favoriteQuery = value; render(); requestAnimationFrame(() => { const input = qs("#favorite-search"); if (input) { input.focus(); input.setSelectionRange(value.length, value.length); } }); }, 180);
  const updateCartSearch = debounce(value => { state.cartQuery = value; render(); requestAnimationFrame(() => { const input = qs("#cart-search"); if (input) { input.focus(); input.setSelectionRange(value.length, value.length); } }); }, 180);
  const updatePrice = debounce((key, value) => {
    const numeric = Number(value);
    state.catalog[key] = value === "" ? "" : Number.isFinite(numeric) && numeric >= 0 ? value : "";
    state.catalog.page = 1;
    commitCatalogState();
  }, 320);

  document.addEventListener("input", event => {
    const input = event.target;
    if (input.id === "global-search-input") {
      qs(".search-clear").hidden = !input.value;
      updateSearchSuggestions(input.value);
    } else if (input.id === "favorite-search") updateFavoriteSearch(input.value);
    else if (input.id === "cart-search") updateCartSearch(input.value);
    else if (input.matches("[data-price]")) updatePrice(input.dataset.price, input.value);
  });

  document.addEventListener("change", event => {
    const input = event.target;
    if (input.id === "catalog-sort") { state.catalog.sort = input.value; state.catalog.page = 1; commitCatalogState(); }
    else if (input.matches("[data-filter]")) {
      const key = input.dataset.filter;
      state.catalog[key] = input.checked ? input.value : "";
      state.catalog.page = 1;
      if (input.checked) qsa(`[data-filter="${key}"]`, input.closest(".drawer") || document).forEach(option => { if (option !== input) option.checked = false; });
      commitCatalogState();
    } else if (input.matches("[data-boolean-filter]")) {
      state.catalog[input.dataset.booleanFilter] = input.checked;
      state.catalog.page = 1;
      commitCatalogState();
    } else if (input.id === "cart-select-all") {
      const productIds = state.cart.filter(item => byId(item.productId)?.stock > 0).map(item => item.productId);
      setAllCartSelection(productIds, input.checked);
    } else if (input.matches("[data-cart-select]")) {
      const item = state.cart.find(entry => String(entry.id) === input.dataset.cartSelect);
      if (item) setCartItemSelection(item, input.checked);
    } else if (input.matches("[data-cart-qty-input]")) changeCartQuantity(input.dataset.cartQtyInput, input.value);
    else if (input.matches("[data-repeat-select]")) {
      const item = state.repeatDraft?.items.find(entry => String(entry.productId) === input.dataset.repeatSelect); if (item) item.selected = input.checked; renderRepeatModal();
    } else if (input.matches("[data-repeat-qty-input]")) {
      const item = state.repeatDraft?.items.find(entry => String(entry.productId) === input.dataset.repeatQtyInput); if (item) item.quantity = Math.max(1, Number(input.value) || 1); renderRepeatModal();
    }
  });

  document.addEventListener("focusin", event => {
    if (event.target.id === "global-search-input") updateSearchSuggestions(event.target.value);
  });

  document.addEventListener("submit", async event => {
    if (event.target.id === "login-form") {
      event.preventDefault();
      const form = event.target;
      const button = qs("button[type=submit]", form);
      const errorBox = qs("#login-error", form);
      button.disabled = true; button.textContent = "Входим…"; errorBox.hidden = true;
      const body = Object.fromEntries(new FormData(form));
      try {
        const response = await API.post("/auth/login", body);
        state.token = response.token ?? response.accessToken ?? response.access_token;
        if (!state.token) throw new Error("Сервер не вернул токен");
        localStorage.setItem("sloy:token", state.token);
        state.apiOnline = true;
        if (!await loadAccountData() || !state.user) throw new Error("Не удалось загрузить профиль");
        render(); showToast("Вы вошли в тестовый аккаунт");
      } catch (error) {
        state.token = "";
        state.user = null;
        localStorage.removeItem("sloy:token");
        errorBox.textContent = "Не удалось войти. Проверьте, что API запущен, и повторите."; errorBox.hidden = false;
        button.disabled = false; button.textContent = "Войти";
      }
    } else if (event.target.id === "checkout-form") {
      event.preventDefault();
      const form = event.target;
      if (!form.reportValidity()) return;
      const button = qs("button[type=submit]", form);
      button.disabled = true; button.textContent = "Оформляем…";
      const totals = cartTotals();
      const formData = Object.fromEntries(new FormData(form));
      let response;
      try {
        response = await API.post("/orders", { deliveryAddress: formData.address, comment: `${formData.slot}; оплата: ${formData.payment}` });
      } catch (error) {
        button.disabled = false;
        button.textContent = "Оформить заказ";
        showToast(error?.status === 409 ? "Проверьте выбранные товары и актуальные остатки" : "Не удалось оформить заказ. Попробуйте ещё раз");
        return;
      }
      const order = normalizeOrder(response);
      state.orders.unshift(order);
      for (const item of response.items || []) {
        const product = byId(item.productId ?? item.product_id);
        if (product) product.stock = Math.max(0, product.stock - Number(item.quantity || 0));
      }
      state.cart = state.cart.filter(item => !item.selected);
      persist(); closeOverlays({ restoreFocus: false }); navigate("orders"); showToast(`Заказ ${order.id} оформлен`);
    } else if (event.target.id === "edit-profile-form") {
      event.preventDefault(); const data = Object.fromEntries(new FormData(event.target)); state.user = { ...state.user, ...data }; closeOverlays({ restoreFocus: false }); render(); showToast("Личные данные сохранены");
    } else if (event.target.id === "edit-address-form") {
      event.preventDefault(); closeOverlays(); showToast("Адрес сохранён");
    }
  });

  document.addEventListener("keydown", event => {
    const input = event.target;
    const focusScope = qs("#modal-root .modal") || qs("#drawer-root .drawer");
    if (event.key === "Tab" && focusScope) {
      const focusable = qsa('button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])', focusScope).filter(element => element.offsetParent !== null);
      if (focusable.length) {
        const first = focusable[0], last = focusable[focusable.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
      }
    }
    if (input.id === "global-search-input") {
      const options = qsa("#search-panel [data-search-index]");
      if (event.key === "ArrowDown" && options.length) { event.preventDefault(); state.searchIndex = (state.searchIndex + 1) % options.length; }
      else if (event.key === "ArrowUp" && options.length) { event.preventDefault(); state.searchIndex = (state.searchIndex - 1 + options.length) % options.length; }
      else if (event.key === "Enter") { event.preventDefault(); if (state.searchIndex >= 0) options[state.searchIndex]?.click(); else if (input.value.trim()) navigate("catalog", { q: input.value.trim() }); }
      else if (event.key === "Escape") hideSearch();
      options.forEach((option, index) => option.setAttribute("aria-selected", String(index === state.searchIndex)));
      options[state.searchIndex]?.scrollIntoView({ block: "nearest" });
    } else if ((event.key === "Enter" || event.key === " ") && input.matches("[data-product]") && !input.matches("button")) { event.preventDefault(); openProduct(input.dataset.product); }
    const overlayOpen = qs("#modal-root").children.length || qs("#drawer-root").children.length || !qs("#catalog-mega").hidden;
    if (event.key === "Escape" && overlayOpen) closeOverlays();
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") { event.preventDefault(); qs("#global-search-input")?.focus(); }
  });

  function openEditProfile() {
    openModal(`<form id="edit-profile-form"><div class="modal-head"><div><h2>Личные данные</h2><p>Используем их только для доставки и связи по заказу</p></div><button type="button" class="close-button" data-action="close-overlay" aria-label="Закрыть">${icon("close")}</button></div><div class="modal-body"><div class="field"><label for="edit-name">Имя и фамилия</label><input id="edit-name" name="name" value="${escapeHTML(state.user?.name || "")}" required></div><div class="field"><label for="edit-email">Электронная почта</label><input id="edit-email" name="email" type="email" value="${escapeHTML(state.user?.email || "")}" required></div><div class="field"><label for="edit-phone">Телефон</label><input id="edit-phone" name="phone" type="tel" value="${escapeHTML(state.user?.phone || "")}" required></div></div><div class="modal-foot"><span></span><div class="modal-actions"><button type="button" class="button-secondary" data-action="close-overlay">Отмена</button><button type="submit" class="button-dark">Сохранить</button></div></div></form>`);
  }

  function openEditAddress() {
    openModal(`<form id="edit-address-form"><div class="modal-head"><div><h2>Адрес доставки</h2><p>Москва</p></div><button type="button" class="close-button" data-action="close-overlay" aria-label="Закрыть">${icon("close")}</button></div><div class="modal-body"><div class="field"><label for="address-street">Улица и дом</label><input id="address-street" name="street" value="ул. Хлебная, 12" required></div><div class="profile-grid"><div class="field"><label for="address-flat">Квартира</label><input id="address-flat" name="flat" value="48"></div><div class="field"><label for="address-entry">Подъезд</label><input id="address-entry" name="entry" value="2"></div></div><div class="field"><label for="address-comment">Комментарий курьеру</label><input id="address-comment" name="comment" value="Домофон 48"></div></div><div class="modal-foot"><span></span><div class="modal-actions"><button type="button" class="button-secondary" data-action="close-overlay">Отмена</button><button type="submit" class="button-dark">Сохранить</button></div></div></form>`);
  }

  function openCityPicker() {
    openModal(`<div class="modal-head"><div><h2>Город доставки</h2><p>От города зависят интервалы и стоимость</p></div><button class="close-button" data-action="close-overlay" aria-label="Закрыть">${icon("close")}</button></div><div class="modal-body"><label class="inline-search" style="width:100%">${icon("search")}<input type="search" placeholder="Введите город" value="Москва"></label><div class="tabs" style="margin-top:18px"><button class="tab active" data-action="close-overlay">Москва</button><button class="tab" data-action="close-overlay">Санкт-Петербург</button><button class="tab" data-action="close-overlay">Казань</button></div></div>`);
  }

  window.addEventListener("hashchange", () => {
    const previousRoute = state.route;
    parseHash();
    if (previousRoute === "catalog" && state.route !== "catalog") {
      state.catalogRemote.controller?.abort();
      state.catalogRemote.requestSeq += 1;
      state.catalogRemote.loading = false;
      state.catalogRemote.controller = null;
    }
    render();
    if (state.apiOnline && state.route === "catalog") queueCatalogFetch();
    window.scrollTo({ top: 0 });
  });
  document.addEventListener("click", event => { if (!event.target.closest(".global-search")) hideSearch(); });

  parseHash();
  render();
  renderMegaMenu();
  updateBadges();
  loadInitialData();
})();
