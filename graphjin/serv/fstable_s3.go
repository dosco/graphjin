//go:build !no_s3

package serv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/fstable"
)

// s3Backend implements fstable.Backend on top of AWS S3 (or any
// S3-compatible service via Endpoint override). One instance is
// created per FilesystemConfig at engine init.
type s3Backend struct {
	client      *s3.Client
	presigner   *s3.PresignClient
	bucket      string
	prefix      string
	listMaxKeys int32
}

// newS3Backend builds an s3Backend from a core.FilesystemConfig. It
// uses the standard AWS credential chain (env vars, ~/.aws/credentials,
// IRSA / EC2 IMDS), so deployments configure auth out-of-band rather
// than through GraphJin config.
func newS3Backend(conf core.FilesystemConfig) (fstable.Backend, error) {
	if conf.Bucket == "" {
		return nil, errors.New("filesystems[s3]: bucket is required")
	}
	loadOpts := []func(*awsconfig.LoadOptions) error{}
	if conf.Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(conf.Region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("filesystems[s3]: aws config: %w", err)
	}

	clientOpts := []func(*s3.Options){}
	if conf.Endpoint != "" {
		ep := conf.Endpoint
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = &ep
			o.UsePathStyle = true // most S3-compatible services need path-style
		})
	}
	client := s3.NewFromConfig(cfg, clientOpts...)

	maxKeys := int32(1000)
	if conf.MaxListPageSize > 0 {
		maxKeys = int32(conf.MaxListPageSize)
	}

	return &s3Backend{
		client:      client,
		presigner:   s3.NewPresignClient(client),
		bucket:      conf.Bucket,
		prefix:      conf.Prefix,
		listMaxKeys: maxKeys,
	}, nil
}

func (b *s3Backend) Name() string { return "s3" }

// fullKey concatenates the configured prefix with the user-supplied
// key, normalising the seam so a missing or doubled "/" is harmless.
func (b *s3Backend) fullKey(key string) string {
	if b.prefix == "" {
		return key
	}
	p := strings.TrimRight(b.prefix, "/")
	k := strings.TrimLeft(key, "/")
	return p + "/" + k
}

// trimPrefix strips the configured prefix off the start of the supplied
// key so the GraphQL response surfaces caller-relative paths.
func (b *s3Backend) trimPrefix(key string) string {
	if b.prefix == "" {
		return key
	}
	p := strings.TrimRight(b.prefix, "/") + "/"
	return strings.TrimPrefix(key, p)
}

func (b *s3Backend) List(ctx context.Context, opts fstable.ListOpts) ([]fstable.Entry, string, error) {
	limit := int32(opts.Limit)
	if limit <= 0 || limit > b.listMaxKeys {
		limit = b.listMaxKeys
	}
	in := &s3.ListObjectsV2Input{
		Bucket:  &b.bucket,
		MaxKeys: &limit,
	}
	if pfx := b.fullKey(opts.Prefix); pfx != "" {
		in.Prefix = &pfx
	}
	if opts.After != "" {
		tok := opts.After
		in.ContinuationToken = &tok
	}
	out, err := b.client.ListObjectsV2(ctx, in)
	if err != nil {
		return nil, "", fmt.Errorf("s3 list: %w", err)
	}
	entries := make([]fstable.Entry, 0, len(out.Contents))
	for _, o := range out.Contents {
		e := fstable.Entry{
			Key: b.trimPrefix(*o.Key),
		}
		if o.Size != nil {
			e.Size = *o.Size
		}
		if o.ETag != nil {
			e.ETag = strings.Trim(*o.ETag, `"`)
		}
		if o.LastModified != nil {
			e.ModifiedAt = *o.LastModified
		}
		entries = append(entries, e)
	}
	next := ""
	if out.NextContinuationToken != nil {
		next = *out.NextContinuationToken
	}
	return entries, next, nil
}

