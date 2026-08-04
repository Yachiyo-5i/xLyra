package migrations

import "embed"

// FS contains the production bootstrap schema. Keep only the initial schema
// here; post-release upgrades should be handled explicitly instead of replaying
// development-era migrations.
//
//go:embed 00001_initial_schema.sql
var FS embed.FS
