package cassandradriver

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// cursorPrefix tags Cassandra cursors so a non-Cassandra cursor is rejected early.
const cursorPrefix = "cql:"

// EncodeCursor turns an opaque gocql page-state into a GraphJin cursor string. An
// empty page-state (no further pages) yields an empty cursor.
func EncodeCursor(pageState []byte) string {
	if len(pageState) == 0 {
		return ""
	}
	return cursorPrefix + base64.RawURLEncoding.EncodeToString(pageState)
}

// DecodeCursor reverses EncodeCursor. An empty cursor decodes to a nil page-state
// (start from the first page).
func DecodeCursor(cursor string) ([]byte, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return nil, nil
	}
	if !strings.HasPrefix(cursor, cursorPrefix) {
		return nil, fmt.Errorf("cassandradriver: not a Cassandra page cursor")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cursor, cursorPrefix))
	if err != nil {
		return nil, fmt.Errorf("cassandradriver: invalid cursor encoding: %w", err)
	}
	return b, nil
}
