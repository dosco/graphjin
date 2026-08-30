package main

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

// Procedural world generation.
//
// The shipped demos are one organization each, and a model measured only
// against them can learn those particular tables rather than how to work in an
// organization it has never seen. A generated world is a different company with
// a different vocabulary and a different shape, produced deterministically from
// a seed, so a policy can be trained on some and measured on others.
//
// The point is not synthetic volume. It is that real schemas are awkward in
// specific, recurring ways — a column named for what it once meant, two tables
// using one word differently, a field that is usually null — and a model that
// has only seen tidy schemas has not met the problem.

type worldColumn struct {
	Name string
	Type string
	// Values is the closed set this column draws from. Columns with one become
	// the filters generated tasks can safely ask about.
	Values   []string
	Nullable bool
	// Note is written into the DDL as a comment, so a pathology is discoverable
	// by a human reading the schema even though nothing announces it.
	Note string
}

type worldTable struct {
	Name    string
	Label   string
	Columns []worldColumn
	// Parent is the table this one references, if any.
	Parent   string
	RowCount int
}

type worldSpec struct {
	Name    string
	Domain  string
	Tables  []worldTable
	Seed    int64
	Anchor  string
	Applied []string
}

// domainPack is a vocabulary: what this kind of company keeps records about.
type domainPack struct {
	Name    string
	AppName string
	Roots   []worldEntity
}

type worldEntity struct {
	Table   string
	Label   string
	Metric  string
	Date    string
	Status  []string
	Extra   []worldColumn
	Follows string
}

// domainPacks are deliberately ordinary businesses rather than invented ones.
// A model has to recognise the shape of an invoice, a shipment or a claim, and
// inventing vocabulary would measure something else.
var domainPacks = []domainPack{
	{
		Name: "logistics", AppName: "Freight Ops",
		Roots: []worldEntity{
			{Table: "carriers", Label: "name", Metric: "on_time_pct", Date: "onboarded_at",
				Status: []string{"active", "suspended", "probation"}},
			{Table: "warehouses", Label: "name", Metric: "capacity_pallets", Date: "opened_at",
				Status: []string{"open", "closed"}},
			{Table: "shipments", Label: "reference", Metric: "weight_kg", Date: "dispatched_at",
				Status: []string{"in_transit", "delivered", "delayed", "lost"}, Follows: "carriers"},
			{Table: "consignments", Label: "reference", Metric: "value_cents", Date: "booked_at",
				Status: []string{"booked", "packed", "shipped"}, Follows: "shipments"},
			{Table: "incidents", Label: "summary", Metric: "cost_cents", Date: "reported_at",
				Status: []string{"open", "resolved", "escalated"}, Follows: "shipments"},
		},
	},
	{
		Name: "clinic", AppName: "Care Operations",
		Roots: []worldEntity{
			{Table: "practices", Label: "name", Metric: "panel_size", Date: "registered_at",
				Status: []string{"active", "dormant"}},
			{Table: "providers", Label: "name", Metric: "weekly_slots", Date: "credentialed_at",
				Status: []string{"credentialed", "pending", "lapsed"}, Follows: "practices"},
			{Table: "appointments", Label: "reference", Metric: "duration_minutes", Date: "scheduled_at",
				Status: []string{"booked", "attended", "no_show", "cancelled"}, Follows: "providers"},
			{Table: "claims", Label: "reference", Metric: "amount_cents", Date: "submitted_at",
				Status: []string{"submitted", "paid", "rejected"}, Follows: "appointments"},
		},
	},
	{
		Name: "retail", AppName: "Store Operations",
		Roots: []worldEntity{
			{Table: "suppliers", Label: "name", Metric: "lead_time_days", Date: "contracted_at",
				Status: []string{"preferred", "standard", "under_review"}},
			{Table: "products", Label: "name", Metric: "unit_price_cents", Date: "listed_at",
				Status: []string{"stocked", "discontinued", "backorder"}, Follows: "suppliers"},
			{Table: "stores", Label: "name", Metric: "floor_sqm", Date: "opened_at",
				Status: []string{"trading", "refit", "closed"}},
			{Table: "orders", Label: "reference", Metric: "total_cents", Date: "placed_at",
				Status: []string{"placed", "fulfilled", "returned", "cancelled"}, Follows: "stores"},
			{Table: "returns", Label: "reference", Metric: "refund_cents", Date: "received_at",
				Status: []string{"received", "refunded", "refused"}, Follows: "orders"},
		},
	},
}

// Pathologies are the awkwardness a real schema has and a generated one would
// not unless it were asked for.
const (
	// PathologyDistractorColumns adds a column whose name is one word away from
	// the one that matters, so choosing between them requires reading the
	// catalog rather than pattern-matching the name.
	PathologyDistractorColumns = "distractor-columns"
	// PathologySynonymCollision gives two tables a column of the same name
	// meaning different things — the "status means four things" problem that
	// makes a schema need explaining.
	PathologySynonymCollision = "synonym-collision"
	// PathologyLegacyColumns keeps a column that looks authoritative and is
	// stale, so answering from it is wrong in a way nothing flags.
	PathologyLegacyColumns = "legacy-columns"
	// PathologyNullableGaps leaves a field unset on some rows, so counting
	// rows and counting values stop being the same question.
	PathologyNullableGaps = "nullable-gaps"
)

