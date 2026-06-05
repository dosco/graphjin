package catalog

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const maxQueryLimit = 500

type rankedCard struct {
	card  Card
	match Match
}

type weightedText struct {
	field  string
	text   string
	weight float64
}

type searchIndex struct {
	docs map[string]searchDoc
}

type searchDoc struct {
	fields      []weightedText
	docTokens   []string
	docTokenSet map[string]struct{}
}

// Query searches and filters catalog cards using GraphJin-shaped arguments:
// search for ranked full text, where for precise filtering, order_by for
// deterministic sorting, and limit for result size.
func (s *Snapshot) Query(q Query) (QueryResult, error) {
	if s == nil {
		return QueryResult{}, nil
	}

	limit := q.Limit
	if limit <= 0 || limit > maxQueryLimit {
		limit = 100
	}

	where := combineQueryWhere(q)
	search := strings.TrimSpace(q.Search)
	idx := s.search
	if idx == nil {
		idx = newSearchIndex(s)
	}

	items := make([]rankedCard, 0, min(limit, len(s.Cards)))
	for _, card := range s.Cards {
		ok, err := cardMatchesWhere(card, where)
		if err != nil {
			return QueryResult{}, err
		}
		if !ok {
			continue
		}

		var match Match
		if search != "" {
			var matched bool
			match, matched = idx.scoreCard(card, search)
			if !matched {
				continue
			}
		}
		items = append(items, rankedCard{card: card, match: match})
	}

	if err := sortRankedCards(items, q.OrderBy, search != ""); err != nil {
		return QueryResult{}, err
	}
	if len(items) > limit {
		items = items[:limit]
	}

	result := QueryResult{Cards: make([]Card, 0, len(items))}
	if q.Explain && search != "" {
		result.Matches = make(map[string]Match, len(items))
	}
	for _, item := range items {
		result.Cards = append(result.Cards, item.card)
		if result.Matches != nil {
			result.Matches[item.card.ID] = item.match
		}
	}
	return result, nil
}

func newSearchIndex(s *Snapshot) *searchIndex {
	idx := &searchIndex{docs: make(map[string]searchDoc, len(s.Cards))}
	details := detailsByCard(s.Details)
	for _, card := range s.Cards {
		fields := cardSearchFields(card, details[card.ID])
		docTokens := make([]string, 0, 64)
		docTokenSet := make(map[string]struct{})
		for _, field := range fields {
			for _, token := range tokenizeCatalogText(field.text) {
				docTokens = append(docTokens, token)
				docTokenSet[token] = struct{}{}
			}
		}
		idx.docs[card.ID] = searchDoc{
			fields:      fields,
			docTokens:   docTokens,
			docTokenSet: docTokenSet,
		}
	}
	return idx
}

func combineQueryWhere(q Query) map[string]any {
	var clauses []any
	if len(q.Where) != 0 {
		clauses = append(clauses, q.Where)
	}

	shorthand := make(map[string]any, 5)
	addShorthandClause(shorthand, "kind", q.Kind)
	addShorthandClause(shorthand, "database_name", q.Database)
	addShorthandClause(shorthand, "schema_name", q.Schema)
	addShorthandClause(shorthand, "table_name", q.Table)
	addShorthandClause(shorthand, "column_name", q.Column)
	if len(shorthand) != 0 {
		clauses = append(clauses, shorthand)
	}

	switch len(clauses) {
	case 0:
		return nil
	case 1:
		if where, ok := clauses[0].(map[string]any); ok {
			return where
		}
		return map[string]any{"and": clauses}
	default:
		return map[string]any{"and": clauses}
	}
}

func addShorthandClause(where map[string]any, field, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	where[field] = map[string]any{"eq": value}
}

func cardMatchesWhere(card Card, where map[string]any) (bool, error) {
	if len(where) == 0 {
		return true, nil
	}
	return evalWhere(card, where)
}

