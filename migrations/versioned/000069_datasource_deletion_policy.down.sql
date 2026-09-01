-- Migration: 000069_datasource_deletion_policy (down)

ALTER TABLE data_sources
    DROP COLUMN IF EXISTS deletion_policy;
