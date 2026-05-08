package serv

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/fstable"
)

// defaultMaxUploadSize is the fallback total body limit for multipart
// requests when UploadsConfig.MaxSize is zero. 25 MB matches the limit
// most reverse proxies use by default.
const defaultMaxUploadSize int64 = 25 * 1024 * 1024

// uploadFile is the JSON shape injected into GraphQL variables for each
// uploaded file. Mutations consume it as a JSONB value (or via a custom
// PL/pgSQL function that decodes `data` back into bytea).
type uploadFile struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	Data        string `json:"data"` // base64
}

// errMultipartDisabled is returned when a multipart request arrives but
// the operator hasn't enabled the upload endpoint.
var errMultipartDisabled = errors.New("multipart uploads are not enabled in server config (uploads.enabled=true)")

// isMultipartRequest reports whether the request carries a
// multipart/form-data body. We don't gate on method here; the caller
// has already restricted to POST.
func isMultipartRequest(r *http.Request) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return mt == "multipart/form-data"
}

// storageRef is the JSON shape injected at each variable path when
// uploads stream to a filesystem backend (UploadsConfig.Storage set).
// Mutations bind this directly to a JSONB column.
type storageRef struct {
	Key         string `json:"key"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url,omitempty"`
	ETag        string `json:"etag,omitempty"`
	ModifiedAt  string `json:"modified_at,omitempty"`
}

// parseMultipartGraphQL parses a graphql-multipart-request-spec body
// and returns a populated gqlReq with file values injected at the paths
// declared in the `map` part.
//
// When `backend` is nil, files are inlined as base64 (legacy mode).
// When `backend` is non-nil, files are streamed to the backend and
// the injected variable becomes a storageRef pointing at the object.
//
// Spec: https://github.com/jaydenseric/graphql-multipart-request-spec
func parseMultipartGraphQL(r *http.Request, conf UploadsConfig, backend fstable.Backend) (gqlReq, error) {
	maxSize := conf.MaxSize
	if maxSize <= 0 {
		maxSize = defaultMaxUploadSize
	}

	// Cap the raw body before any parsing; protects against zip-bomb
	// style abuse (large declared length, slow trickle).
	r.Body = http.MaxBytesReader(nil, r.Body, maxSize)

	if err := r.ParseMultipartForm(maxSize); err != nil {
		return gqlReq{}, fmt.Errorf("multipart: parse failed: %w", err)
	}

	// 1. operations: { query, operationName, variables }
	opsRaw := r.FormValue("operations")
	if opsRaw == "" {
		return gqlReq{}, errors.New("multipart: missing 'operations' field")
	}
	var ops gqlReq
	if err := json.Unmarshal([]byte(opsRaw), &ops); err != nil {
		return gqlReq{}, fmt.Errorf("multipart: 'operations' is not valid JSON: %w", err)
	}

	// 2. map: { "0": ["variables.file"], "1": ["variables.files.0"] }
	mapRaw := r.FormValue("map")
	if mapRaw == "" {
		// Spec mandates `map`; without it, no file injection happens.
		// Treat as a structural error rather than silently dropping uploads.
		return gqlReq{}, errors.New("multipart: missing 'map' field")
	}
	var fileMap map[string][]string
	if err := json.Unmarshal([]byte(mapRaw), &fileMap); err != nil {
		return gqlReq{}, fmt.Errorf("multipart: 'map' is not valid JSON: %w", err)
	}

	// Decode variables to a generic structure so we can write file
	// values at arbitrary paths.
	var vars map[string]any
	if len(ops.Vars) > 0 {
		if err := json.Unmarshal(ops.Vars, &vars); err != nil {
			return gqlReq{}, fmt.Errorf("multipart: 'variables' is not valid JSON: %w", err)
		}
	} else {
		vars = make(map[string]any)
	}
	root := map[string]any{"variables": vars}

	allowed := buildMIMEAllowlist(conf.AllowedMIME)
	ctx := r.Context()

	for partName, paths := range fileMap {
		fhs, ok := r.MultipartForm.File[partName]
		if !ok || len(fhs) == 0 {
			return gqlReq{}, fmt.Errorf("multipart: 'map' references file %q which is missing from the request", partName)
		}
		fh := fhs[0]

		ct := fh.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		if !mimeAllowed(ct, allowed) {
			return gqlReq{}, fmt.Errorf("multipart: file %q has disallowed content-type %q", fh.Filename, ct)
		}

		var injected any
		if backend != nil {
			f, err := fh.Open()
			if err != nil {
				return gqlReq{}, fmt.Errorf("multipart: open %q: %w", fh.Filename, err)
			}
			ref, err := streamToBackend(ctx, backend, f, fh.Filename, ct, conf.StorageKeyPrefix)
			_ = f.Close()
			if err != nil {
				return gqlReq{}, fmt.Errorf("multipart: stream %q: %w", fh.Filename, err)
			}
			injected = ref
		} else {
			f, err := fh.Open()
			if err != nil {
				return gqlReq{}, fmt.Errorf("multipart: open file %q: %w", fh.Filename, err)
			}
			buf, err := io.ReadAll(f)
			_ = f.Close()
			if err != nil {
				return gqlReq{}, fmt.Errorf("multipart: read file %q: %w", fh.Filename, err)
			}
			injected = uploadFile{
				Filename:    fh.Filename,
				ContentType: ct,
				Size:        len(buf),
				Data:        base64.StdEncoding.EncodeToString(buf),
			}
		}

		for _, p := range paths {
			if err := setAtPath(root, p, injected); err != nil {
				return gqlReq{}, fmt.Errorf("multipart: %w", err)
			}
		}
	}

	// Re-marshal vars after injection.
	merged, err := json.Marshal(vars)
	if err != nil {
		return gqlReq{}, fmt.Errorf("multipart: re-marshal variables: %w", err)
	}
	ops.Vars = merged
	return ops, nil
}

