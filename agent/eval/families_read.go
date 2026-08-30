package eval

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// Read families derived from facts the catalog publishes beyond a single
// column's statistics: the closed set of values a column actually holds, and
// the relationships between tables. Every task these emit is verified against
// the live database before it can enter a suite, so a shape the schema cannot
// serve disappears instead of becoming a question nobody can answer.

const (
	// Emission caps keep one wide table from crowding the candidate pool. They
	// bound breadth per column rather than total output, so a larger schema
	// still yields proportionally more tasks.
	maxObservedValuesPerColumn = 4
	maxMetricColumnsPerTable   = 3
	maxValueColumnsPerTable    = 3
)

// windowedFilterOffsets is deliberately shorter than the plain window family's
// seven offsets: each offset here multiplies by the value filters, and the
// question being measured is the composition, not the window length.
var windowedFilterOffsets = []int{30, 90}

// familyTask builds a generated task attributed to a named family. It mirrors
// generatedTask, which is pinned to the founding family's provenance.
func familyTask(seed int64, source, slugKey, sourceID string, category Category, difficulty Difficulty, prompt, query, extract, answerKind string, method []string) Task {
	if strings.TrimSpace(sourceID) == "" {
		sourceID = slugKey
	}
	return Task{
		Category: category, Difficulty: difficulty, Prompt: prompt,
		Slug:              string(category) + "-" + slugKey + "-" + extract,
		Provenance:        Provenance{GeneratorVersion: GeneratorVersion, Source: source, Seed: seed, SourceID: sourceID},
		CapabilityProfile: CapabilityProfile{RoleClass: "user"}, ExpectedStatus: gjagent.StatusAnswered,
		Oracle: &OracleSpec{Query: query, Extract: extract}, Answer: AnswerRule{Kind: answerKind},
		Method:   MethodRule{RequireQueryMatch: method, ForbidFinalizeFromListOnly: answerKind == "number"},
		Behavior: BehaviorRule{RequiredActions: []string{"query_catalog", "execute_graphql"}, ForbiddenActions: []string{"execute_graphql:mutation"}},
	}
}

// columnAggregateMethodPattern requires the graded column and a database-side
// aggregate in one successful query, without dictating how the rows were
// narrowed. A filtered count and a grouped projection are both legitimate ways
// to answer "how many X are in state Y", and a rule that recognised only the
// filter would score the grouped answer as a method failure.
func columnAggregateMethodPattern(column, fn, metric string) string {
	quoted := regexp.QuoteMeta(column)
	return fmt.Sprintf(`(?s)\b%s\b.*%s`, quoted, aggregateMethodPattern(fn, metric))
}

// observedValueColumns returns each table's columns that publish a closed value
// set, in a stable order.
func observedValueColumns(rows []CatalogRow) map[string]map[string][]string {
	out := map[string]map[string][]string{}
	for _, row := range rows {
		values := observedColumnValues(row)
		if len(values) == 0 {
			continue
		}
		sorted := append([]string(nil), values...)
		sort.Strings(sorted)
		if len(sorted) > maxObservedValuesPerColumn {
			sorted = sorted[:maxObservedValuesPerColumn]
		}
		if out[row.TableName] == nil {
			out[row.TableName] = map[string][]string{}
		}
		out[row.TableName][row.ColumnName] = sorted
	}
	return out
}

func sortedColumnNames(values map[string][]string, limit int) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	if limit > 0 && len(names) > limit {
		names = names[:limit]
	}
	return names
}

func tablePrimaryKey(table generatorTable) string {
	if table.PrimaryKey != "" {
		return table.PrimaryKey
	}
	if len(table.Columns) != 0 {
		return table.Columns[0].Name
	}
	return ""
}

// generateFilteredAggregateCandidates asks for counts and metrics restricted to
// a value the column actually holds. The catalog publishes the closed set, so
// the filter names a real state of the business rather than a placeholder — the
// difference between "how many accounts are on the enterprise plan" and a
// question about a plan the schema has never seen.
func generateFilteredAggregateCandidates(snapshot CatalogSnapshot, seed int64) []Task {
	tables := catalogTables(snapshot.Rows)
	observed := observedValueColumns(snapshot.Rows)
	var tasks []Task
	for _, table := range tables {
		byColumn := observed[table.Name]
		if len(byColumn) == 0 {
			continue
		}
		pk := tablePrimaryKey(table)
		if pk == "" {
			continue
		}
		metrics := make([]generatorColumn, 0, maxMetricColumnsPerTable)
		for _, column := range table.Columns {
			if isNumericType(column.Type) && !isIdentifierColumn(table, column) {
				metrics = append(metrics, column)
			}
			if len(metrics) == maxMetricColumnsPerTable {
				break
			}
		}
		columnID := func(name string) string {
			for _, column := range table.Columns {
				if column.Name == name {
					return column.ID
				}
			}
			return table.ID
		}
		for _, name := range sortedColumnNames(byColumn, maxValueColumnsPerTable) {
			for _, value := range byColumn[name] {
				slugKey := fmt.Sprintf("%s:%s:%s", table.ID, name, value)
				tasks = append(tasks, familyTask(seed, "filtered-aggregate", slugKey, columnID(name),
					CategoryAggregate, DifficultyT2,
					fmt.Sprintf("How many %s have a %s of %q?", humanize(table.Name), humanize(name), value),
					fmt.Sprintf("query { %s(where: {%s: {eq: %q}}) { count_%s } }", table.Name, name, value, pk),
					table.Name+".0.count_"+pk, "number",
					[]string{columnAggregateMethodPattern(name, "count", pk)}))
				for _, metric := range metrics {
					if metric.Name == name {
						continue
					}
					for _, fn := range []string{"sum", "avg"} {
						field := fn + "_" + metric.Name
						tasks = append(tasks, familyTask(seed, "filtered-aggregate",
							fmt.Sprintf("%s:%s:%s:%s", table.ID, name, value, field), columnID(metric.Name),
							CategoryAggregate, DifficultyT2,
							fmt.Sprintf("What is the %s %s across %s with a %s of %q?",
								aggregatePhrase(fn), humanize(metric.Name), humanize(table.Name), humanize(name), value),
							fmt.Sprintf("query { %s(where: {%s: {eq: %q}}) { %s } }", table.Name, name, value, field),
							table.Name+".0."+field, "number",
							[]string{columnAggregateMethodPattern(name, fn, metric.Name)}))
					}
				}
			}
		}
	}
	return tasks
}

