-- +goose Up
-- Ephemeral per-credential refresh leases. Not part of backups: the row only
-- exists while an instance is refreshing, and TRUNCATE of site_credentials
-- CASCADE drops it on restore.
CREATE TABLE site_credential_refresh_leases (
  site_credential_id UUID PRIMARY KEY REFERENCES site_credentials(id) ON DELETE CASCADE,
  lease_id TEXT NOT NULL,
  lease_until TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE site_credential_refresh_leases;
