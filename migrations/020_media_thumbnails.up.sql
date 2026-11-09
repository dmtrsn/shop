-- P2.1: the one derived thumbnail of a managed image (ADR 022). The row exists
-- only after its file was written, and only for images larger than the
-- recipe's maximum side. A separate table keeps media_assets rows immutable,
-- and an asset without a row (older, or small enough) stays fully valid.
CREATE TABLE media_thumbnails (
    media_id text PRIMARY KEY REFERENCES media_assets(id),
    recipe text NOT NULL
        CONSTRAINT media_thumbnails_recipe_format CHECK (recipe ~ '^[a-z0-9][a-z0-9-]{0,63}$'),
    content_type text NOT NULL
        CONSTRAINT media_thumbnails_content_type CHECK (content_type IN ('image/jpeg', 'image/png')),
    size_bytes bigint NOT NULL
        CONSTRAINT media_thumbnails_size_positive CHECK (size_bytes > 0),
    width integer NOT NULL
        CONSTRAINT media_thumbnails_width_positive CHECK (width > 0),
    height integer NOT NULL
        CONSTRAINT media_thumbnails_height_positive CHECK (height > 0),
    sha256 text NOT NULL
        CONSTRAINT media_thumbnails_sha256_format CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL DEFAULT now()
);
