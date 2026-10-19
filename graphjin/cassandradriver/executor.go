package cassandradriver

import (
	"context"
	"fmt"
	"math/big"
	"net"
	"reflect"
	"time"

	"github.com/gocql/gocql"
	"gopkg.in/inf.v0"
)

// gocqlExecutor implements Executor over a live *gocql.Session. It is the only
// part of the driver that touches gocql; the planner/assembler stay pure.
type gocqlExecutor struct {
	session *gocql.Session
}

func newGocqlExecutor(s *gocql.Session) *gocqlExecutor { return &gocqlExecutor{session: s} }

// Query runs a SELECT, paging by gocql PageState/PageSize, and returns rows with
// gocql-native types normalized to portable Go values.
func (e *gocqlExecutor) Query(ctx context.Context, stmt Statement) (ResultSet, error) {
	q := e.session.Query(stmt.CQL, stmt.Args...).WithContext(ctx).Idempotent(stmt.Idempotent)
	if stmt.PageSize > 0 {
		q = q.PageSize(stmt.PageSize)
	}
	if len(stmt.PageState) > 0 {
		q = q.PageState(stmt.PageState)
	}
	iter := q.Iter()

	var rows []map[string]any
	if stmt.PageSize > 0 {
		// Read exactly the current page. Calling MapScan past NumRows would make
		// gocql transparently fetch the next page, defeating cursor pagination.
		n := iter.NumRows()
		for i := 0; i < n; i++ {
			raw := make(map[string]any)
			if !iter.MapScan(raw) {
				break
			}
			rows = append(rows, normalizeRow(raw))
		}
	} else {
		for {
			raw := make(map[string]any)
			if !iter.MapScan(raw) {
				break
			}
			rows = append(rows, normalizeRow(raw))
		}
	}
	next := iter.PageState()
	if err := iter.Close(); err != nil {
		return ResultSet{}, fmt.Errorf("cassandradriver: query failed: %w", err)
	}
	page := make([]byte, len(next))
	copy(page, next) // gocql reuses its buffer; copy before it's recycled
	return ResultSet{Rows: rows, PageState: page}, nil
}

// Exec runs a write. The applied/page state of an LWT is not surfaced in v1.
func (e *gocqlExecutor) Exec(ctx context.Context, stmt Statement) (ResultSet, error) {
	q := e.session.Query(stmt.CQL, stmt.Args...).WithContext(ctx).Idempotent(stmt.Idempotent)
	if err := q.Exec(); err != nil {
		return ResultSet{}, fmt.Errorf("cassandradriver: exec failed: %w", err)
	}
	return ResultSet{}, nil
}

func normalizeRow(raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		out[k] = normalizeValue(v)
	}
	return out
}

// normalizeValue converts gocql-native scan types into portable Go values that
// DecodeValue (and encoding/json) understand. timestamp/blob are left for
// DecodeValue to format by column type.
func normalizeValue(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case gocql.UUID:
		return x.String()
	case net.IP:
		return x.String()
	case *inf.Dec:
		if x == nil {
			return nil
		}
		return x.String()
	case *big.Int:
		if x == nil {
			return nil
		}
		return x.String()
	case float32:
		return float64(x)
	case time.Time:
		return x
	case []byte:
		return x
	case string, bool, int, int8, int16, int32, int64, float64:
		return v
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = normalizeValue(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		for _, k := range rv.MapKeys() {
			out[fmt.Sprint(normalizeValue(k.Interface()))] = normalizeValue(rv.MapIndex(k).Interface())
		}
		return out
	case reflect.Ptr:
		if rv.IsNil() {
			return nil
		}
		return normalizeValue(rv.Elem().Interface())
	default:
		return v
	}
}
