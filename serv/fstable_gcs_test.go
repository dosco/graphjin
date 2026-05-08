//go:build !no_gcs

package serv

import "testing"

// Pure-logic tests for the GCS backend. Real GCS / fake-gcs-server
// interactions belong in a separate integration suite gated on
// environment variables.

func TestGCSBackend_FullKey(t *testing.T) {
	cases := []struct {
		prefix, key, want string
	}{
		{"", "users/42.png", "users/42.png"},
		{"avatars/", "users/42.png", "avatars/users/42.png"},
		{"avatars", "users/42.png", "avatars/users/42.png"},
		{"avatars/", "/users/42.png", "avatars/users/42.png"},
		{"avatars//", "users/42.png", "avatars/users/42.png"},
	}
	for _, c := range cases {
		b := &gcsBackend{prefix: c.prefix}
		got := b.fullKey(c.key)
		if got != c.want {
			t.Errorf("fullKey(prefix=%q, key=%q) = %q, want %q", c.prefix, c.key, got, c.want)
		}
	}
}

func TestGCSBackend_TrimPrefix(t *testing.T) {
	cases := []struct {
		prefix, full, want string
	}{
		{"", "users/42.png", "users/42.png"},
		{"avatars/", "avatars/users/42.png", "users/42.png"},
		{"avatars", "avatars/users/42.png", "users/42.png"},
		{"avatars/", "other/path.png", "other/path.png"},
	}
	for _, c := range cases {
		b := &gcsBackend{prefix: c.prefix}
		got := b.trimPrefix(c.full)
		if got != c.want {
			t.Errorf("trimPrefix(prefix=%q, full=%q) = %q, want %q", c.prefix, c.full, got, c.want)
		}
	}
}

func TestGCSBackend_NameMethod(t *testing.T) {
	b := &gcsBackend{}
	if got := b.Name(); got != "gcs" {
		t.Errorf("Name() = %q, want gcs", got)
	}
}
