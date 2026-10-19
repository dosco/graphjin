package lazy

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var resultID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type resultStore struct {
	root string
	ttl  time.Duration
}

func resultPath(path string) (id, file string, lookup bool) {
	for prefix, name := range map[string]string{"/v1/status/poll/": "status.json", "/v1/result/": "result.json"} {
		if strings.HasPrefix(path, prefix) {
			return strings.TrimPrefix(path, prefix), name, true
		}
	}
	return "", "", false
}

// Published directories are immutable. Open files remain readable if expiry
// concurrently unlinks them. No result body is loaded into the listener heap.
func (s *resultStore) serve(w http.ResponseWriter, r *http.Request, id, file string) bool {
	if !resultID.MatchString(id) {
		http.NotFound(w, r)
		return true
	}
	folder := filepath.Join(s.root, id)
	info, err := os.Lstat(folder)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil || !info.IsDir() {
		http.Error(w, "result unavailable", 503)
		return true
	}
	if time.Since(info.ModTime()) >= s.ttl {
		return false
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		http.Error(w, "result unavailable", 503)
		return true
	}
	defer root.Close()
	input, err := root.Open(file)
	if err != nil {
		http.Error(w, "result unavailable", 503)
		return true
	}
	defer input.Close()
	stat, err := input.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > 8<<20 {
		http.Error(w, "result unavailable", 503)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.Copy(w, input)
	return true
}

func (s *resultStore) expire() {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !resultID.MatchString(strings.TrimPrefix(name, ".tmp-")) {
			continue
		}
		info, err := entry.Info()
		if err == nil && time.Since(info.ModTime()) >= s.ttl {
			_ = os.RemoveAll(filepath.Join(s.root, name))
		}
	}
}
