// Package fstable defines a minimal, uniform abstraction over object
// stores (local filesystem, S3, GCS) so the GraphJin engine can expose
// them as virtual tables with a single shape.
//
// The interface is intentionally small. Anything backend-specific —
// versions, storage classes, KMS keys, ACLs — is omitted from v1 and
// can be reintroduced later via a `BackendExtras() any` escape hatch.
package fstable

import (
	"context"
	"errors"
	"io"
	"time"
)

// Entry is one row of a filesystem table. Every backend produces this
// shape; the columns map 1:1 to the synthetic DBTable the engine
// registers in the schema graph.
type Entry struct {
	Key         string    // path within the table's effective root
	Size        int64     // bytes
	ContentType string    // best-effort, may be empty
	ETag        string    // strong identifier; backend-defined
	ModifiedAt  time.Time // last-modified timestamp
}

// ListOpts bounds a list operation. Backends may return fewer than
// Limit entries even when more exist; the NextToken is the only signal
// the caller has more pages to fetch.
type ListOpts struct {
	Prefix string
	Limit  int    // 0 = backend default
	After  string // opaque continuation token from a previous List
}

// PutMeta carries the metadata we accept on writes. Size is set by the
// backend on success.
type PutMeta struct {
	ContentType string
}

// PresignOp is the operation a presigned URL grants.
type PresignOp string

const (
	PresignGet PresignOp = "get"
	PresignPut PresignOp = "put"
)

// Backend is the contract every filesystem implementation satisfies.
// Methods take a context so the engine's request deadlines and
// cancellation flow through to the SDK calls.
type Backend interface {
	// Name reports the backend kind ("local", "s3", "gcs"). Used in logs
	// and error messages — never as control-flow input by callers.
	Name() string

	// List enumerates entries under opts.Prefix, returning a page of
	// results and an opaque token for the next page. nextToken is
	// empty when the result set is fully drained.
	List(ctx context.Context, opts ListOpts) (entries []Entry, nextToken string, err error)

	// Stat returns metadata for a single key, or ErrNotFound if absent.
	// No body is fetched.
	Stat(ctx context.Context, key string) (Entry, error)

	// Get returns the body and metadata for a key. Callers MUST close
	// the returned reader. Returns ErrNotFound when the key is absent.
	Get(ctx context.Context, key string) (io.ReadCloser, Entry, error)

	// Put writes a value at key. The reader is consumed entirely; the
	// returned Entry has Size populated from the actual bytes written.
	Put(ctx context.Context, key string, body io.Reader, meta PutMeta) (Entry, error)

	// Delete removes a key. Idempotent: deleting a missing key is not
	// an error.
	Delete(ctx context.Context, key string) error

	// Presign returns a URL that grants the named operation on the key
	// for ttl. Backends that don't support presigning (e.g. local) MAY
	// return a non-presigned URL (file://) when op==PresignGet and
	// ErrUnsupported when op==PresignPut.
	Presign(ctx context.Context, key string, op PresignOp, ttl time.Duration) (string, error)
}

// Sentinel errors used uniformly by backends so the bridge can map
// them to GraphQL-friendly responses without sniffing strings.
var (
	ErrNotFound    = errors.New("fstable: not found")
	ErrUnsupported = errors.New("fstable: operation not supported by this backend")
)
