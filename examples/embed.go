// Package examples embeds the curated demo project that ships inside the
// graphjin binary. `graphjin serve --demo` without --path extracts
// DefaultDemoFS into ./graphjin-demo so a bare install boots a real,
// seeded example instead of an empty scaffold.
package examples

import "embed"

// DefaultDemoRoot is the directory within DefaultDemoFS that holds the
// default demo project.
const DefaultDemoRoot = "clinic-scheduler"

// DefaultDemoFS holds the zero-container clinic-scheduler demo: SQLite
// source, schema DDL, seed script, saved queries, workflows, and configs.
//
//go:embed all:clinic-scheduler
var DefaultDemoFS embed.FS
