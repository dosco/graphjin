// Package assets exposes the binary's embedded compose files, migrations, and
// seed scripts. The directory layout matches what the compose supervisor and
// db migrator expect:
//
//	compose/core.yml, dev.yml, demo.yml
//	migrations/*.sql
//	env/.env.example
package assets

import "embed"

//go:embed compose/*.yml
var ComposeFS embed.FS

//go:embed migrations/*.sql
var MigrationsFS embed.FS

//go:embed env/.env.example
var DefaultEnvExample []byte
