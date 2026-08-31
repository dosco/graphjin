// Package examples embeds the curated demo project that ships inside the
// graphjin binary. `graphjin serve --demo` without --path extracts
// DefaultDemoFS into ./graphjin-demo so a bare install boots a real,
// seeded example instead of an empty scaffold.
package examples

import "embed"

// DefaultDemoRoot is the directory within DefaultDemoFS that holds the
// default demo project.
const DefaultDemoRoot = "saas-ops"

// DefaultDemoFS holds the zero-container saas-ops demo: SQLite source,
// schema DDL, seed script, saved queries, workflows, and configs.
//
// The paths are listed rather than globbed with all:saas-ops, which also swept
// in demo/ and .graphjin/ — gitignored local runtime state that exists only if
// somebody ran the demo in their clone. Two builds of the same commit then
// differed by whether the maintainer had done that, which matters now that the
// binary's own hash is published as build identity. Extraction already skipped
// those directories, so nothing needed them; scripts/ is dropped for the same
// reason it is skipped there — it depends on the surrounding examples tree.
//
// embed_test.go asserts both halves of this: that the excluded state is absent,
// and that everything the demo actually boots from is present.
//
//go:embed saas-ops/.env.example
//go:embed saas-ops/PROMPTS.md
//go:embed saas-ops/README.md
//go:embed saas-ops/agentic.yml
//go:embed saas-ops/dev.yml
//go:embed saas-ops/prod.yml
//go:embed saas-ops/files
//go:embed saas-ops/queries
//go:embed saas-ops/schema-ddl
//go:embed saas-ops/seed
//go:embed saas-ops/specs
//go:embed saas-ops/workflows
var DefaultDemoFS embed.FS
