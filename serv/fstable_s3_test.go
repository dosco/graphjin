//go:build !no_s3

package serv

import "testing"

// These tests cover the pure key-normalisation logic in the S3 backend.
// Real S3 / localstack interactions are out of scope for unit tests —
// the backend code paths around AWS SDK calls are exercised in
// integration tests (gated separately on environment).

func TestS3Backend_FullKey(t *testing.T) {
	cases := []struct {
		prefix, key, want string
	}{
		// No prefix = key passes through unchanged.
		{"", "users/42.png", "users/42.png"},
		{"", "/leading-slash.png", "/leading-slash.png"}, // empty prefix early return preserves input
		// Plain prefix with trailing slash.
		{"avatars/", "users/42.png", "avatars/users/42.png"},
		// Plain prefix without trailing slash — still produces a clean seam.
		{"avatars", "users/42.png", "avatars/users/42.png"},
		// Caller's leading slash gets trimmed so we don't double up.
		{"avatars/", "/users/42.png", "avatars/users/42.png"},
		// Prefix with multiple trailing slashes is normalised.
		{"avatars//", "users/42.png", "avatars/users/42.png"},
	}
	for _, c := range cases {
		b := &s3Backend{prefix: c.prefix}
		got := b.fullKey(c.key)
		if got != c.want {
			t.Errorf("fullKey(prefix=%q, key=%q) = %q, want %q", c.prefix, c.key, got, c.want)
		}
	}
}

func TestS3Backend_TrimPrefix(t *testing.T) {
	cases := []struct {
		prefix, full, want string
	}{
		{"", "users/42.png", "users/42.png"},
		{"avatars/", "avatars/users/42.png", "users/42.png"},
		{"avatars", "avatars/users/42.png", "users/42.png"},
		// Object outside the prefix — returned unchanged (caller is
		// responsible for pre-filtering, this is a defensive default).
		{"avatars/", "other/path.png", "other/path.png"},
	}
	for _, c := range cases {
		b := &s3Backend{prefix: c.prefix}
		got := b.trimPrefix(c.full)
		if got != c.want {
			t.Errorf("trimPrefix(prefix=%q, full=%q) = %q, want %q", c.prefix, c.full, got, c.want)
		}
	}
}

func TestS3Backend_NameMethod(t *testing.T) {
	b := &s3Backend{}
	if got := b.Name(); got != "s3" {
		t.Errorf("Name() = %q, want s3", got)
	}
}
