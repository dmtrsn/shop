-- C3: set once per product by `shopctl media migrate-legacy` (ADR 021). While
-- NULL, customers may still fall back to products.image/gallery; once set, the
-- product's images come from product_media only, so detaching every image
-- leaves it without images and a rerun never re-imports the legacy files.
ALTER TABLE products ADD COLUMN legacy_media_retired_at timestamptz;