var supportedPathologies = []string{
	PathologyDistractorColumns,
	PathologySynonymCollision,
	PathologyLegacyColumns,
	PathologyNullableGaps,
}

func validatePathologies(requested []string) ([]string, error) {
	known := map[string]bool{}
	for _, name := range supportedPathologies {
		known[name] = true
	}
	out := make([]string, 0, len(requested))
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !known[name] {
			return nil, fmt.Errorf("unknown pathology %q; known: %s", name, strings.Join(supportedPathologies, ", "))
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func packByName(name string) (domainPack, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	for _, pack := range domainPacks {
		if pack.Name == name {
			return pack, nil
		}
	}
	names := make([]string, 0, len(domainPacks))
	for _, pack := range domainPacks {
		names = append(names, pack.Name)
	}
	return domainPack{}, fmt.Errorf("unknown domain %q; known: %s", name, strings.Join(names, ", "))
}

// buildWorld lays out a world's tables. Everything is derived from the seed, so
// the same seed is the same company every time — a world nobody can reproduce
// is not a world anyone can be measured against twice.
func buildWorld(pack domainPack, seed int64, tableCount int, pathologies []string, anchor string) worldSpec {
	rng := rand.New(rand.NewSource(seed))
	applied := map[string]bool{}
	for _, name := range pathologies {
		applied[name] = true
	}
	if tableCount <= 0 || tableCount > len(pack.Roots) {
		tableCount = len(pack.Roots)
	}
	world := worldSpec{
		Name: pack.AppName, Domain: pack.Name, Seed: seed, Anchor: anchor,
		Applied: append([]string(nil), pathologies...),
	}
	for index, entity := range pack.Roots[:tableCount] {
		table := worldTable{
			Name: entity.Table, Label: entity.Label,
			RowCount: 8 + rng.Intn(14),
		}
		if entity.Follows != "" {
			for _, existing := range world.Tables {
				if existing.Name == entity.Follows {
					table.Parent = entity.Follows
					break
				}
			}
		}
		table.Columns = append(table.Columns,
			worldColumn{Name: "id", Type: "Bigint! @id"},
			worldColumn{Name: entity.Label, Type: "Text!"},
		)
		if table.Parent != "" {
			table.Columns = append(table.Columns, worldColumn{
				Name: singular(table.Parent) + "_id",
				Type: fmt.Sprintf("Bigint! @relation(type: %s, field: id)", table.Parent),
			})
		}
		table.Columns = append(table.Columns,
			worldColumn{Name: "status", Type: "Text!", Values: entity.Status},
			worldColumn{Name: entity.Metric, Type: "Bigint!"},
			worldColumn{Name: entity.Date, Type: "TimestampWithTimeZone!"},
		)
		if applied[PathologyDistractorColumns] {
			table.Columns = append(table.Columns, worldColumn{
				Name: "status_code", Type: "Text", Nullable: true,
				Values: []string{"S1", "S2", "S9"},
				Note:   "internal code, not the business status; kept for a downstream export",
			})
		}
		if applied[PathologyLegacyColumns] && index%2 == 0 {
			table.Columns = append(table.Columns, worldColumn{
				Name: "legacy_" + entity.Metric, Type: "Bigint", Nullable: true,
				Note: "frozen at migration time; superseded by " + entity.Metric,
			})
		}
		if applied[PathologyNullableGaps] {
			table.Columns = append(table.Columns, worldColumn{
				Name: "note", Type: "Text", Nullable: true,
				Note: "set on roughly a third of rows",
			})
		}
		world.Tables = append(world.Tables, table)
	}
	if applied[PathologySynonymCollision] && len(world.Tables) > 1 {
		// One word, two meanings, in one schema. Nothing marks it: a caller has
		// to read both tables' cards to find out that "state" is a workflow
		// stage here and a place there.
		world.Tables[0].Columns = append(world.Tables[0].Columns, worldColumn{
			Name: "state", Type: "Text", Nullable: true,
			Values: []string{"draft", "confirmed"},
			Note:   "workflow stage — not a geographic state",
		})
		last := len(world.Tables) - 1
		world.Tables[last].Columns = append(world.Tables[last].Columns, worldColumn{
			Name: "state", Type: "Text", Nullable: true,
			Values: []string{"CA", "TX", "NY"},
			Note:   "geographic state — not a workflow stage",
		})
	}
	return world
}

func singular(table string) string {
	return strings.TrimSuffix(table, "s")
}

// renderWorldDDL writes the schema in GraphJin's DDL dialect.
func renderWorldDDL(world worldSpec) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# %s — generated world (domain %s, seed %d).\n", world.Name, world.Domain, world.Seed)
	if len(world.Applied) != 0 {
		fmt.Fprintf(&out, "# Deliberate schema pathologies: %s\n", strings.Join(world.Applied, ", "))
	}
	out.WriteString("# Regenerate with: graphjin env new-world --domain " + world.Domain + fmt.Sprintf(" --seed %d\n\n", world.Seed))
	for _, table := range world.Tables {
		fmt.Fprintf(&out, "type %s {\n", table.Name)
		for _, column := range table.Columns {
			fmt.Fprintf(&out, "  %s: %s", column.Name, column.Type)
			if column.Note != "" {
				fmt.Fprintf(&out, " # %s", column.Note)
			}
			out.WriteString("\n")
		}
		out.WriteString("}\n\n")
	}
	return out.String()
}
