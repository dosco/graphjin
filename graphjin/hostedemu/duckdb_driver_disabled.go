//go:build !cgo

package hostedemu

import "fmt"

func ensureDuckDBDriver() error {
	return fmt.Errorf("duckdb backend requires a CGO-enabled build")
}
