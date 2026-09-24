CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS products_name_trgm_idx ON products USING gin (lower(name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS products_short_name_trgm_idx ON products USING gin (lower(short_name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS products_sku_trgm_idx ON products USING gin (lower(sku) gin_trgm_ops);
CREATE UNIQUE INDEX IF NOT EXISTS products_sku_lower_uidx ON products (lower(sku));
CREATE INDEX IF NOT EXISTS products_series_trgm_idx ON products USING gin (lower(series) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS manufacturers_name_trgm_idx ON manufacturers USING gin (lower(name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS brands_name_trgm_idx ON brands USING gin (lower(name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS categories_name_trgm_idx ON categories USING gin (lower(name) gin_trgm_ops);
CREATE INDEX IF NOT EXISTS products_brand_idx ON products (brand_id);
CREATE INDEX IF NOT EXISTS products_created_at_idx ON products (created_at DESC);
CREATE INDEX IF NOT EXISTS products_popular_idx ON products (featured DESC, reviews_count DESC, rating DESC);
CREATE INDEX IF NOT EXISTS products_sale_idx ON products (price_minor) WHERE old_price_minor > price_minor;
CREATE INDEX IF NOT EXISTS products_badges_idx ON products USING gin (badges);
CREATE INDEX IF NOT EXISTS collection_products_product_idx ON collection_products (product_id, collection_id);
