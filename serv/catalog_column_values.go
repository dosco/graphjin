package serv

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

// Enum-like column values are sampled here rather than in the catalog build,
// which is deliberately pure: schema in, cards out, no data access. Without them
// a status column's example is a placeholder — where: { status: { eq: "<status>" } }
// — and a model has no way to learn the real values short of sampling rows itself.
//
// Benchmark generation 2028.1 measured what that costs. Asked to close a ticket
// that had been "sorted out", the agent wrote status "closed" against a schema
// whose statuses are open, pending, and resolved: it could not have known, and
// every one of the five planning-gap findings in that run was this.
//
// The cardinality cap is the privacy guard. A column with more than a handful of
// distinct values is free text or an identifier, not an enum, and publishes
// nothing.
const (
	columnValueMaxDistinct = 8
	columnValueMaxLength   = 40
	columnValueSampleRows  = columnValueMaxDistinct + 1
	columnValueTimeout     = 3 * time.Second
)

// observedColumnValues returns the sampled sets, sampling once per service. Every
// catalog build path calls this, including per-request overlay builds, so it must
// not re-query the database each time. Values refresh when the process does; an
// enum gaining a value mid-run is not worth a live query on every card read.
//
// Sampling is retried until an attempt can actually be made. A sync.Once here meant
// that if the first catalog build happened before GraphJin finished initialising,
// the closure returned without sampling, the Once was spent, and the process
// published no column values for its entire life. That is a startup race, so it
// struck at random: of two benchmark runs on the same build, one published values
// and one published none, which made the missing values look like a per-column
// problem for days.
//
// An attempt that runs and finds nothing still counts as done, so a schema with no
// enum-like columns does not re-query on every build.
func (s *graphjinService) observedColumnValues() map[string][]string {
	if s == nil || s.columnValueSamplingDisabled() {
		return nil
	}
	values, firstSample := s.sampleColumnValuesOnce()
	if firstSample && len(values) != 0 {
		// The catalog is materialised, and anything built before this point holds
		// cards with no values. Sampling cannot run until the engine can describe its
		// own schema, so the first build almost always precedes it: that is why cards
		// kept arriving without values even though every layer sampled correctly in
		// isolation. Refresh once, outside the sampling lock, so the rebuild can call
		// back in without deadlocking.
		s.markCatalogChanged("column value sampling")
	}
	return values
}

// sampleColumnValuesOnce performs the sampling attempt under its own lock and
// reports whether this call was the one that completed it.
func (s *graphjinService) sampleColumnValuesOnce() (map[string][]string, bool) {
	s.columnValuesMu.Lock()
	defer s.columnValuesMu.Unlock()
	if s.columnValuesSampled {
		return s.columnValues, false
	}
	md, err := s.metadataForColumnValues()
	if err != nil || md == nil {
		// GraphJin is not ready to describe its own schema yet. Leave the attempt
		// unmade so a later build can take it.
		return nil, false
	}
	s.columnValues = s.sampleObservedColumnValues(context.Background(), md)
	s.columnValuesSampled = true
	return s.columnValues, true
}

func (s *graphjinService) metadataForColumnValues() (*core.MetadataSnapshot, error) {
	if s.gj == nil {
		return nil, nil
	}
	return s.gj.MetadataSnapshot(s.metadataSnapshotExcludes()...)
}

// sampleObservedColumnValues returns closed value sets for enum-like columns,
// keyed by catalog column card id. A failure to sample is never fatal: the card
// falls back to its placeholder example.
func (s *graphjinService) sampleObservedColumnValues(ctx context.Context, md *core.MetadataSnapshot) map[string][]string {
	if s == nil || md == nil || len(s.dbs) == 0 || s.columnValueSamplingDisabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, columnValueTimeout)
	defer cancel()

	out := map[string][]string{}
	for _, column := range md.Columns {
		if !enumLikeColumn(column.ColumnName, column.Type, column.Array) {
			continue
		}
		db := s.dbs[column.DatabaseName]
		if db == nil {
			continue
		}
		values, ok := sampleDistinctValues(ctx, db, column.SchemaName, column.TableName, column.ColumnName)
		if !ok {
			continue
		}
		out["column:"+column.ID] = values
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *graphjinService) columnValueSamplingDisabled() bool {
	return s != nil && s.conf != nil && s.conf.DisableColumnValueSampling
}

// enumLikeColumn keeps sampling to columns whose name and type suggest a small
// closed vocabulary. The name test mirrors the catalog's own status heuristic and
// adds the vocabularies an organizational schema tends to carry.
func enumLikeColumn(name, columnType string, array bool) bool {
	if array {
		return false
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return false
	}
	var named bool
	for _, hint := range []string{"status", "state", "type", "category", "severity", "priority", "plan", "tier", "role", "kind", "stage"} {
		if strings.Contains(lower, hint) {
			named = true
			break
		}
	}
	if !named {
		return false
	}
	// Text-ish only. A numeric or timestamp column carrying one of those names is
	// not an enum worth publishing.
	t := strings.ToLower(strings.TrimSpace(columnType))
	for _, textual := range []string{"char", "text", "string", "enum", "clob"} {
		if strings.Contains(t, textual) {
			return true
		}
	}
	return t == ""
}

// sampleDistinctValues reads one more row than the cap so an over-cardinality
// column can be recognised and skipped rather than truncated.
func sampleDistinctValues(ctx context.Context, db *sql.DB, schema, table, column string) ([]string, bool) {
	if db == nil || table == "" || column == "" {
		return nil, false
	}
	query := fmt.Sprintf(
		`SELECT DISTINCT %s FROM %s WHERE %s IS NOT NULL LIMIT %d`,
		quoteSQLIdentifier(column), qualifiedSQLName(schema, table), quoteSQLIdentifier(column), columnValueSampleRows)
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	values := make([]string, 0, columnValueSampleRows)
	for rows.Next() {
		var raw sql.NullString
		if err := rows.Scan(&raw); err != nil {
			return nil, false
		}
		value := strings.TrimSpace(raw.String)
		if !raw.Valid || value == "" || len(value) > columnValueMaxLength {
			// A long value means this is not the closed vocabulary it looked like.
			return nil, false
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, false
	}
	if len(values) == 0 || len(values) > columnValueMaxDistinct {
		return nil, false
	}
	sort.Strings(values)
	return values, true
}

// quoteSQLIdentifier double-quotes an identifier, which every engine GraphJin
// supports accepts. Embedded quotes are doubled so a hostile column name cannot
// break out; identifiers here come from schema introspection, not callers.
func quoteSQLIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func qualifiedSQLName(schema, table string) string {
	if schema == "" || strings.EqualFold(schema, "main") {
		return quoteSQLIdentifier(table)
	}
	return quoteSQLIdentifier(schema) + "." + quoteSQLIdentifier(table)
}
