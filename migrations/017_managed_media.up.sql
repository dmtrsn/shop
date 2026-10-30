-- Managed product media (ADR 019). The bytes live in the media store
-- (MEDIA_ROOT), never in PostgreSQL; a row describes one immutable,
-- normalized image and exists only after its file was written. deleted_at is
-- a tombstone: the ID is never reused and the row is never removed.
CREATE TABLE media_assets (
    id text PRIMARY KEY
        CONSTRAINT media_assets_id_format CHECK (id ~ '^med_[0-9a-f]{32}$'),
    content_type text NOT NULL
        CONSTRAINT media_assets_content_type CHECK (content_type IN ('image/jpeg', 'image/png')),
    size_bytes bigint NOT NULL
        CONSTRAINT media_assets_size_positive CHECK (size_bytes > 0),
    width integer NOT NULL
        CONSTRAINT media_assets_width_positive CHECK (width > 0),
    height integer NOT NULL
        CONSTRAINT media_assets_height_positive CHECK (height > 0),
    sha256 text NOT NULL
        CONSTRAINT media_assets_sha256_format CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX media_assets_live_created_idx ON media_assets (created_at DESC, id) WHERE deleted_at IS NULL;

-- Managed images of a product: at most one primary (position 0) and an
-- ordered gallery. Products keep their legacy image/gallery columns, which
-- stay authoritative until the product-media cut-over (C2/C3).
CREATE TABLE product_media (
    product_id text NOT NULL REFERENCES products(id),
    media_id text NOT NULL REFERENCES media_assets(id),
    role text NOT NULL
        CONSTRAINT product_media_role CHECK (role IN ('primary', 'gallery')),
    position integer NOT NULL
        CONSTRAINT product_media_position_nonnegative CHECK (position >= 0),
    CONSTRAINT product_media_primary_first CHECK (role <> 'primary' OR position = 0),
    PRIMARY KEY (product_id, role, position),
    CONSTRAINT product_media_once_per_role UNIQUE (product_id, role, media_id)
);
CREATE INDEX product_media_media_idx ON product_media (media_id);
