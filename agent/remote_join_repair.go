package agent

import (
	"context"
	"fmt"
	"strings"
)

// A join-only remote table is reachable one way and one way only: nested under
// the parent row that carries its key. Models do not discover that route from
// prose. Across two benchmark runs every failing cross-source episode was
// structurally wrong and never wrong about anything else — inverted nesting,
// the parent dropped entirely, a filter placed on the child's own join column —
// and one episode spent sixteen syntactic permutations without ever writing the
// parent as the outer field.
//
// So the repair hands over the query rather than describing it, the same way the
// value guard hands over a corrected write. The model's own filter is grafted in
// when every column it names actually belongs to the parent; otherwise the shape
// arrives with a hole and the real column names to fill it from.
//
// Offering alone was measured and it did not work: across the round-2 A/B the
// repair fired seven times and was executed zero times, and both cross-source
// passes in that run were nested queries the model authored for itself after
// reading the catalog. So a repair whose filter is entirely the model's own —
// every column of it legal on the parent — now executes instead of being handed
// back, on the same reasoning the watch-subscription repair executes: it is the
// unique reading of the caller's own question, and only the route was ever ours
// to fix. Anything less certain is still offered, because choosing which row to
// ask about stays the model's job.

// remoteJoinFilterOperators are the comparison and combinator keys GraphJin's
// where grammar defines. They appear as object keys alongside column names, so
// a graft check that did not subtract them would reject every filter as
// referencing unknown "columns".
var remoteJoinFilterOperators = map[string]bool{
	"eq": true, "neq": true, "ne": true, "gt": true, "gte": true, "lt": true, "lte": true,
	"in": true, "nin": true, "like": true, "nlike": true, "ilike": true, "nilike": true,
	"similar": true, "nsimilar": true, "regex": true, "nregex": true, "iregex": true,
	"has_key": true, "has_key_any": true, "has_key_all": true, "contains": true,
	"contained_in": true, "is_null": true, "is": true, "and": true, "or": true, "not": true,
}

const (
	remoteJoinParentFieldLimit = 4
	remoteJoinChildFieldLimit  = 6
	remoteJoinRepairLimit      = 20
)

// remoteJoinFilterKind reports how much of the offered filter is the model's own
// work, which is what decides whether the repair may execute rather than be
// handed back.
type remoteJoinFilterKind int

const (
	// remoteJoinFilterHole: nothing of the model's filter could be carried
	// across, so the shape arrives with a placeholder and the parent's real
	// column names to fill it from.
	remoteJoinFilterHole remoteJoinFilterKind = iota
	// remoteJoinFilterSalvaged: the filter named no parent column but carried a
	// single string literal the parent's name column can take. The literal is
	// the model's; the column is our inference, so this is offered, not run.
	remoteJoinFilterSalvaged
	// remoteJoinFilterGrafted: every column the model named belongs to the
	// parent, so the corrected query asks the model's own question by the only
	// route it has.
	remoteJoinFilterGrafted
)

// remoteJoinRepairedQuery builds the nested query the intercepted one was
// reaching for. The kind reports how much of the model's own filter survived
// into it, which decides both how much the caller needs to explain and whether
// the caller may execute it outright.
func (r *protocolRuntime) remoteJoinRepairedQuery(ctx context.Context, query, root, parent string) (string, remoteJoinFilterKind) {
	parentColumns := r.observedColumnNames(ctx, parent)
	childColumns := r.observedColumnNames(ctx, root)

	filter, kind := r.remoteJoinFilter(ctx, query, root, parent, parentColumns)
	return fmt.Sprintf("query { %s(where: %s, limit: %d) { %s %s { %s } } }",
		parent, filter, remoteJoinRepairLimit,
		remoteJoinFieldList(parentColumns, remoteJoinParentFieldLimit),
		root,
		remoteJoinFieldList(childColumns, remoteJoinChildFieldLimit),
	), kind
}

