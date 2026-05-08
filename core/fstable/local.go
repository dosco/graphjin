package fstable

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LocalConfig configures a Local backend rooted at Root. Every key is
// treated as a path relative to Root; absolute paths and `..` segments
// are rejected to keep callers inside the root.
type LocalConfig struct {
	Root string
}

// Local is a Backend backed by an OS directory. Useful for development,
// tests, and single-host deployments. Not a substitute for S3/GCS when
// you need durability or cross-instance sharing.
type Local struct {
	root string
}

// NewLocal validates Root and returns a Local backend. Root must exist
// and be a directory; the backend never creates Root automatically
// because doing so masks misconfigured deployments (e.g. a stale
// directory inside a container).
func NewLocal(conf LocalConfig) (*Local, error) {
	if conf.Root == "" {
		return nil, errors.New("fstable/local: root directory is required")
	}
	abs, err := filepath.Abs(conf.Root)
	if err != nil {
		return nil, fmt.Errorf("fstable/local: resolve root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("fstable/local: stat root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("fstable/local: root %q is not a directory", abs)
	}
	return &Local{root: abs}, nil
}

func (l *Local) Name() string { return "local" }

// resolve joins key onto the root and rejects any path that escapes.
// `key` uses forward slashes regardless of host OS — the wire format
// is intentionally portable so the same query works against an S3 or
// GCS backend.
func (l *Local) resolve(key string) (string, error) {
	if key == "" {
		return "", errors.New("fstable/local: key is required")
	}
	clean := filepath.ToSlash(filepath.Clean("/" + key))
	if strings.HasPrefix(clean, "/../") || clean == "/.." {
		return "", fmt.Errorf("fstable/local: key %q escapes root", key)
	}
	full := filepath.Join(l.root, filepath.FromSlash(strings.TrimPrefix(clean, "/")))
	// Defence in depth: re-check after Join in case the OS-specific path
	// rules let something slip through.
	if !strings.HasPrefix(full+string(filepath.Separator), l.root+string(filepath.Separator)) && full != l.root {
		return "", fmt.Errorf("fstable/local: key %q escapes root", key)
	}
	return full, nil
}

// relKey converts an absolute filesystem path back into a slash-style
// key relative to the root.
func (l *Local) relKey(abs string) (string, error) {
	rel, err := filepath.Rel(l.root, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func (l *Local) List(ctx context.Context, opts ListOpts) ([]Entry, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 1000
	}

	prefix := strings.TrimPrefix(filepath.ToSlash(opts.Prefix), "/")
	walkRoot := l.root
	if prefix != "" {
		walkRoot = filepath.Join(l.root, filepath.FromSlash(prefix))
	}

	// We collect everything under walkRoot, sort lexicographically so
	// `after` becomes a stable continuation marker, then slice.
	var all []Entry
	err := filepath.Walk(walkRoot, func(path string, info os.FileInfo, werr error) error {
		if werr != nil {
			if errors.Is(werr, os.ErrNotExist) && path == walkRoot {
				// Empty prefix: treat as no entries, not an error.
				return filepath.SkipDir
			}
			return werr
		}
		if info.IsDir() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		key, err := l.relKey(path)
		if err != nil {
			return err
		}
		all = append(all, Entry{
			Key:         key,
			Size:        info.Size(),
			ContentType: guessContentType(path),
			ETag:        fmt.Sprintf("%d-%d", info.Size(), info.ModTime().UnixNano()),
			ModifiedAt:  info.ModTime(),
		})
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, "", fmt.Errorf("fstable/local: walk: %w", err)
	}

	sort.Slice(all, func(i, j int) bool { return all[i].Key < all[j].Key })

	// Continuation: skip past `after`.
	start := 0
	if opts.After != "" {
		start = sort.Search(len(all), func(i int) bool { return all[i].Key > opts.After })
	}
	end := start + limit
	var nextToken string
	if end < len(all) {
		nextToken = all[end-1].Key
	} else {
		end = len(all)
	}
	return all[start:end], nextToken, nil
}

func (l *Local) Stat(ctx context.Context, key string) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	full, err := l.resolve(key)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Entry{}, ErrNotFound
		}
		return Entry{}, fmt.Errorf("fstable/local: stat %q: %w", key, err)
	}
	if info.IsDir() {
		return Entry{}, ErrNotFound
	}
	return Entry{
		Key:         key,
		Size:        info.Size(),
		ContentType: guessContentType(full),
		ETag:        fmt.Sprintf("%d-%d", info.Size(), info.ModTime().UnixNano()),
		ModifiedAt:  info.ModTime(),
	}, nil
}

func (l *Local) Get(ctx context.Context, key string) (io.ReadCloser, Entry, error) {
	entry, err := l.Stat(ctx, key)
	if err != nil {
		return nil, Entry{}, err
	}
	full, err := l.resolve(key)
	if err != nil {
		return nil, Entry{}, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, Entry{}, fmt.Errorf("fstable/local: open %q: %w", key, err)
	}
	return f, entry, nil
}

func (l *Local) Put(ctx context.Context, key string, body io.Reader, meta PutMeta) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	full, err := l.resolve(key)
	if err != nil {
		return Entry{}, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return Entry{}, fmt.Errorf("fstable/local: mkdir: %w", err)
	}
	// Write to a sibling temp file then rename for atomicity.
	tmp, err := os.CreateTemp(filepath.Dir(full), ".gj-fstable-*")
	if err != nil {
		return Entry{}, fmt.Errorf("fstable/local: temp: %w", err)
	}
	tmpName := tmp.Name()
	n, copyErr := io.Copy(tmp, body)
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpName)
		return Entry{}, fmt.Errorf("fstable/local: write: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpName)
		return Entry{}, fmt.Errorf("fstable/local: close temp: %w", closeErr)
	}
	if err := os.Rename(tmpName, full); err != nil {
		_ = os.Remove(tmpName)
		return Entry{}, fmt.Errorf("fstable/local: rename: %w", err)
	}
	info, err := os.Stat(full)
	if err != nil {
		return Entry{}, err
	}
	ct := meta.ContentType
	if ct == "" {
		ct = guessContentType(full)
	}
	return Entry{
		Key:         key,
		Size:        n,
		ContentType: ct,
		ETag:        fmt.Sprintf("%d-%d", info.Size(), info.ModTime().UnixNano()),
		ModifiedAt:  info.ModTime(),
	}, nil
}

func (l *Local) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	full, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("fstable/local: delete %q: %w", key, err)
	}
	return nil
}

// Presign on a Local backend returns a `file://` URL for GET (no auth,
// since the path is not network-accessible) and refuses PUT (no
// signing concept exists).
func (l *Local) Presign(ctx context.Context, key string, op PresignOp, ttl time.Duration) (string, error) {
	if op == PresignPut {
		return "", ErrUnsupported
	}
	full, err := l.resolve(key)
	if err != nil {
		return "", err
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(full)}
	return u.String(), nil
}

// guessContentType is a best-effort MIME detection from the extension.
// Backends that have authoritative content-type metadata (S3, GCS) use
// it directly; this helper exists for the local case.
func guessContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	case ".pdf":
		return "application/pdf"
	case ".json":
		return "application/json"
	case ".txt":
		return "text/plain"
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	}
	return "application/octet-stream"
}
