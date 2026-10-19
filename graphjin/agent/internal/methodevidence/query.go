// Package methodevidence shares execution-backed method checks between the
// current evaluator and the compatibility data-accuracy evaluator.
package methodevidence

import (
	"regexp"
	"strings"

	"github.com/dosco/graphjin/core/v3/queryshape"
)

const Version = "graphjin.eval.method/v2"

// Bare native aggregate requirements are semantic operation/column checks.
// Other patterns remain explicit structural regex requirements (e.g. order_by).
var aggregateRequirement = regexp.MustCompile(`^(?:(?:count|sum|avg|min|max|stddev|variance)_[A-Za-z0-9_]*)(?:\|(?:count|sum|avg|min|max|stddev|variance)_[A-Za-z0-9_]*)*$`)

func MatchRequired(pattern *regexp.Regexp, source, query string) bool {
	if !aggregateRequirement.MatchString(source) {
		return pattern.MatchString(query)
	}
	facts, err := queryshape.Aggregates(query)
	if err != nil {
		return false
	}
	for _, fact := range facts {
		for _, alternative := range strings.Split(source, "|") {
			fn, col, _ := strings.Cut(alternative, "_")
			if strings.EqualFold(fn, fact.Operation) && (col == "" || strings.EqualFold(col, fact.Column)) {
				return true
			}
		}
	}
	return false
}

func HasAggregate(query string) bool {
	facts, err := queryshape.Aggregates(query)
	return err == nil && len(facts) > 0
}

func HasAnyAggregate(queries []string) bool {
	for _, query := range queries {
		if HasAggregate(query) {
			return true
		}
	}
	return false
}

func Successful(action map[string]any) bool {
	status, _ := action["status"].(string)
	if status != "" && status != "ok" {
		return false
	}
	count := object(action["summary"])["error_count"]
	return count == nil || count == float64(0) || count == 0 || count == int64(0)
}

// ExecutionQuery reads raw GraphQL only for execution actions. A saved-query
// name or caller-provided query argument is never proof of its definition.
func (r *Resolver) ExecutionQuery(action map[string]any) string {
	args := object(action["args"])
	switch action["tool"] {
	case "execute_graphql", "execute_sql", "execute_raw_query":
		return strings.TrimSpace(text(args["query"]))
	case "execute_saved_query":
		name := strings.TrimSpace(text(args["name"]))
		ns := strings.TrimSpace(text(args["namespace"]))
		if ns == "" {
			ns = r.namespace
		}
		if ns != "" && !strings.Contains(name, ".") {
			name = ns + "." + name
		}
		return r.observed[name]
	}
	return ""
}