// remoteJoinFilter returns the model's own where object when it is legal on the
// parent, then the salvage, and otherwise a hole naming the columns that are.
// Every return is a complete `{...}` value: the where argument is spliced whole
// into the repaired query, and a body without its own braces does not parse.
func (r *protocolRuntime) remoteJoinFilter(ctx context.Context, query, root, parent string, parentColumns []string) (string, remoteJoinFilterKind) {
	hole := "{<filter on " + parent + ">}"
	if len(parentColumns) != 0 {
		hole = "{<filter on " + parent + ", columns: " + strings.Join(remoteJoinCapped(parentColumns, 6), ", ") + ">}"
	}
	if len(parentColumns) == 0 {
		// Without the parent's columns there is nothing to validate a graft
		// against, and grafting unchecked is how a filter on the child's own
		// join column would survive into the repair.
		return hole, remoteJoinFilterHole
	}
	body, ok := remoteJoinWhereBody(query, root)
	if !ok {
		return hole, remoteJoinFilterHole
	}
	// A filter carrying variables cannot be offered: the repair travels as a
	// bare query string with no channel for the values it would reference.
	if strings.Contains(body, "$") {
		return hole, remoteJoinFilterHole
	}
	keys := map[string]bool{}
	collectGraphQLObjectKeys(graphQLStructure(body), keys)
	named := 0
	allowed := map[string]bool{}
	for _, column := range parentColumns {
		allowed[strings.ToLower(strings.TrimSpace(column))] = true
	}
	for key := range keys {
		if remoteJoinFilterOperators[key] {
			continue
		}
		if !allowed[key] {
			return remoteJoinSalvagedFilter(body, parentColumns, hole)
		}
		named++
	}
	if named == 0 {
		return remoteJoinSalvagedFilter(body, parentColumns, hole)
	}
	// graphQLNamedObject returns an object's INTERIOR — the span between the
	// braces, not including them. Round 2 spliced that interior straight into
	// `where: %s` and shipped `where: name: {eq: "..."}` to seven episodes as a
	// query to "execute exactly as given". None of them could.
	return "{" + body + "}", remoteJoinFilterGrafted
}

// remoteJoinSalvagedFilter rescues the one thing a rejected filter still knows:
// which row the model is asking about. Every miss observed across two benchmark
// runs — client, account_name, executive_owner — carried the account's name as
// its only literal and put it under a column the parent does not have. A lone
// string literal plus a name column on the parent is therefore a filter the
// model already wrote, moved to the column it belongs in. The value stays the
// model's, so the caller offers this rather than running it.
func remoteJoinSalvagedFilter(body string, parentColumns []string, hole string) (string, remoteJoinFilterKind) {
	name := ""
	for _, column := range parentColumns {
		if strings.EqualFold(strings.TrimSpace(column), "name") {
			name = strings.TrimSpace(column)
			break
		}
	}
	if name == "" {
		return hole, remoteJoinFilterHole
	}
	literals := remoteJoinStringLiterals(body)
	if len(literals) != 1 || strings.TrimSpace(literals[0]) == "" {
		return hole, remoteJoinFilterHole
	}
	return fmt.Sprintf("{%s: {eq: %q}}", name, literals[0]), remoteJoinFilterSalvaged
}

// remoteJoinStringLiterals returns every quoted string value in a where body, in
// order. Exactly one of them is what makes a salvage unambiguous.
func remoteJoinStringLiterals(body string) []string {
	var out []string
	for i := 0; i < len(body); i++ {
		if body[i] != '"' {
			continue
		}
		value, next := graphQLStringLiteral(body, i)
		if next <= i {
			break
		}
		out = append(out, value)
		i = next - 1
	}
	return out
}

// remoteJoinWhereBody extracts the intercepted query's where object, spans and
// all. The whole object is spliced rather than rebuilt field by field: a where
// clause nests, and the span helpers that read individual string fields
// deliberately skip nested objects.
func remoteJoinWhereBody(query, root string) (string, bool) {
	clean := graphQLStructure(query)
	blocks := mutationInputBlocks(clean, root, "where")
	if len(blocks) == 0 {
		return "", false
	}
	span := blocks[0]
	if span[0] < 0 || span[1] > len(query) || span[0] >= span[1] {
		return "", false
	}
	body := strings.TrimSpace(query[span[0]:span[1]])
	if body == "" {
		return "", false
	}
	return body, true
}

// remoteJoinFieldList renders a selection set, preferring an id-like column
// first so the offered query returns something the model can join back to.
func remoteJoinFieldList(columns []string, limit int) string {
	if len(columns) == 0 {
		return "<fields>"
	}
	ordered := make([]string, 0, len(columns))
	for _, column := range columns {
		if strings.EqualFold(strings.TrimSpace(column), "id") {
			ordered = append(ordered, column)
		}
	}
	for _, column := range columns {
		if !strings.EqualFold(strings.TrimSpace(column), "id") {
			ordered = append(ordered, column)
		}
	}
	return strings.Join(remoteJoinCapped(ordered, limit), " ")
}

func remoteJoinCapped(values []string, limit int) []string {
	if limit <= 0 || len(values) <= limit {
		return values
	}
	return values[:limit]
}

// remoteJoinRepairOffered records which roots have already been handed the full
// explanation, so a model looping on the same closed route is not re-taught the
// same paragraph on every attempt. The repaired query itself is rebuilt each
// time: the model's filter may improve between attempts even while its route
// does not.
func (s *discoveryState) remoteJoinRepairSeen(root string) bool {
	if s == nil {
		return false
	}
	key := strings.ToLower(strings.TrimSpace(root))
	if s.remoteJoinRepairOffered[key] {
		return true
	}
	if s.remoteJoinRepairOffered == nil {
		s.remoteJoinRepairOffered = map[string]bool{}
	}
	s.remoteJoinRepairOffered[key] = true
	return false
}