func evalWhere(card Card, clause any) (bool, error) {
	if clause == nil {
		return true, nil
	}

	where, ok := asStringAnyMap(clause)
	if !ok {
		return false, fmt.Errorf("catalog where clause must be an object, got %T", clause)
	}
	if len(where) == 0 {
		return true, nil
	}

	keys := sortedMapKeys(where)
	for _, key := range keys {
		value := where[key]
		switch strings.ToLower(key) {
		case "and":
			ok, err := evalLogicalGroup(card, value, true)
			if err != nil || !ok {
				return ok, err
			}
		case "or":
			ok, err := evalLogicalGroup(card, value, false)
			if err != nil || !ok {
				return ok, err
			}
		case "not":
			ok, err := evalWhere(card, value)
			if err != nil {
				return false, err
			}
			if ok {
				return false, nil
			}
		default:
			ok, err := evalFieldWhere(card, key, value)
			if err != nil || !ok {
				return ok, err
			}
		}
	}
	return true, nil
}

func evalLogicalGroup(card Card, value any, requireAll bool) (bool, error) {
	switch v := value.(type) {
	case []any:
		if len(v) == 0 {
			return requireAll, nil
		}
		for _, item := range v {
			ok, err := evalWhere(card, item)
			if err != nil {
				return false, err
			}
			if requireAll && !ok {
				return false, nil
			}
			if !requireAll && ok {
				return true, nil
			}
		}
		return requireAll, nil
	default:
		where, ok := asStringAnyMap(v)
		if !ok {
			return false, fmt.Errorf("catalog logical where group must be an object or array, got %T", value)
		}
		if requireAll {
			return evalWhere(card, where)
		}
		for _, key := range sortedMapKeys(where) {
			ok, err := evalWhere(card, map[string]any{key: where[key]})
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
}

func evalFieldWhere(card Card, field string, expr any) (bool, error) {
	value, ok := catalogFieldValue(card, field)
	if !ok {
		return false, fmt.Errorf("unsupported catalog where field %q", field)
	}

	ops, ok := asStringAnyMap(expr)
	if !ok {
		return compareCatalogField("eq", value, expr)
	}
	if len(ops) == 0 {
		return true, nil
	}

	for _, op := range sortedMapKeys(ops) {
		ok, err := compareCatalogField(op, value, ops[op])
		if err != nil || !ok {
			return ok, err
		}
	}
	return true, nil
}

func catalogFieldValue(card Card, field string) (any, bool) {
	switch normalizeCatalogField(field) {
	case "id":
		return card.ID, true
	case "kind":
		return card.Kind, true
	case "title":
		return card.Title, true
	case "summary":
		return card.Summary, true
	case "database_name":
		return card.DatabaseName, true
	case "schema_name":
		return card.SchemaName, true
	case "table_name":
		return card.TableName, true
	case "column_name":
		return card.ColumnName, true
	case "source":
		return card.Source, true
	case "source_kind":
		return card.SourceKind, true
	case "owner_source":
		return card.OwnerSource, true
	case "owner_sources_json":
		return card.OwnerSourcesJSON, true
	case "risk_level":
		return card.RiskLevel, true
	case "confidence":
		return card.Confidence, true
	case "sensitive":
		return card.Sensitive, true
	case "sensitivity":
		return card.Sensitivity, true
	case "detail_ref":
		return card.DetailRef, true
	case "created_at":
		return card.CreatedAt, true
	case "updated_at":
		return card.UpdatedAt, true
	default:
		return nil, false
	}
}

func normalizeCatalogField(field string) string {
	field = strings.TrimSpace(strings.ToLower(field))
	switch field {
	case "database":
		return "database_name"
	case "schema":
		return "schema_name"
	case "table":
		return "table_name"
	case "column":
		return "column_name"
	case "created_on":
		return "created_at"
	case "updated_on":
		return "updated_at"
	default:
		return field
	}
}

func compareCatalogField(op string, value, want any) (bool, error) {
	op = strings.ToLower(strings.TrimSpace(op))
	switch op {
	case "eq":
		return equalCatalogValues(value, want), nil
	case "neq":
		return !equalCatalogValues(value, want), nil
	case "in", "nin":
		list, ok := asAnyList(want)
		if !ok {
			return false, fmt.Errorf("catalog where.%s requires an array", op)
		}
		found := false
		for _, item := range list {
			if equalCatalogValues(value, item) {
				found = true
				break
			}
		}
		if op == "nin" {
			return !found, nil
		}
		return found, nil
	case "like", "ilike":
		return sqlLikeMatch(catalogString(want), catalogString(value), op == "ilike")
	case "regex", "iregex":
		pattern := catalogString(want)
		if op == "iregex" {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false, fmt.Errorf("invalid catalog regex: %w", err)
		}
		return re.MatchString(catalogString(value)), nil
	case "contains":
		haystack := strings.ToLower(catalogString(value))
		needle := strings.ToLower(catalogString(want))
		return strings.Contains(haystack, needle), nil
	case "is_null":
		wantNull, ok := catalogBool(want)
		if !ok {
			return false, fmt.Errorf("catalog where.is_null requires a boolean")
		}
		isNull := isCatalogNull(value)
		return isNull == wantNull, nil
	default:
		return false, fmt.Errorf("unsupported catalog where operator %q", op)
	}
}

func equalCatalogValues(left, right any) bool {
	switch l := left.(type) {
	case bool:
		r, ok := catalogBool(right)
		return ok && l == r
	default:
		return strings.EqualFold(catalogString(left), catalogString(right))
	}
}

func catalogString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(x)
	}
}

func catalogBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true":
			return true, true
		case "false":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func isCatalogNull(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

func sqlLikeMatch(pattern, value string, insensitive bool) (bool, error) {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '%':
			b.WriteString(".*")
		case '_':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")

	if insensitive {
		value = strings.ToLower(value)
		pattern = strings.ToLower(b.String())
	} else {
		pattern = b.String()
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(value), nil
}

func sortRankedCards(items []rankedCard, orderBy map[string]string, hasSearch bool) error {
	if len(orderBy) != 0 {
		for field := range orderBy {
			if normalizeCatalogField(field) == "score" && !hasSearch {
				return fmt.Errorf("catalog order_by.score is only available when search is present")
			}
			if normalizeCatalogField(field) != "score" {
				if _, ok := catalogFieldValue(Card{}, field); !ok {
					return fmt.Errorf("unsupported catalog order_by field %q", field)
				}
			}
		}
	}

	orderFields := sortedMapKeysString(orderBy)
	sort.SliceStable(items, func(i, j int) bool {
		if len(orderFields) == 0 {
			if hasSearch && items[i].match.Score != items[j].match.Score {
				return items[i].match.Score > items[j].match.Score
			}
			return items[i].card.ID < items[j].card.ID
		}

		for _, field := range orderFields {
			dir := strings.ToLower(strings.TrimSpace(orderBy[field]))
			if dir == "" {
				dir = "asc"
			}
			cmp := compareOrderField(items[i], items[j], field)
			if cmp == 0 {
				continue
			}
			if dir == "desc" {
				return cmp > 0
			}
			return cmp < 0
		}
		return items[i].card.ID < items[j].card.ID
	})
	return nil
}

func compareOrderField(left, right rankedCard, field string) int {
	normalizedField := normalizeCatalogField(field)
	if normalizedField == "score" {
		switch {
		case left.match.Score < right.match.Score:
			return -1
		case left.match.Score > right.match.Score:
			return 1
		default:
			return 0
		}
	}
	lv, _ := catalogFieldValue(left.card, field)
	rv, _ := catalogFieldValue(right.card, field)
	if normalizedField == "created_at" || normalizedField == "updated_at" {
		if cmp, ok := compareCatalogTimes(catalogString(lv), catalogString(rv)); ok {
			return cmp
		}
	}
	if lb, ok := lv.(bool); ok {
		rb, _ := rv.(bool)
		switch {
		case !lb && rb:
			return -1
		case lb && !rb:
			return 1
		default:
			return 0
		}
	}
	return strings.Compare(strings.ToLower(catalogString(lv)), strings.ToLower(catalogString(rv)))
}

func compareCatalogTimes(left, right string) (int, bool) {
	lt, lok := parseCatalogTime(left)
	rt, rok := parseCatalogTime(right)
	switch {
	case lok && rok:
		switch {
		case lt.Before(rt):
			return -1, true
		case lt.After(rt):
			return 1, true
		default:
			return 0, true
		}
	case lok:
		return 1, true
	case rok:
		return -1, true
	default:
		return 0, false
	}
}

func parseCatalogTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func detailsByCard(details []CardDetail) map[string]string {
	out := make(map[string]string)
	for _, detail := range details {
		// DataJSON may contain redacted structured values. The search index stays
		// conservative and indexes only section/content labels plus card examples.
		out[detail.CardID] = strings.TrimSpace(out[detail.CardID] + " " + detail.Section + " " + detail.Content)
	}
	return out
}

func (idx *searchIndex) scoreCard(card Card, search string) (Match, bool) {
	terms := uniqueTokens(tokenizeCatalogText(search))
	if len(terms) == 0 {
		return Match{}, false
	}

	doc := idx.docs[card.ID]
	queryPhrase := normalizePhrase(search)
	matchedFields := make(map[string]struct{})
	matchedTerms := make(map[string]struct{})
	signals := make(map[string]struct{})

	var score float64
	for _, field := range doc.fields {
		if strings.TrimSpace(field.text) == "" {
			continue
		}
		fieldText := strings.ToLower(field.text)
		fieldPhrase := normalizePhrase(field.text)
		fieldTokens := tokenizeCatalogText(field.text)
		fieldTokenSet := tokenSet(fieldTokens)

		if queryPhrase != "" && strings.Contains(fieldPhrase, queryPhrase) {
			score += 12 * field.weight
			matchedFields[field.field] = struct{}{}
			signals["phrase"] = struct{}{}
		}
		if strings.EqualFold(strings.TrimSpace(field.text), strings.TrimSpace(search)) {
			score += 15 * field.weight
			matchedFields[field.field] = struct{}{}
			signals["exact"] = struct{}{}
		}
		if strings.Contains(fieldText, strings.ToLower(search)) {
			score += 8 * field.weight
			matchedFields[field.field] = struct{}{}
			signals["substring"] = struct{}{}
		}

		for _, term := range terms {
			if _, ok := fieldTokenSet[term]; ok {
				score += 3 * field.weight
				matchedFields[field.field] = struct{}{}
				matchedTerms[term] = struct{}{}
				continue
			}
			if len(term) >= 3 && strings.Contains(fieldPhrase, term) {
				score += field.weight
				matchedFields[field.field] = struct{}{}
				matchedTerms[term] = struct{}{}
			}
		}
	}

	if boost, signal := exactIdentifierBoost(card, terms, search); boost != 0 {
		score += boost
		signals[signal] = struct{}{}
		matchedFields["title"] = struct{}{}
	}
	if boost, signal := proximityBoost(doc.docTokens, terms); boost != 0 {
		score += boost
		signals[signal] = struct{}{}
	}
	if boost, signal := intentBoost(card, terms); boost != 0 {
		score += boost
		signals[signal] = struct{}{}
		matchedFields["generated_tags"] = struct{}{}
	}
	if boost := fuzzyBoost(terms, doc.docTokenSet, matchedTerms); boost != 0 {
		score += boost
		signals["fuzzy typo recovery"] = struct{}{}
		matchedFields["generated_tags"] = struct{}{}
	}

	if score <= 0 {
		return Match{}, false
	}

	return Match{
		Score:         math.Round(score*100) / 100,
		MatchedFields: sortedSet(matchedFields),
		MatchedTerms:  sortedSet(matchedTerms),
		Why:           matchWhy(signals, matchedFields),
	}, true
}

func cardSearchFields(card Card, detailText string) []weightedText {
	names := strings.Join([]string{card.DatabaseName, card.SchemaName, card.TableName, card.ColumnName}, " ")
	return []weightedText{
		{field: "title", text: card.Title, weight: 10},
		{field: "id", text: card.ID, weight: 8},
		{field: "names", text: names, weight: 8},
		{field: "kind", text: card.Kind, weight: 5},
		{field: "summary", text: card.Summary, weight: 5},
		{field: "examples", text: card.ExamplesJSON, weight: 3},
		{field: "details", text: detailText, weight: 2},
		{field: "source", text: card.Source, weight: 2},
		{field: "source_kind", text: card.SourceKind, weight: 2},
		{field: "owner_source", text: card.OwnerSource, weight: 2},
		{field: "owner_sources", text: card.OwnerSourcesJSON, weight: 1},
		{field: "created_at", text: card.CreatedAt, weight: 1},
		{field: "updated_at", text: card.UpdatedAt, weight: 1},
		{field: "generated_tags", text: strings.Join(generatedSearchTags(card), " "), weight: 6},
	}
}

func exactIdentifierBoost(card Card, terms []string, search string) (float64, string) {
	search = strings.ToLower(strings.TrimSpace(search))
	if search == "" {
		return 0, ""
	}
	switch {
	case strings.EqualFold(card.Title, search), strings.EqualFold(card.ID, search):
		return 120, "exact identifier"
	case containsTerm(terms, strings.ToLower(card.TableName)), containsTerm(terms, strings.ToLower(card.ColumnName)):
		return 30, "identifier"
	default:
		return 0, ""
	}
}

func proximityBoost(docTokens, terms []string) (float64, string) {
	if len(terms) < 2 || len(docTokens) == 0 {
		return 0, ""
	}

	first, last := -1, -1
	for _, term := range terms {
		pos := -1
		for i, token := range docTokens {
			if token == term {
				pos = i
				break
			}
		}
		if pos == -1 {
			return 0, ""
		}
		if first == -1 || pos < first {
			first = pos
		}
		if pos > last {
			last = pos
		}
	}

	span := last - first + 1
	switch {
	case span <= len(terms)+2:
		return 45, "close phrase"
	case span <= len(terms)*4:
		return 20, "near terms"
	default:
		return 0, ""
	}
}

func intentBoost(card Card, terms []string) (float64, string) {
	termSet := tokenSet(terms)
	switch {
	case hasAnyTerm(termSet, "join", "joins", "joined", "path", "paths", "relationship", "relationships", "foreign", "fk", "connect", "connected", "link"):
		if card.Kind == "relationship" {
			return 80, "relationship intent"
		}
	case hasAnyTerm(termSet, "window", "windows", "running", "moving", "rank", "ranking", "dense", "row", "number", "total", "revenue", "metric", "analytics", "partition", "over", "trend"):
		if card.Kind == "directive" || card.Kind == "query_pattern" || card.Kind == "deprecated_feature" {
			text := strings.ToLower(card.ID + " " + card.Title + " " + card.Summary + " " + strings.Join(generatedSearchTags(card), " "))
			if analyticsIntentApplies(termSet, text) {
				return 55, "analytics intent"
			}
		}
	case hasAnyTerm(termSet,
		"permission", "permissions", "role", "roles", "blocked", "block", "deny", "denied", "hide", "hidden",
		"policy", "config", "configuration", "secret", "token", "password", "env", "environment",
		"tenant", "account", "namespace", "org", "organization", "workspace", "claim", "claims", "jwt",
		"admin", "public", "readonly", "read", "write", "delete", "artifact", "artifacts", "audit",
		"runtime", "security", "access", "identity", "root", "roots", "source", "sources", "migration", "migrate"):
		if card.Kind == "config_recipe" {
			if hasAnyTerm(termSet, "add", "enable", "migrate", "migration", "block", "blocked", "allow", "set", "make", "lock", "deny", "hide", "admin") {
				return 95, "config recipe action intent"
			}
			return 70, "config recipe intent"
		}
		if card.Kind == "config" || card.Kind == "capability" || card.Kind == "system_capability" || card.Kind == "help" {
			return 45, "config/policy intent"
		}
	case hasAnyTerm(termSet, "workflow", "workflows", "automation", "agent", "runtime"):
		if card.Kind == "workflow" {
			return 75, "workflow intent"
		}
		if card.Kind == "capability" {
			return 35, "workflow/capability intent"
		}
	}
	return 0, ""
}

func analyticsIntentApplies(terms map[string]struct{}, cardText string) bool {
	specific := map[string]string{
		"@running": "running",
		"running":  "running",
		"@moving":  "moving",
		"moving":   "moving",
		"@rank":    "rank",
		"rank":     "rank",
		"ranking":  "rank",
		"dense":    "rank",
	}
	for term, required := range specific {
		if _, ok := terms[term]; ok {
			return strings.Contains(cardText, required)
		}
	}
	if hasAnyTerm(terms, "window", "windows", "analytics", "metric", "revenue", "total", "partition", "over", "trend", "aggregate") {
		return strings.Contains(cardText, "analytic") ||
			strings.Contains(cardText, "window") ||
			strings.Contains(cardText, "running") ||
			strings.Contains(cardText, "moving") ||
			strings.Contains(cardText, "rank") ||
			strings.Contains(cardText, "aggregate")
	}
	return false
}

func fuzzyBoost(terms []string, docTokens map[string]struct{}, matchedTerms map[string]struct{}) float64 {
	var score float64
	for _, term := range terms {
		if len(term) < 5 {
			continue
		}
		if _, ok := matchedTerms[term]; ok {
			continue
		}
		threshold := 1
		if len(term) >= 8 {
			threshold = 2
		}
		for token := range docTokens {
			if len(token) < 5 || absInt(len(token)-len(term)) > threshold {
				continue
			}
			if levenshteinWithin(term, token, threshold) {
				score += 8
				matchedTerms[term] = struct{}{}
				break
			}
		}
	}
	return score
}

func generatedSearchTags(card Card) []string {
	tags := []string{card.Kind}
	switch card.Kind {
	case "relationship":
		tags = append(tags, "join", "joins", "path", "relationship", "foreign_key", "fk", "references", "nested", "selector", "connect", "through")
	case "directive":
		tags = append(tags, "graphql", "dsl", "syntax", "directive")
	case "operator_set":
		tags = append(tags, "where", "filter", "operator", "predicate", "validation")
	case "query_pattern":
		tags = append(tags, "query", "pattern", "graphql", "dsl", "analytics", "metric")
	case "mutation_pattern":
		tags = append(tags, "mutation", "insert", "update", "upsert", "delete", "write")
	case "deprecated_feature":
		tags = append(tags, "deprecated", "migration", "replacement", "mistake")
	case "config":
		tags = append(tags, "config", "configuration", "policy", "permission", "permissions", "role", "blocked", "redacted", "secret", "token", "environment", "production")
	case "config_recipe":
		tags = append(tags,
			"config", "configuration", "recipe", "policy", "permission", "permissions", "security",
			"role", "roles", "jwt", "claim", "claims", "identity", "tenant", "account", "namespace",
			"org", "workspace", "admin", "public", "blocked", "block", "deny", "hide", "readonly",
			"read", "write", "delete", "access", "source", "sources", "root", "roots", "gj_catalog",
			"gj_artifacts", "gj_workflow", "gj_workflow_execution", "gj_runtime", "gj_security", "gj_config",
			"artifact", "artifacts", "audit", "migration", "migrate", "legacy", "roles_tables")
	case "capability":
		tags = append(tags, "capability", "tool", "mcp", "action", "workflow", "validate", "execute", "repair")
	case "workflow":
		tags = append(tags, "workflow", "workflows", "automation", "agent", "runtime", "javascript", "goja", "orchestration", "reusable")
	case "help":
		tags = append(tags, "help", "guide", "bootstrap", "discovery", "graphql", "catalog", "mcp")
	case "saved_query":
		tags = append(tags, "saved", "query", "allowlist", "allow_list", "execute_saved_query", "variables")
	case "table":
		tags = append(tags, "table", "dataset", "records", "rows", "schema", "sample", "profile", "columns")
	case "column":
		tags = append(tags, "column", "field", "where", "filter", "type", "value")
		if card.Sensitive {
			tags = append(tags, "sensitive", "pii", "secret", "token", "redacted")
		}
	}

	text := strings.ToLower(card.ID + " " + card.Title + " " + card.Summary)
	if strings.Contains(text, "running") || strings.Contains(text, "moving") || strings.Contains(text, "rank") || strings.Contains(text, "window") || strings.Contains(text, "aggregate") {
		tags = append(tags, "analytics", "window", "over", "partition", "order_by", "reporting", "metric", "trend")
	}
	if strings.Contains(text, "running") {
		tags = append(tags, "running", "running_total")
	}
	if strings.Contains(text, "moving") {
		tags = append(tags, "moving", "moving_average", "trailing")
	}
	if strings.Contains(text, "rank") {
		tags = append(tags, "rank", "ranking")
	}
	if strings.Contains(text, "sample") || strings.Contains(text, "profile") {
		tags = append(tags, "sample", "profile", "on_demand")
	}
	return tags
}

func tokenizeCatalogText(text string) []string {
	text = splitCamelCase(text)
	var tokens []string
	for _, part := range strings.FieldsFunc(text, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '@')
	}) {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			continue
		}
		tokens = append(tokens, part)
		if strings.HasPrefix(part, "@") && len(part) > 1 {
			tokens = append(tokens, strings.TrimPrefix(part, "@"))
		}
		collapsed := collapseToken(part)
		if collapsed != "" && collapsed != part {
			tokens = append(tokens, collapsed)
		}
	}
	return tokens
}