// streamToBackend uploads a file body to the given backend under a
// generated key. The key combines the prefix template with a random
// suffix and the original extension, so callers can predict where
// files land and the backend rejects collisions naturally.
func streamToBackend(
	ctx context.Context,
	backend fstable.Backend,
	body io.Reader,
	originalName, contentType, prefixTpl string,
) (storageRef, error) {
	key := generateUploadKey(prefixTpl, originalName)
	entry, err := backend.Put(ctx, key, body, fstable.PutMeta{ContentType: contentType})
	if err != nil {
		return storageRef{}, err
	}

	url, perr := backend.Presign(ctx, entry.Key, fstable.PresignGet, 15*time.Minute)
	if perr != nil && !errors.Is(perr, fstable.ErrUnsupported) {
		return storageRef{}, fmt.Errorf("presign: %w", perr)
	}

	return storageRef{
		Key:         entry.Key,
		ContentType: entry.ContentType,
		Size:        entry.Size,
		URL:         url,
		ETag:        entry.ETag,
		ModifiedAt:  entry.ModifiedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

// generateUploadKey produces a deterministic-looking storage key that
// combines the prefix template, a random suffix for uniqueness, and
// the original file extension. The {date} marker in prefixTpl is
// substituted with YYYY/MM/DD at call time.
func generateUploadKey(prefixTpl, originalName string) string {
	prefix := strings.ReplaceAll(prefixTpl, "{date}", time.Now().UTC().Format("2006/01/02"))
	prefix = strings.TrimRight(prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	suffix := randomHex(8)
	ext := filepath.Ext(originalName)
	return prefix + suffix + ext
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}


// setAtPath writes value into root at a dotted path like
// "variables.input.avatar" or "variables.files.0". Numeric path
// components index into slices; string components into maps.
func setAtPath(root map[string]any, p string, value any) error {
	if p == "" {
		return errors.New("empty path in 'map'")
	}
	parts := strings.Split(p, ".")
	if parts[0] != "variables" {
		return fmt.Errorf("path %q must start with 'variables'", p)
	}

	var cur any = root
	for i := 0; i < len(parts)-1; i++ {
		key := parts[i]
		next, ok := stepInto(cur, key)
		if !ok {
			return fmt.Errorf("path %q: cannot traverse segment %q", p, key)
		}
		cur = next
	}

	last := parts[len(parts)-1]
	switch container := cur.(type) {
	case map[string]any:
		container[last] = value
	case []any:
		idx, err := strconv.Atoi(last)
		if err != nil || idx < 0 || idx >= len(container) {
			return fmt.Errorf("path %q: index %q out of range", p, last)
		}
		container[idx] = value
	default:
		return fmt.Errorf("path %q: parent of %q is not an object or array", p, last)
	}
	return nil
}

func stepInto(cur any, key string) (any, bool) {
	switch v := cur.(type) {
	case map[string]any:
		next, ok := v[key]
		return next, ok
	case []any:
		idx, err := strconv.Atoi(key)
		if err != nil || idx < 0 || idx >= len(v) {
			return nil, false
		}
		return v[idx], true
	}
	return nil, false
}

// buildMIMEAllowlist returns the parsed allowlist, or nil when the
// caller didn't set one (any MIME accepted).
func buildMIMEAllowlist(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// mimeAllowed reports whether a content-type matches the allowlist.
// Glob patterns are supported in the form "type/*" (e.g. "image/*").
func mimeAllowed(ct string, allow []string) bool {
	if len(allow) == 0 {
		return true
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	mt = strings.ToLower(mt)
	for _, p := range allow {
		ok, err := path.Match(p, mt)
		if err == nil && ok {
			return true
		}
	}
	return false
}
