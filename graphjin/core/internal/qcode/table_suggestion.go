package qcode

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// maxTableSuggestions caps how many names an error offers. A short list gets
// acted on; a long one gets skimmed and the name guessed again.
const maxTableSuggestions = 3

// suggestTableNames returns the tables whose names are related to want, sorted
// and capped, for a "did you mean" clause.
//
// Matching is deliberately loose rather than a spelling distance, because the
// misses worth catching are not misspellings: a model reading "the support SLA
// policy file" asks for `policies` when the table is `sla_policies`, or for
// `ticket` when it is `support_tickets`. Substring and suffix cover exactly
// that, so a dropped or added qualifier still resolves to the real name.
func (co *Compiler) suggestTableNames(want string) []string {
	want = strings.ToLower(co.ParseName(want))
	if want == "" {
		return nil
	}
	var names []string
	seen := make(map[string]bool)
	for _, table := range co.s.GetTables() {
		name := strings.ToLower(table.Name)
		if !strings.Contains(name, want) && !strings.Contains(want, name) && !strings.HasSuffix(name, "_"+want) {
			continue
		}
		if seen[table.Name] {
			continue
		}
		seen[table.Name] = true
		names = append(names, table.Name)
	}
	sort.Strings(names)
	if len(names) > maxTableSuggestions {
		names = names[:maxTableSuggestions]
	}
	return names
}

// didYouMeanClause renders suggestions as an error suffix, and renders nothing
// when there are none — an error that trails off into "did you mean?" with no
// names is worse than one that stops.
func didYouMeanClause(suggestions []string) string {
	switch len(suggestions) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("; did you mean %q?", suggestions[0])
	default:
		return fmt.Sprintf("; did you mean one of %q?", suggestions)
	}
}

// decorateTableSuggestions applies a root-naming scheme to suggested table
// names, for dialects whose roots are not the bare table name.
func decorateTableSuggestions(names []string, decorate func(string) string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, decorate(n))
	}
	return out
}

// tableNotFoundError names the tables a missing root may have meant.
//
// Without this a model gets back only the name it already knows is wrong, so it
// guesses again: recorded runs spent an entire step budget re-asking for
// `policies` and `files` while the table sat there as `sla_policies`, and the
// file half of every cross-source task scored zero because of it. Errors that
// already name real tables — the ambiguous-schema one — are left alone.
func (co *Compiler) tableNotFoundError(name string, err error) error {
	if !errors.Is(err, sdata.ErrTableNotFound) {
		return err
	}
	clause := didYouMeanClause(co.suggestTableNames(name))
	if clause == "" {
		return err
	}
	return fmt.Errorf("%w%s", err, clause)
}
