package qcode

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func graphError(err error, from, to, through string) error {
	var ambig *sdata.AmbiguousPathError
	if errors.As(err, &ambig) {
		cols := make([]string, len(ambig.Candidates))
		snippets := make([]string, len(ambig.Candidates))
		for i, c := range ambig.Candidates {
			cols[i] = c.Column
			snippets[i] = fmt.Sprintf(`@through(column: %q)`, c.Column)
		}
		return fmt.Errorf(
			"ambiguous relationship %s -> %s: multiple foreign keys (%s). Disambiguate by adding %s on the nested selection",
			from, to, strings.Join(cols, ", "), strings.Join(snippets, " or "))
	}
	switch err {
	case sdata.ErrFromEdgeNotFound:
		return fmt.Errorf("table not found: %s", from)
	case sdata.ErrToEdgeNotFound:
		return fmt.Errorf("table not found: %s", to)
	case sdata.ErrPathNotFound:
		return fmt.Errorf("relationship not found: %s -> %s", from, to)
	case sdata.ErrThoughNodeNotFound:
		return fmt.Errorf("@through(table: %q) not resolved: %q must be a table name (the intermediate/join table). To disambiguate multiple FKs to the same target table by FK column name, use @through(column: %q) instead", through, through, through)
	default:
		return err
	}
}