// generateRelTraversalCandidates asks questions whose answer lives on the other
// side of a relationship, with a count as the objective ground truth. The
// traversal family previously had no oracle of its own, which is why the
// sampler still refuses to backfill it; these give it one.
//
// The method rule asks only for a database-side count. Following a relationship
// in one nested query and resolving it in two steps are both correct, and only
// the answer can tell them apart from a guess.
func generateRelTraversalCandidates(snapshot CatalogSnapshot, seed int64) []Task {
	tables := catalogTables(snapshot.Rows)
	byName := make(map[string]generatorTable, len(tables))
	for _, table := range tables {
		byName[table.Name] = table
	}
	observed := observedValueColumns(snapshot.Rows)
	relationships := catalogRelationships(snapshot.Rows, tables)
	var tasks []Task
	for _, rel := range relationships {
		child, referenced := byName[rel.FromTable], byName[rel.ToTable]
		childPK, parentPK := tablePrimaryKey(child), tablePrimaryKey(referenced)
		if childPK == "" || parentPK == "" {
			continue
		}
		// "Has at least one" is an existence join. The negated form reads the
		// same in English but is an anti-join that also counts rows with no
		// match at all, so the oracle states the positive form directly.
		tasks = append(tasks, familyTask(seed, "rel-traversal", rel.ID+":has-"+rel.FromTable, rel.ID,
			CategoryTraversal, DifficultyT3,
			fmt.Sprintf("How many %s have at least one related %s?", humanize(rel.ToTable), humanize(rel.FromTable)),
			fmt.Sprintf("query { %s(where: {%s: {%s: {is_null: false}}}) { count_%s } }",
				rel.ToTable, rel.FromTable, childPK, parentPK),
			rel.ToTable+".0.count_"+parentPK, "number",
			[]string{aggregateMethodPattern("count", parentPK)}))

		// Counting one side while filtering on the other cannot be answered
		// without crossing the relationship.
		for _, name := range sortedColumnNames(observed[rel.ToTable], maxValueColumnsPerTable) {
			for _, value := range observed[rel.ToTable][name] {
				tasks = append(tasks, familyTask(seed, "rel-traversal",
					fmt.Sprintf("%s:%s:%s", rel.ID, name, value), rel.ID,
					CategoryTraversal, DifficultyT3,
					fmt.Sprintf("How many %s belong to %s with a %s of %q?",
						humanize(rel.FromTable), humanize(rel.ToTable), humanize(name), value),
					fmt.Sprintf("query { %s(where: {%s: {%s: {eq: %q}}}) { count_%s } }",
						rel.FromTable, rel.ToTable, name, value, childPK),
					rel.FromTable+".0.count_"+childPK, "number",
					[]string{aggregateMethodPattern("count", childPK)}))
			}
		}
	}
	return tasks
}

// generateWindowedFilterCandidates composes the two narrowings a real question
// usually carries at once: a period, and a state. The window is anchored to the
// table's own latest date so the question keeps its meaning as data moves.
func generateWindowedFilterCandidates(snapshot CatalogSnapshot, seed int64) []Task {
	tables := catalogTables(snapshot.Rows)
	observed := observedValueColumns(snapshot.Rows)
	var tasks []Task
	for _, table := range tables {
		byColumn := observed[table.Name]
		if len(byColumn) == 0 {
			continue
		}
		pk := tablePrimaryKey(table)
		if pk == "" {
			continue
		}
		var dateColumn string
		for _, column := range table.Columns {
			if isDateColumn(column) {
				dateColumn = column.Name
				break
			}
		}
		if dateColumn == "" {
			continue
		}
		for _, name := range sortedColumnNames(byColumn, 2) {
			values := byColumn[name]
			if len(values) > 2 {
				values = values[:2]
			}
			for _, value := range values {
				for _, days := range windowedFilterOffsets {
					query := fmt.Sprintf("{ %s(where: {%s: {gte: %q}, %s: {eq: %q}}) { count_%s } }",
						table.Name, dateColumn, oracleVariableMarker("from"), name, value, pk)
					task := familyTask(seed, "windowed-filter",
						fmt.Sprintf("%s:%s:%s:%s:%d", table.ID, dateColumn, name, value, days), table.ID,
						CategoryWindow, DifficultyT3,
						fmt.Sprintf("Using the latest recorded %s as the anchor, how many %s with a %s of %q have %s on or after the date exactly %d days before that anchor?",
							dateColumn, humanize(table.Name), humanize(name), value, dateColumn, days),
						query, table.Name+".0.count_"+pk, "number",
						[]string{filteredCountMethodPattern(dateColumn, `gte\s*:`, pk)})
					task.Oracle.AnchorQuery = fmt.Sprintf("query { %s { max_%s } }", table.Name, dateColumn)
					task.Oracle.AnchorExtract = table.Name + ".0.max_" + dateColumn
					task.Oracle.Variables = map[string]any{"from": fmt.Sprintf("{{anchor-%dd}}", days)}
					tasks = append(tasks, task)
				}
			}
		}
	}
	return tasks
}
