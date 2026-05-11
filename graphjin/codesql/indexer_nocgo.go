//go:build !cgo

package codesql

import (
	"context"
	"fmt"
)

// OpenManaged is available only in cgo-enabled builds because the bundled
// tree-sitter grammar providers are cgo bindings.
func OpenManaged(ctx context.Context, opts Options) (*Managed, *Stats, error) {
	return nil, nil, fmt.Errorf("codesql requires a cgo-enabled build with bundled tree-sitter grammars")
}
