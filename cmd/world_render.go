package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

// renderWorldConfig writes the project config for a generated world.
//
// It is deliberately the same shape a hand-written project has: one SQLite
// source, dev identity, no special casing. A generated world that needed its
// own boot path would stop being a test of the real one.
func renderWorldConfig(world worldSpec) string {
	var out strings.Builder
	fmt.Fprintf(&out, "app_name: %s\n", world.Name)
	out.WriteString(`mode: dev
host_port: 0.0.0.0:8084
web_ui: true
log_level: "info"
log_format: "plain"
production: false
default_block: false
default_limit: 50

identity:
  role_claims: ["roles"]
  admin_roles: ["admin"]

auth:
  type: none
  development: true

mcp:
  allow_config_updates: false
  allow_schema_updates: false

sources:
  - name: app
    kind: database
    default: true
    type: sqlite
    access:
      read: public
      write: authenticated
      delete: blocked
    capabilities:
      data.read: true
      data.write: true
      schema.read: true
      schema.write: false
`)
	return out.String()
}

// renderWorldSeed writes a seed script of literal rows.
//
// Rows are written out rather than generated in JavaScript so the data is a
// property of the seed and not of whatever the seeding runtime does with
// randomness. Dates are relative to the seed run, as the shipped demos are, so
// a world stays answerable as it ages; pinning the environment's clock is what
// makes a run reproducible.
func renderWorldSeed(world worldSpec) string {
	rng := rand.New(rand.NewSource(world.Seed))
	var out strings.Builder
	out.WriteString(`const seedOptions = { source: "app", user_id: "world-seed", role: "user" };

function insert(query, variables) {
  return graphql(query, variables, seedOptions);
}

const DAY_MS = 86400000;
const seedNowMs = Date.now();

function day(offset) {
  return new Date(seedNowMs + offset * DAY_MS).toISOString().slice(0, 10);
}

function stamp(offset, hhmm) {
  return day(offset) + "T" + hhmm + ":00Z";
}

`)
	for _, table := range world.Tables {
		rows := renderWorldRows(rng, world, table)
		fmt.Fprintf(&out, "insert(\n  `mutation { %s(insert: $rows) { id } }`,\n  { rows: [\n", table.Name)
		out.WriteString(strings.Join(rows, ",\n"))
		out.WriteString("\n  ] }\n);\n\n")
	}
	return out.String()
}

func renderWorldRows(rng *rand.Rand, world worldSpec, table worldTable) []string {
	parentRows := 0
	for _, candidate := range world.Tables {
		if candidate.Name == table.Parent {
			parentRows = candidate.RowCount
		}
	}
	rows := make([]string, 0, table.RowCount)
	for index := 1; index <= table.RowCount; index++ {
		fields := make([]string, 0, len(table.Columns))
		for _, column := range table.Columns {
			value := worldValue(rng, table, column, index, parentRows)
			if value == "" {
				continue
			}
			fields = append(fields, fmt.Sprintf("%s: %s", column.Name, value))
		}
		rows = append(rows, "    { "+strings.Join(fields, ", ")+" }")
	}
	return rows
}

func worldValue(rng *rand.Rand, table worldTable, column worldColumn, index, parentRows int) string {
	switch {
	case column.Name == "id":
		return fmt.Sprintf("%d", index)
	case strings.HasSuffix(column.Name, "_id") && table.Parent != "":
		if parentRows <= 0 {
			return "1"
		}
		return fmt.Sprintf("%d", 1+rng.Intn(parentRows))
	case column.Name == table.Label:
		return fmt.Sprintf("%q", worldLabel(rng, table.Name, index))
	case len(column.Values) != 0:
		// A nullable column with a closed set is left unset on some rows, which
		// is what makes counting rows and counting values different questions.
		if column.Nullable && rng.Intn(3) == 0 {
			return ""
		}
		return fmt.Sprintf("%q", column.Values[rng.Intn(len(column.Values))])
	case strings.HasPrefix(column.Type, "Bigint"):
		if column.Nullable && rng.Intn(2) == 0 {
			return ""
		}
		return fmt.Sprintf("%d", 100+rng.Intn(90000))
	case strings.HasPrefix(column.Type, "TimestampWithTimeZone"):
		return fmt.Sprintf("stamp(%d, %q)", -rng.Intn(180), fmt.Sprintf("%02d:%02d", rng.Intn(24), rng.Intn(60)))
	case column.Nullable:
		if rng.Intn(3) != 0 {
			return ""
		}
		return fmt.Sprintf("%q", "recorded by operations")
	default:
		return fmt.Sprintf("%q", "value-"+fmt.Sprint(index))
	}
}