func (b *s3Backend) Stat(ctx context.Context, key string) (fstable.Entry, error) {
	full := b.fullKey(key)
	out, err := b.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &b.bucket, Key: &full})
	if err != nil {
		var nf *types.NotFound
		if errors.As(err, &nf) {
			return fstable.Entry{}, fstable.ErrNotFound
		}
		return fstable.Entry{}, fmt.Errorf("s3 head: %w", err)
	}
	e := fstable.Entry{Key: key}
	if out.ContentLength != nil {
		e.Size = *out.ContentLength
	}
	if out.ContentType != nil {
		e.ContentType = *out.ContentType
	}
	if out.ETag != nil {
		e.ETag = strings.Trim(*out.ETag, `"`)
	}
	if out.LastModified != nil {
		e.ModifiedAt = *out.LastModified
	}
	return e, nil
}

func (b *s3Backend) Get(ctx context.Context, key string) (io.ReadCloser, fstable.Entry, error) {
	full := b.fullKey(key)
	out, err := b.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &b.bucket, Key: &full})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, fstable.Entry{}, fstable.ErrNotFound
		}
		return nil, fstable.Entry{}, fmt.Errorf("s3 get: %w", err)
	}
	e := fstable.Entry{Key: key}
	if out.ContentLength != nil {
		e.Size = *out.ContentLength
	}
	if out.ContentType != nil {
		e.ContentType = *out.ContentType
	}
	if out.ETag != nil {
		e.ETag = strings.Trim(*out.ETag, `"`)
	}
	if out.LastModified != nil {
		e.ModifiedAt = *out.LastModified
	}
	return out.Body, e, nil
}

func (b *s3Backend) Put(ctx context.Context, key string, body io.Reader, meta fstable.PutMeta) (fstable.Entry, error) {
	full := b.fullKey(key)
	in := &s3.PutObjectInput{Bucket: &b.bucket, Key: &full, Body: body}
	if meta.ContentType != "" {
		ct := meta.ContentType
		in.ContentType = &ct
	}
	out, err := b.client.PutObject(ctx, in)
	if err != nil {
		return fstable.Entry{}, fmt.Errorf("s3 put: %w", err)
	}
	// PutObjectOutput doesn't carry size; surface what we know.
	e := fstable.Entry{Key: key, ContentType: meta.ContentType, ModifiedAt: time.Now().UTC()}
	if out.ETag != nil {
		e.ETag = strings.Trim(*out.ETag, `"`)
	}
	return e, nil
}

func (b *s3Backend) Delete(ctx context.Context, key string) error {
	full := b.fullKey(key)
	_, err := b.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &b.bucket, Key: &full})
	if err != nil {
		// Match the local backend's idempotent semantics.
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil
		}
		return fmt.Errorf("s3 delete: %w", err)
	}
	return nil
}

func (b *s3Backend) Presign(ctx context.Context, key string, op fstable.PresignOp, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	full := b.fullKey(key)
	switch op {
	case fstable.PresignGet:
		req, err := b.presigner.PresignGetObject(ctx,
			&s3.GetObjectInput{Bucket: &b.bucket, Key: &full},
			s3.WithPresignExpires(ttl),
		)
		if err != nil {
			return "", fmt.Errorf("s3 presign get: %w", err)
		}
		return req.URL, nil
	case fstable.PresignPut:
		req, err := b.presigner.PresignPutObject(ctx,
			&s3.PutObjectInput{Bucket: &b.bucket, Key: &full},
			s3.WithPresignExpires(ttl),
		)
		if err != nil {
			return "", fmt.Errorf("s3 presign put: %w", err)
		}
		return req.URL, nil
	default:
		return "", fstable.ErrUnsupported
	}
}

// init() registers the s3 factory on package load. This relies on the
// fact that the user's binary imports `serv` (every GraphJin server
// does) and that the build tag !no_s3 is on by default. Slim builds
// can opt out with `-tags no_s3`.
func init() {
	registerBuiltinFilesystemBackend("s3", newS3Backend)
}
