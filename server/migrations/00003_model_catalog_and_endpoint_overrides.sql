-- +goose Up
ALTER TABLE canonical_models
  ADD COLUMN pricing_variants JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE site_model_endpoint_overrides (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  site_model_id UUID NOT NULL REFERENCES site_models(id) ON DELETE CASCADE,
  mode TEXT NOT NULL DEFAULT 'inherit',
  endpoint_types JSONB NOT NULL DEFAULT '[]'::jsonb,
  reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(site_model_id)
);

CREATE INDEX site_model_endpoint_overrides_site_model_id_idx ON site_model_endpoint_overrides(site_model_id);

-- +goose Down
DROP TABLE site_model_endpoint_overrides;
ALTER TABLE canonical_models DROP COLUMN pricing_variants;