var worldNameParts = []string{
	"Northwind", "Harborlight", "Meridian", "Quartzline", "Blackpine", "Evergate",
	"Silverbrook", "Ironvale", "Redcedar", "Fairmount", "Longwater", "Ashford",
	"Kingsway", "Thornbury", "Westmoor", "Calder", "Draycott", "Pellham",
}

func worldLabel(rng *rand.Rand, table string, index int) string {
	if strings.Contains(table, "shipment") || strings.Contains(table, "order") ||
		strings.Contains(table, "claim") || strings.Contains(table, "appointment") ||
		strings.Contains(table, "return") || strings.Contains(table, "consignment") {
		return fmt.Sprintf("%s-%04d", strings.ToUpper(singular(table))[:3], 1000+index)
	}
	part := worldNameParts[rng.Intn(len(worldNameParts))]
	suffixes := []string{"Group", "Holdings", "Partners", "Works", "Collective", "Industries"}
	return fmt.Sprintf("%s %s", part, suffixes[rng.Intn(len(suffixes))])
}

// writeWorld lays the project out on disk.
func writeWorld(world worldSpec, directory string) error {
	if entries, err := os.ReadDir(directory); err == nil && len(entries) != 0 {
		return fmt.Errorf("%s is not empty; generated worlds are written to a fresh directory", directory)
	}
	files := map[string]string{
		"dev.yml":            renderWorldConfig(world),
		"schema-ddl/app.ddl": renderWorldDDL(world),
		"seed/app.js":        renderWorldSeed(world),
		"README.md":          renderWorldReadme(world),
	}
	for name, contents := range files {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func renderWorldReadme(world worldSpec) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n\nA generated GraphJin world: domain %s, seed %d.\n\n",
		world.Name, world.Domain, world.Seed)
	out.WriteString("Regenerate exactly:\n\n```bash\ngraphjin env new-world --domain " +
		world.Domain + fmt.Sprintf(" --seed %d --tables %d", world.Seed, len(world.Tables)))
	if len(world.Applied) != 0 {
		out.WriteString(" --pathologies " + strings.Join(world.Applied, ","))
	}
	out.WriteString("\n```\n\n")
	fmt.Fprintf(&out, "## Tables\n\n")
	for _, table := range world.Tables {
		fmt.Fprintf(&out, "- `%s` (%d rows)", table.Name, table.RowCount)
		if table.Parent != "" {
			fmt.Fprintf(&out, " → `%s`", table.Parent)
		}
		out.WriteString("\n")
	}
	if len(world.Applied) != 0 {
		out.WriteString("\n## Deliberate awkwardness\n\nReal schemas are hard in specific ways. " +
			"This world was asked to be hard in these:\n\n")
		for _, name := range world.Applied {
			fmt.Fprintf(&out, "- **%s** — %s\n", name, pathologyDescription(name))
		}
	}
	out.WriteString("\n## Use\n\n```bash\ngraphjin eval create --path . --writable --scale 300 --composition coverage\ngraphjin env serve --path . --suite eval/suite.yml --pool 4\n```\n")
	return out.String()
}

func pathologyDescription(name string) string {
	switch name {
	case PathologyDistractorColumns:
		return "a column one word away from the one that matters, so the right choice needs the catalog rather than the name"
	case PathologySynonymCollision:
		return "one word meaning two things in one schema, with nothing marking which is which"
	case PathologyLegacyColumns:
		return "a column that looks authoritative and is stale, so answering from it is wrong and nothing flags it"
	case PathologyNullableGaps:
		return "fields unset on some rows, so counting rows and counting values are different questions"
	}
	return ""
}
