//go:build cgo

package hostedemu

import _ "github.com/duckdb/duckdb-go/v2"

func ensureDuckDBDriver() error {
	return nil
}