func splitCamelCase(text string) string {
	var b strings.Builder
	var prev rune
	for i, r := range text {
		if i > 0 && unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)) {
			b.WriteRune(' ')
		}
		b.WriteRune(r)
		prev = r
	}
	return b.String()
}

func collapseToken(token string) string {
	var b strings.Builder
	for _, r := range token {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '@' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizePhrase(text string) string {
	return strings.Join(tokenizeCatalogText(text), " ")
}

func matchWhy(signals, matchedFields map[string]struct{}) string {
	parts := sortedSet(signals)
	if len(parts) == 0 {
		parts = append(parts, "text match")
	}
	fields := sortedSet(matchedFields)
	if len(fields) != 0 {
		parts = append(parts, "fields: "+strings.Join(fields, ", "))
	}
	if len(parts) > 4 {
		parts = parts[:4]
	}
	return strings.Join(parts, "; ")
}

func asStringAnyMap(v any) (map[string]any, bool) {
	switch x := v.(type) {
	case map[string]any:
		return x, true
	case map[string]string:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = v
		}
		return out, true
	default:
		return nil, false
	}
}

func asAnyList(v any) ([]any, bool) {
	switch x := v.(type) {
	case []any:
		return x, true
	case []string:
		out := make([]any, len(x))
		for i, v := range x {
			out[i] = v
		}
		return out, true
	default:
		return nil, false
	}
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeysString(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func tokenSet(tokens []string) map[string]struct{} {
	out := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if token != "" {
			out[token] = struct{}{}
		}
	}
	return out
}

func uniqueTokens(tokens []string) []string {
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func sortedSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func containsTerm(terms []string, want string) bool {
	if want == "" {
		return false
	}
	for _, term := range terms {
		if term == want {
			return true
		}
	}
	return false
}

func hasAnyTerm(terms map[string]struct{}, values ...string) bool {
	for _, value := range values {
		if _, ok := terms[value]; ok {
			return true
		}
	}
	return false
}

func levenshteinWithin(a, b string, maxDistance int) bool {
	if absInt(len(a)-len(b)) > maxDistance {
		return false
	}
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr := make([]int, len(b)+1)
		curr[0] = i
		rowMin := curr[0]
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = minInt(
				prev[j]+1,
				curr[j-1]+1,
				prev[j-1]+cost,
			)
			if curr[j] < rowMin {
				rowMin = curr[j]
			}
		}
		if rowMin > maxDistance {
			return false
		}
		prev = curr
	}
	return prev[len(b)] <= maxDistance
}

func minInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	out := values[0]
	for _, v := range values[1:] {
		if v < out {
			out = v
		}
	}
	return out
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
