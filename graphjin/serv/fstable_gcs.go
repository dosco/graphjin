//go:build !no_gcs

package serv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/fstable"
)

// gcsBackend implements fstable.Backend on top of Google Cloud Storage.
// One instance is created per FilesystemConfig at engine init.
//
// Auth follows the standard Application Default Credentials chain
// (GOOGLE_APPLICATION_CREDENTIALS, metadata server on GCE/GKE, gcloud
// user creds), so deployments configure auth out-of-band rather than
// through GraphJin config.
type gcsBackend struct {
	client      *storage.Client
	bucket      *storage.BucketHandle
	bucketName  string
	prefix      string
	listMaxKeys int
}

func newGCSBackend(conf core.FilesystemConfig) (fstable.Backend, error) {
	if conf.Bucket == "" {
		return nil, errors.New("filesystems[gcs]: bucket is required")
	}
	client, err := storage.NewClient(context.Background())
	if err != nil {
		return nil, fmt.Errorf("filesystems[gcs]: client: %w", err)
	}
	maxKeys := 1000
	if conf.MaxListPageSize > 0 {
		maxKeys = conf.MaxListPageSize
	}
	return &gcsBackend{
		client:      client,
		bucket:      client.Bucket(conf.Bucket),
		bucketName:  conf.Bucket,
		prefix:      conf.Prefix,
		listMaxKeys: maxKeys,
	}, nil
}

func (b *gcsBackend) Name() string { return "gcs" }

func (b *gcsBackend) fullKey(key string) string {
	if b.prefix == "" {
		return key
	}
	return strings.TrimRight(b.prefix, "/") + "/" + strings.TrimLeft(key, "/")
}

func (b *gcsBackend) trimPrefix(key string) string {
	if b.prefix == "" {
		return key
	}
	return strings.TrimPrefix(key, strings.TrimRight(b.prefix, "/")+"/")
}

func (b *gcsBackend) List(ctx context.Context, opts fstable.ListOpts) ([]fstable.Entry, string, error) {
	limit := opts.Limit
	if limit <= 0 || limit > b.listMaxKeys {
		limit = b.listMaxKeys
	}
	q := &storage.Query{Prefix: b.fullKey(opts.Prefix)}
	if err := q.SetAttrSelection([]string{"Name", "Size", "ContentType", "Etag", "Updated"}); err != nil {
		return nil, "", fmt.Errorf("gcs query attrs: %w", err)
	}
	it := b.bucket.Objects(ctx, q)

	entries := make([]fstable.Entry, 0, limit)
	for len(entries) < limit {
		attrs, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("gcs list: %w", err)
		}
		key := b.trimPrefix(attrs.Name)
		// Skip-until-after pagination: cheaper than threading GCS's
		// pageToken through our opaque token shape, and the result
		// set is already in lexicographic order.
		if opts.After != "" && key <= opts.After {
			continue
		}
		entries = append(entries, fstable.Entry{
			Key:         key,
			Size:        attrs.Size,
			ContentType: attrs.ContentType,
			ETag:        attrs.Etag,
			ModifiedAt:  attrs.Updated,
		})
	}

	next := ""
	if len(entries) == limit {
		next = entries[len(entries)-1].Key
	}
	return entries, next, nil
}

func (b *gcsBackend) Stat(ctx context.Context, key string) (fstable.Entry, error) {
	attrs, err := b.bucket.Object(b.fullKey(key)).Attrs(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return fstable.Entry{}, fstable.ErrNotFound
		}
		return fstable.Entry{}, fmt.Errorf("gcs attrs: %w", err)
	}
	return fstable.Entry{
		Key:         key,
		Size:        attrs.Size,
		ContentType: attrs.ContentType,
		ETag:        attrs.Etag,
		ModifiedAt:  attrs.Updated,
	}, nil
}

func (b *gcsBackend) Get(ctx context.Context, key string) (io.ReadCloser, fstable.Entry, error) {
	r, err := b.bucket.Object(b.fullKey(key)).NewReader(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil, fstable.Entry{}, fstable.ErrNotFound
		}
		return nil, fstable.Entry{}, fmt.Errorf("gcs reader: %w", err)
	}
	attrs := r.Attrs
	return r, fstable.Entry{
		Key:         key,
		Size:        attrs.Size,
		ContentType: attrs.ContentType,
		ModifiedAt:  attrs.LastModified,
	}, nil
}

func (b *gcsBackend) Put(ctx context.Context, key string, body io.Reader, meta fstable.PutMeta) (fstable.Entry, error) {
	w := b.bucket.Object(b.fullKey(key)).NewWriter(ctx)
	if meta.ContentType != "" {
		w.ContentType = meta.ContentType
	}
	n, err := io.Copy(w, body)
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fstable.Entry{}, fmt.Errorf("gcs put: %w", err)
	}
	return fstable.Entry{
		Key:         key,
		Size:        n,
		ContentType: meta.ContentType,
		ETag:        w.Attrs().Etag,
		ModifiedAt:  w.Attrs().Updated,
	}, nil
}

func (b *gcsBackend) Delete(ctx context.Context, key string) error {
	err := b.bucket.Object(b.fullKey(key)).Delete(ctx)
	if err != nil {
		// Match the local backend's idempotent semantics.
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil
		}
		return fmt.Errorf("gcs delete: %w", err)
	}
	return nil
}

// Presign on GCS uses V4 signed URLs. Requires the runtime identity
// to either carry a private key or have iam.serviceAccountTokenCreator
// on itself so it can self-sign via the IAM API.
func (b *gcsBackend) Presign(ctx context.Context, key string, op fstable.PresignOp, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	var method string
	switch op {
	case fstable.PresignGet:
		method = "GET"
	case fstable.PresignPut:
		method = "PUT"
	default:
		return "", fstable.ErrUnsupported
	}
	url, err := b.bucket.SignedURL(b.fullKey(key), &storage.SignedURLOptions{
		Method:  method,
		Expires: time.Now().Add(ttl),
		Scheme:  storage.SigningSchemeV4,
	})
	if err != nil {
		return "", fmt.Errorf("gcs signedurl: %w", err)
	}
	return url, nil
}

func init() {
	registerBuiltinFilesystemBackend("gcs", newGCSBackend)
}
