package dialect

import (
	"fmt"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

// RenderStandardArithOp returns the standard SQL infix token for the
// given scalar arithmetic op. Used by every dialect that supports inline
// arithmetic (postgres, mysql, mariadb, mssql, oracle, sqlite, snowflake)
// — mongodb routes to a different renderer because its aggregation
// pipeline uses operators like $multiply rather than infix tokens.
//
// Note: integer division semantics differ across dialects (Postgres /
// Oracle return int when both operands are int; MySQL / MSSQL return
// float). v1 follows the underlying DB's semantics — users wanting a
// specific behavior wrap operands with `cast`.
func RenderStandardArithOp(op qcode.ExpOp) (string, error) {
	switch op {
	case qcode.OpAdd:
		return "+", nil
	case qcode.OpSub:
		return "-", nil
	case qcode.OpMul:
		return "*", nil
	case qcode.OpDiv:
		return "/", nil
	case qcode.OpMod:
		return "%", nil
	}
	return "", fmt.Errorf("not an arithmetic op: %s", op)
}
