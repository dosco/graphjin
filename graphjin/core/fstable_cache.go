package core

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/dosco/graphjin/core/v3/fstable"
)

type cacheInvalidatingFilesystemBackend struct {
	name    string
	backend fstable.Backend
	cache   ResponseCacheProvider
}

func (b cacheInvalidatingFilesystemBackend) Name() string {
	return b.backend.Name()
}

func (b cacheInvalidatingFilesystemBackend) List(ctx context.Context, opts fstable.ListOpts) ([]fstable.Entry, string, error) {
	return b.backend.List(ctx, opts)
}

func (b cacheInvalidatingFilesystemBackend) Stat(ctx context.Context, key string) (fstable.Entry, error) {
	return b.backend.Stat(ctx, key)
}

func (b cacheInvalidatingFilesystemBackend) Get(ctx context.Context, key string) (io.ReadCloser, fstable.Entry, error) {
	return b.backend.Get(ctx, key)
}

func (b cacheInvalidatingFilesystemBackend) Put(ctx context.Context, key string, body io.Reader, meta fstable.PutMeta) (fstable.Entry, error) {
	entry, err := b.backend.Put(ctx, key, body, meta)
	if err == nil && b.cache != nil {
		_ = b.cache.InvalidateRows(ctx, FilesystemKeyRefs(b.name, entry.Key))
	}
	return entry, err
}

func (b cacheInvalidatingFilesystemBackend) Delete(ctx context.Context, key string) error {
	err := b.backend.Delete(ctx, key)
	if err == nil && b.cache != nil {
		_ = b.cache.InvalidateRows(ctx, FilesystemKeyRefs(b.name, key))
	}
	return err
}

func (b cacheInvalidatingFilesystemBackend) Presign(ctx context.Context, key string, op fstable.PresignOp, ttl time.Duration) (string, error) {
	return b.backend.Presign(ctx, key, op, ttl)
}

type readOnlyFilesystemBackend struct {
	name    string
	backend fstable.Backend
}

func (b readOnlyFilesystemBackend) Name() string {
	return b.backend.Name()
}

func (b readOnlyFilesystemBackend) List(ctx context.Context, opts fstable.ListOpts) ([]fstable.Entry, string, error) {
	return b.backend.List(ctx, opts)
}

func (b readOnlyFilesystemBackend) Stat(ctx context.Context, key string) (fstable.Entry, error) {
	return b.backend.Stat(ctx, key)
}

func (b readOnlyFilesystemBackend) Get(ctx context.Context, key string) (io.ReadCloser, fstable.Entry, error) {
	return b.backend.Get(ctx, key)
}

func (b readOnlyFilesystemBackend) Put(ctx context.Context, key string, body io.Reader, meta fstable.PutMeta) (fstable.Entry, error) {
	return fstable.Entry{}, fmt.Errorf("filesystem %q is read-only", b.name)
}

func (b readOnlyFilesystemBackend) Delete(ctx context.Context, key string) error {
	return fmt.Errorf("filesystem %q is read-only", b.name)
}

func (b readOnlyFilesystemBackend) Presign(ctx context.Context, key string, op fstable.PresignOp, ttl time.Duration) (string, error) {
	return b.backend.Presign(ctx, key, op, ttl)
}
