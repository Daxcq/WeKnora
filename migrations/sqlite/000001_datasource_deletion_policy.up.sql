ALTER TABLE data_sources
    ADD COLUMN deletion_policy VARCHAR(32) NOT NULL DEFAULT 'retain';
