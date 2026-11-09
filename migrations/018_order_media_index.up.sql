-- C2: indexed historical image retention; no snapshot values are rewritten.
CREATE INDEX order_items_managed_image_idx ON order_items (image, order_id) WHERE image LIKE '/media/%';
