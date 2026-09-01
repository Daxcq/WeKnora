-- Migration: 000069_datasource_deletion_policy

ALTER TABLE data_sources
    ADD COLUMN IF NOT EXISTS deletion_policy VARCHAR(32) NOT NULL DEFAULT 'retain';

UPDATE data_sources
SET deletion_policy = 'retain'
WHERE deletion_policy IS NULL OR deletion_policy = '';
