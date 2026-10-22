package core

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/fstable"
)

// userFolderFilesystemBackend keeps per-user folders private. A key under
// prefix + "<user id>/" is visible only to the caller with that user id: it
// is left out of lists, reads as not found, and refuses writes for anyone
// else, including requests with no user. Keys outside prefix are unaffected.
type userFolderFilesystemBackend struct {
	name    string
	prefix  string
	backend fstable.Backend
}

// allowed reports whether the caller in ctx may see key.
func (b userFolderFilesystemBackend) allowed(ctx context.Context, key string) bool {
	rest, ok := strings.CutPrefix(key, b.prefix)
	if !ok {
		return true
	}
	owner, _, ok := strings.Cut(rest, "/")
	if !ok || owner == "" {
		return false
	}
	uid := ctx.Value(UserIDKey)
	return identityValuePresent(uid) && fmt.Sprint(uid) == owner
}

func (b userFolderFilesystemBackend) Name() string { return b.backend.Name() }

func (b userFolderFilesystemBackend) List(ctx context.Context, opts fstable.ListOpts) ([]fstable.Entry, string, error) {
	entries, next, err := b.backend.List(ctx, opts)
	if err != nil {
		return nil, "", err
	}
	out := entries[:0]
	for _, e := range entries {
		if b.allowed(ctx, e.Key) {
			out = append(out, e)
		}
	}
	return out, next, nil
}

func (b userFolderFilesystemBackend) Stat(ctx context.Context, key string) (fstable.Entry, error) {
	if !b.allowed(ctx, key) {
		return fstable.Entry{}, fstable.ErrNotFound
	}
	return b.backend.Stat(ctx, key)
}

func (b userFolderFilesystemBackend) Get(ctx context.Context, key string) (io.ReadCloser, fstable.Entry, error) {
	if !b.allowed(ctx, key) {
		return nil, fstable.Entry{}, fstable.ErrNotFound
	}
	return b.backend.Get(ctx, key)
}

func (b userFolderFilesystemBackend) Put(ctx context.Context, key string, body io.Reader, meta fstable.PutMeta) (fstable.Entry, error) {
	if !b.allowed(ctx, key) {
		return fstable.Entry{}, fmt.Errorf("filesystem %q: %s belongs to another user", b.name, key)
	}
	return b.backend.Put(ctx, key, body, meta)
}

func (b userFolderFilesystemBackend) Delete(ctx context.Context, key string) error {
	if !b.allowed(ctx, key) {
		return fmt.Errorf("filesystem %q: %s belongs to another user", b.name, key)
	}
	return b.backend.Delete(ctx, key)
}

func (b userFolderFilesystemBackend) Presign(ctx context.Context, key string, op fstable.PresignOp, ttl time.Duration) (string, error) {
	if !b.allowed(ctx, key) {
		return "", fstable.ErrNotFound
	}
	return b.backend.Presign(ctx, key, op, ttl)
}
