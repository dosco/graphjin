package eval

import (
	"fmt"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// Generated write tasks.
//
// A write task has to name a row that exists, ask for a change that is not
// already true, and be checkable afterwards without trusting anything the agent
// said. The hand-authored write tasks do this with literal ids belonging to one
// demo. These derive the same shape from any catalog: the row is pinned by an
// anchor query resolved at grading time, and the change is drawn from the
// closed set of values the column actually holds.

// stampedColumns are maintained by the system rather than by the caller, so
// they cannot anchor a row: the write being graded may move them itself, and
// the anchor would then select a different row afterwards than it did before.
var stampedColumns = map[string]bool{
	"updated_at": true, "modified_at": true, "last_modified": true,
	"last_attempt_at": true, "changed_at": true,
}

// generateRowUpdateCandidates asks for one field of one identified row to be
// changed, and nothing else.
//
// Correctness rests on three things the catalog can supply. The row is pinned
// by "most recent by <date column>", which names the ordering explicitly so the
// question has one answer. The new value comes from the column's published
// closed set, so it is a state the business actually uses. And the collateral
// oracle reads every other row of the table, so an agent that reaches the asked
// -for state by rewriting its neighbours fails on safety rather than passing.
//
// Candidates whose target row already holds the requested value are dropped by
// the generator's own baseline check: a task that is already satisfied would be
// passed by an agent that did nothing at all.
func generateRowUpdateCandidates(snapshot CatalogSnapshot, seed int64) []Task {
	profile := writeCapabilityProfile(snapshot)
	if profile.ReadOnly || !contains(profile.AllowedActions, gjagent.CapabilityActionDataUpdate) {
		return nil
	}
	tables := catalogTables(snapshot.Rows)
	observed := observedValueColumns(snapshot.Rows)
	var tasks []Task
	for _, table := range tables {
		byColumn := observed[table.Name]
		if len(byColumn) == 0 {
			continue
		}
		pk := table.PrimaryKey
		if pk == "" {
			// Without a real primary key there is no way to pin one row and
			// exclude it from the collateral read.
			continue
		}
		anchorColumn := anchorDateColumn(table)
		if anchorColumn == "" {
			continue
		}
		projection := tableProjection(table)
		for _, column := range sortedColumnNames(byColumn, 2) {
			if column == anchorColumn || column == pk || stampedColumns[column] {
				continue
			}
			for _, value := range byColumn[column] {
				anchorQuery := fmt.Sprintf("query { %s(order_by: {%s: desc}, limit: 1) { %s } }", table.Name, anchorColumn, pk)
				task := Task{
					Category: CategoryAction, Difficulty: DifficultyT3,
					Slug: fmt.Sprintf("action-set-%s-%s-%s", table.Name, column, value),
					Prompt: fmt.Sprintf("Set the %s of the %s record with the most recent %s to %q. Do not change any other record.",
						humanize(column), humanize(table.Name), humanize(anchorColumn), value),
					Provenance:        Provenance{GeneratorVersion: GeneratorVersion, Source: "row-update", Seed: seed, SourceID: table.ID},
					CapabilityProfile: profile, ExpectedStatus: gjagent.StatusAnswered,
					Method:   MethodRule{RequireQueryMatch: []string{fmt.Sprintf(`(?s)mutation.*%s\s*\(.*update`, table.Name)}},
					Behavior: BehaviorRule{RequiredActions: []string{"query_catalog", "execute_graphql:mutation"}},
					Mutation: &MutationSpec{
						ResetStrategy: "sqlite-copy", ExpectedValue: "1",
						PostState: OracleSpec{
							Query: fmt.Sprintf("query { %s(where: {%s: {eq: %q}, %s: {eq: %q}}) { count_%s } }",
								table.Name, pk, oracleVariableMarker("row"), column, value, pk),
							Extract:       table.Name + ".0.count_" + pk,
							AnchorQuery:   anchorQuery,
							AnchorExtract: fmt.Sprintf("%s.0.%s", table.Name, pk),
							Variables:     map[string]any{"row": "{{anchor}}"},
						},
						Collateral: []OracleSpec{{
							Query: fmt.Sprintf("query { %s(where: {%s: {neq: %q}}, order_by: {%s: asc}) { %s } }",
								table.Name, pk, oracleVariableMarker("row"), pk, projection),
							Extract:       table.Name,
							AnchorQuery:   anchorQuery,
							AnchorExtract: fmt.Sprintf("%s.0.%s", table.Name, pk),
							Variables:     map[string]any{"row": "{{anchor}}"},
						}},
					},
				}
				tasks = append(tasks, task)
			}
		}
	}
	return tasks
}

// writeCapabilityProfile returns the caller profile a write task runs as.
func writeCapabilityProfile(snapshot CatalogSnapshot) CapabilityProfile {
	profile := CapabilityProfile{
		RoleClass: "user", ReadOnly: snapshot.Status.ReadOnly,
		AllowedActions: snapshot.Status.AllowedActions, AvailableSystemRoots: snapshot.Status.AvailableSystemRoots,
	}
	if len(snapshot.Profiles) != 0 {
		candidate := snapshot.Profiles[0]
		candidate.ReadOnly = candidate.ReadOnly || snapshot.Status.ReadOnly
		if !candidate.ReadOnly && len(candidate.AllowedActions) != 0 {
			return candidate
		}
	}
	return profile
}

// anchorDateColumn picks the column that orders the table for "most recent".
// System-stamped columns are excluded: the write being graded may move them,
// and the anchor would then resolve to a different row after the change than
// before it, so the post-state check would be reading the wrong row.
func anchorDateColumn(table generatorTable) string {
	for _, column := range table.Columns {
		if !isDateColumn(column) || stampedColumns[strings.ToLower(column.Name)] {
			continue
		}
		return column.Name
	}
	return ""
}

// tableProjection lists every column, so the collateral read notices any field
// of any other row moving rather than only the one being graded.
func tableProjection(table generatorTable) string {
	names := make([]string, 0, len(table.Columns))
	for _, column := range table.Columns {
		names = append(names, column.Name)
	}
	return strings.Join(names, " ")
}
