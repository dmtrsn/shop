-- Development databases only: drops thumbnail metadata. Assets and their
-- originals are unaffected; the thumbnails/ files are not touched.
DROP TABLE media_thumbnails;
