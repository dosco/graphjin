package qcode

import (
	"errors"
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// suggestTableNames returns the tables whose names are related to want. The
// matching rule lives in sdata so the compiler and the public API cannot drift
// apart on what counts as a near miss.
func (co *Compiler) suggestTableNames(want string) []string {
	tables := co.s.GetTables()
	names := make([]string, 0, len(tables))
	for _, t := range tables {
		names = append(names, t.Name)
	}
	return sdata.MatchTableNames(co.ParseName(want), names)
}

// didYouMeanClause renders suggestions as an error suffix.
func didYouMeanClause(suggestions []string) string {
	return sdata.DidYouMeanClause(suggestions)
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
