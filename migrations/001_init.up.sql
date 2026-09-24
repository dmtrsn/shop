CREATE TABLE users (
    id text PRIMARY KEY,
    name text NOT NULL,
    email text NOT NULL,
    phone text NOT NULL DEFAULT '',
    password_hash text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_lower_uidx ON users (lower(email));

CREATE TABLE sessions (
    token_hash text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE INDEX sessions_expiry_idx ON sessions(expires_at);

CREATE TABLE categories (
    id text PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    parent_id text REFERENCES categories(id) ON DELETE SET NULL,
    icon text NOT NULL DEFAULT '',
    sort_order integer NOT NULL DEFAULT 0
);

CREATE TABLE manufacturers (
    id text PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    name text NOT NULL,
    country text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    logo text NOT NULL DEFAULT '',
    sort_order integer NOT NULL DEFAULT 0
);

CREATE TABLE brands (
    id text PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    name text NOT NULL,
    manufacturer_id text NOT NULL REFERENCES manufacturers(id),
    description text NOT NULL DEFAULT '',
    logo text NOT NULL DEFAULT '',
    sort_order integer NOT NULL DEFAULT 0
);
CREATE INDEX brands_manufacturer_idx ON brands(manufacturer_id);

CREATE TABLE products (
    id text PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    sku text NOT NULL UNIQUE,
    name text NOT NULL,
    short_name text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT '',
    price_minor bigint NOT NULL CHECK (price_minor >= 0),
    old_price_minor bigint NOT NULL DEFAULT 0 CHECK (old_price_minor >= 0),
    currency char(3) NOT NULL DEFAULT 'RUB',
    unit text NOT NULL,
    weight_grams integer NOT NULL DEFAULT 0 CHECK (weight_grams >= 0),
    image text NOT NULL DEFAULT '',
    gallery text[] NOT NULL DEFAULT '{}',
    category_id text NOT NULL REFERENCES categories(id),
    manufacturer_id text NOT NULL REFERENCES manufacturers(id),
    brand_id text NOT NULL DEFAULT 'brand-unbranded' REFERENCES brands(id),
    series text NOT NULL DEFAULT '',
    tags text[] NOT NULL DEFAULT '{}',
    badges text[] NOT NULL DEFAULT '{}',
    ingredients text NOT NULL DEFAULT '',
    nutrition text NOT NULL DEFAULT '',
    shelf_life_days integer NOT NULL DEFAULT 0,
    rating numeric(2,1) NOT NULL DEFAULT 0 CHECK (rating >= 0 AND rating <= 5),
    reviews_count integer NOT NULL DEFAULT 0 CHECK (reviews_count >= 0),
    stock integer NOT NULL DEFAULT 0 CHECK (stock >= 0),
    minimum_quantity integer NOT NULL DEFAULT 1 CHECK (minimum_quantity > 0),
    quantity_step integer NOT NULL DEFAULT 1 CHECK (quantity_step > 0),
    featured boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    search_vector tsvector GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(name, '') || ' ' || coalesce(short_name, '') || ' ' || coalesce(sku, '') || ' ' || coalesce(description, ''))
    ) STORED
);
CREATE INDEX products_category_idx ON products(category_id);
CREATE INDEX products_manufacturer_idx ON products(manufacturer_id);
CREATE INDEX products_price_idx ON products(price_minor);
CREATE INDEX products_available_idx ON products(stock) WHERE stock > 0;
CREATE INDEX products_search_idx ON products USING gin(search_vector);
CREATE INDEX products_tags_idx ON products USING gin(tags);

CREATE TABLE collections (
    id text PRIMARY KEY,
    slug text NOT NULL UNIQUE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    accent text NOT NULL DEFAULT 'cocoa',
    image text NOT NULL DEFAULT '',
    starts_at timestamptz,
    ends_at timestamptz,
    sort_order integer NOT NULL DEFAULT 0
);

CREATE TABLE collection_products (
    collection_id text NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    product_id text NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    sort_order integer NOT NULL DEFAULT 0,
    PRIMARY KEY (collection_id, product_id)
);

CREATE TABLE cart_items (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    product_id text NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    quantity integer NOT NULL CHECK (quantity > 0),
    selected boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, product_id)
);
CREATE INDEX cart_items_updated_idx ON cart_items(user_id, updated_at DESC);

CREATE TABLE favorites (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    product_id text NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, product_id)
);

CREATE TABLE orders (
    id text PRIMARY KEY,
    number text NOT NULL UNIQUE,
    user_id text NOT NULL REFERENCES users(id),
    status text NOT NULL,
    status_label text NOT NULL,
    item_count integer NOT NULL CHECK (item_count > 0),
    total_minor bigint NOT NULL CHECK (total_minor >= 0),
    delivery_address text NOT NULL,
    comment text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz
);
CREATE SEQUENCE order_number_seq START WITH 10483;
CREATE INDEX orders_user_created_idx ON orders(user_id, created_at DESC);

CREATE TABLE order_items (
    order_id text NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
    position integer NOT NULL,
    product_id text NOT NULL,
    sku text NOT NULL,
    name text NOT NULL,
    image text NOT NULL DEFAULT '',
    manufacturer_id text NOT NULL DEFAULT '',
    manufacturer text NOT NULL DEFAULT '',
    quantity integer NOT NULL CHECK (quantity > 0),
    unit_price_minor bigint NOT NULL CHECK (unit_price_minor >= 0),
    line_total_minor bigint NOT NULL CHECK (line_total_minor >= 0),
    replacement_product_id text NOT NULL DEFAULT '',
    PRIMARY KEY (order_id, position)
);
CREATE INDEX order_items_product_idx ON order_items(product_id);
