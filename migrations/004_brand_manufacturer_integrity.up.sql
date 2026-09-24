ALTER TABLE brands
    ADD CONSTRAINT brands_id_manufacturer_unique UNIQUE (id, manufacturer_id);

ALTER TABLE products
    ADD CONSTRAINT products_brand_manufacturer_fk
    FOREIGN KEY (brand_id, manufacturer_id)
    REFERENCES brands (id, manufacturer_id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE products ALTER COLUMN brand_id DROP DEFAULT;
