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
		return fmt.Errorf("table not found: %s", through)
	default:
		return err
	}
}
