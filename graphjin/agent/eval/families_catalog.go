package eval

import (
	"regexp"
	"strings"
)

// Extraction helpers shared by the generated task families. They read only what
// the catalog snapshot already publishes. Adding a field to the snapshot query
// would change the catalog fingerprint every frozen suite is stamped with, so
// these parse the published presentation instead, and fail closed: when a shape
// is not exactly what the catalog builder emits, they return nothing rather than
// a guess. A guessed relationship or value would become a task asserting
// something untrue about the customer's data.

// observedValuesPattern matches the closed-set example the catalog publishes for
// a column with few distinct values: "status values: active, trial, churned".
var observedValuesPattern = regexp.MustCompile(`^([_A-Za-z][_0-9A-Za-z]*) values:\s*(.+)$`)

// observedEqualityPattern matches the companion example that quotes one real
// value: `where: { status: { eq: "active" } }`. It is the cross-check that the
// closed set was parsed correctly.
var observedEqualityPattern = regexp.MustCompile(`where:\s*\{\s*([_A-Za-z][_0-9A-Za-z]*):\s*\{\s*eq:\s*"(.*)"\s*\}\s*\}`)

// observedColumnValues returns the closed set of values the catalog observed for
// a column, or nil when the column has none. The catalog publishes the set only
// for columns whose distinct values are few and stable, which is exactly the set
// a filter task may safely draw from.
func observedColumnValues(row CatalogRow) []string {
	column := strings.TrimSpace(row.ColumnName)
	if row.Kind != "column" || column == "" {
		return nil
	}
	examples := detailStringList(row.ExamplesJSON)
	if len(examples) == 0 {
		return nil
	}
	var values []string
	var quoted string
	var quotedSeen bool
	for _, example := range examples {
		example = strings.TrimSpace(example)
		if match := observedValuesPattern.FindStringSubmatch(example); match != nil && match[1] == column {
			values = splitObservedValues(match[2])
			continue
		}
		if match := observedEqualityPattern.FindStringSubmatch(example); match != nil && match[1] == column {
			quoted, quotedSeen = match[2], true
		}
	}
	if len(values) == 0 {
		return nil
	}
	// The set is joined with ", " for display, so a value containing that
	// separator is indistinguishable from two values. The quoted example carries
	// the first value verbatim: when it disagrees with the first parsed element,
	// the split was wrong and the whole set is unusable.
	if !quotedSeen || quoted != values[0] {
		return nil
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil
		}
	}
	return values
}

func splitObservedValues(raw string) []string {
	parts := strings.Split(raw, ", ")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		values = append(values, strings.TrimSpace(part))
	}
	return values
}

// detailStringList flattens the strings out of a catalog row's JSON payload.
// Payloads arrive either already decoded or as a JSON string, which
// decodeDetailValue resolves.
func detailStringList(raw any) []string {
	var out []string
	var walk func(any)
	walk = func(value any) {
		switch typed := decodeDetailValue(value).(type) {
		case string:
			out = append(out, typed)
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(raw)
	return out
}

// generatorRelationship is one verified edge between two local tables. From is
// the side holding the reference and To is the side referenced, matching the
// direction the catalog publishes.
type generatorRelationship struct {
	ID          string
	FromTable   string
	FromColumn  string
	ToTable     string
	ToColumn    string
	FromTableID string
}

// relationshipTitlePattern matches the title the catalog builder formats as
// "invoices.account_id -> accounts.id".
var relationshipTitlePattern = regexp.MustCompile(`^([_A-Za-z][_0-9A-Za-z]*)\.([_A-Za-z][_0-9A-Za-z]*)\s*->\s*([_A-Za-z][_0-9A-Za-z]*)\.([_A-Za-z][_0-9A-Za-z]*)$`)

// catalogRelationships returns the relationships whose two sides are both local
// tables the snapshot describes in full.
//
// The referencing side is taken from the card's structured table and column
// fields rather than from the title; only the referenced side is parsed. The
// parsed referencing side is then compared against the structured one, so a
// title whose format has drifted is dropped instead of silently reinterpreted.
// Relationships into sources the snapshot does not describe as tables — remote
// API joins, for one — are dropped for the same reason: a task cannot assert a
// join count over a table it cannot see the columns of.
func catalogRelationships(rows []CatalogRow, tables []generatorTable) []generatorRelationship {
	byName := make(map[string]generatorTable, len(tables))
	for _, table := range tables {
		byName[table.Name] = table
	}
	hasColumn := func(table generatorTable, name string) bool {
		for _, column := range table.Columns {
			if column.Name == name {
				return true
			}
		}
		return false
	}
	var out []generatorRelationship
	seen := map[string]struct{}{}
	for _, row := range rows {
		if row.Kind != "relationship" {
			continue
		}
		match := relationshipTitlePattern.FindStringSubmatch(strings.TrimSpace(row.Title))
		if match == nil {
			continue
		}
		fromTable, fromColumn, toTable, toColumn := match[1], match[2], match[3], match[4]
		// The structured fields are authoritative. A title that disagrees with
		// them describes a different edge than the card claims to be about.
		if structured := strings.TrimSpace(row.TableName); structured != "" && structured != fromTable {
			continue
		}
		if structured := strings.TrimSpace(row.ColumnName); structured != "" && structured != fromColumn {
			continue
		}
		parent, okParent := byName[fromTable]
		child, okChild := byName[toTable]
		if !okParent || !okChild {
			continue
		}
		if !hasColumn(parent, fromColumn) || !hasColumn(child, toColumn) {
			continue
		}
		if fromTable == toTable {
			// A self-reference makes "rows of A that have a B" degenerate.
			continue
		}
		key := fromTable + "." + fromColumn + "->" + toTable + "." + toColumn
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, generatorRelationship{
			ID: row.ID, FromTable: fromTable, FromColumn: fromColumn,
			ToTable: toTable, ToColumn: toColumn, FromTableID: parent.ID,
		})
	}
	return out
}
