ALTER TABLE products DROP CONSTRAINT IF EXISTS products_brand_manufacturer_fk;
ALTER TABLE products ALTER COLUMN brand_id SET DEFAULT 'brand-unbranded';
ALTER TABLE brands DROP CONSTRAINT IF EXISTS brands_id_manufacturer_unique;
