package introspection

import (
	"context"
	"strings"
	"time"
)

type discoveryQueryEvent struct {
	Phase         string
	SQL           string
	Elapsed       time.Duration
	TableSpecific bool
}

type discoveryQueryRecorder func(discoveryQueryEvent)

type discoveryQueryRecorderKey struct{}

func withDiscoveryQueryRecorder(ctx context.Context, fn discoveryQueryRecorder) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, discoveryQueryRecorderKey{}, fn)
}

func recordDiscoveryQuery(ctx context.Context, phase, sql string, elapsed time.Duration) {
	fn, _ := ctx.Value(discoveryQueryRecorderKey{}).(discoveryQueryRecorder)
	if fn == nil {
		return
	}
	fn(discoveryQueryEvent{
		Phase:         phase,
		SQL:           sql,
		Elapsed:       elapsed,
		TableSpecific: isTableSpecificDiscoverySQL(sql),
	})
}

func isTableSpecificDiscoverySQL(sql string) bool {
	upper := strings.ToUpper(strings.Join(strings.Fields(sql), " "))
	if upper == "" {
		return false
	}
	if strings.Contains(upper, "INFORMATION_SCHEMA") ||
		strings.Contains(upper, "PG_CATALOG") ||
		strings.Contains(upper, "PG_CLASS") ||
		strings.Contains(upper, "SYS.") ||
		strings.Contains(upper, "ALL_TABLES") ||
		strings.Contains(upper, "USER_TAB_COLUMNS") ||
		strings.Contains(upper, "SQLITE_MASTER") ||
		strings.Contains(upper, "PRAGMA_TABLE_INFO") {
		return false
	}
	return strings.Contains(upper, " TABLE_NAME =") ||
		strings.Contains(upper, " TABLE_NAME=") ||
		strings.Contains(upper, " RELNAME =") ||
		strings.Contains(upper, " RELNAME=") ||
		strings.Contains(upper, " OBJECT_ID(")
}
