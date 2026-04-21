package qcode

import (
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func graphError(err error, from, to, through string) error {
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
