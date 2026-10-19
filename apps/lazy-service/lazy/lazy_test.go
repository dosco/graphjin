package lazy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// The test binary doubles as a real child HTTP process.
func TestChild(t *testing.T) {
	if os.Getenv("LAZY_TEST_CHILD") != "1" {
		return
	}
	http.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"ok":true}`) })
	http.HandleFunc("/health/idle", func(w http.ResponseWriter, r *http.Request) {
		_, err := os.Stat(os.Getenv("LAZY_TEST_BUSY"))
		fmt.Fprintf(w, `{"idle":%t}`, os.IsNotExist(err))
	})
	http.HandleFunc("/v1/work", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "done") })
	if err := http.ListenAndServe(os.Getenv("LAZY_TEST_ADDR"), nil); err != nil {
		os.Exit(1)
	}
}

func fixture(t *testing.T) *service {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	t.Setenv("LAZY_TEST_CHILD", "1")
	t.Setenv("LAZY_TEST_ADDR", addr)
	t.Setenv("LAZY_TEST_BUSY", filepath.Join(t.TempDir(), "busy"))
	s := &service{command: []string{os.Args[0], "-test.run=^TestChild$"}, upstream: "http://" + addr, startup: 2 * time.Second, idle: time.Millisecond, client: &http.Client{Timeout: 100 * time.Millisecond}}
	t.Cleanup(func() { s.mu.Lock(); s.stop(); s.mu.Unlock() })
	return s
}

func TestConcurrentWakeIdleAndCrashRecovery(t *testing.T) {
	s := fixture(t)
	h := s.handler()
	health := func() {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/health/ready", nil))
		if r.Code != 200 {
			t.Fatalf("health: %s", r.Body.String())
		}
	}
	for i := 0; i < 10; i++ {
		health()
	}
	if s.starts != 0 {
		t.Fatal("health checks woke the child")
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRecorder()
			h.ServeHTTP(r, httptest.NewRequest("GET", "/v1/work", nil))
			if r.Code != 200 || r.Body.String() != "done" {
				t.Errorf("work: %d %s", r.Code, r.Body.String())
			}
		}()
	}
	wg.Wait()
	if s.starts != 1 {
		t.Fatalf("concurrent startup spawned %d children", s.starts)
	}
	if err := os.WriteFile(os.Getenv("LAZY_TEST_BUSY"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.lastUsed = time.Now().Add(-time.Hour)
	s.mu.Unlock()
	s.expire()
	if s.cmd == nil {
		t.Fatal("stopped with asynchronous work or uncollected results")
	}
	_ = os.Remove(os.Getenv("LAZY_TEST_BUSY"))
	if err := s.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.lastUsed = time.Now().Add(-time.Hour)
	s.mu.Unlock()
	s.expire()
	if s.cmd == nil {
		t.Fatal("stopped with an active request")
	}
	s.release()
	s.mu.Lock()
	s.lastUsed = time.Now().Add(-time.Hour)
	s.mu.Unlock()
	s.expire()
	if s.cmd != nil {
		t.Fatal("did not release idle child")
	}
	health()
	if s.starts != 1 {
		t.Fatal("idle health woke the child")
	}
	if err := s.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.release()
	if s.starts != 2 {
		t.Fatal("did not restart after idle")
	}
	_ = s.cmd.Process.Kill()
	<-s.done
	if err := s.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.release()
	if s.starts != 3 {
		t.Fatal("did not recover from child crash")
	}
}

func TestStartupFailureDoesNotPoisonRetryOrLeakActiveLease(t *testing.T) {
	s := fixture(t)
	valid := s.command
	s.command = []string{"/missing-child"}
	if err := s.acquire(context.Background()); err == nil {
		t.Fatal("accepted missing command")
	}
	if s.active != 0 || s.cmd != nil {
		t.Fatal("leaked failed startup")
	}
	s.command = valid
	if err := s.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.release()
	if s.failed {
		t.Fatal("successful retry did not restore readiness")
	}
}

func TestActualHTTPProxyAndSleepingHealth(t *testing.T) {
	s := fixture(t)
	server := httptest.NewServer(s.handler())
	defer server.Close()
	response, err := http.Get(server.URL + "/health/ready")
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	_ = json.NewDecoder(response.Body).Decode(&status)
	response.Body.Close()
	if status["state"] != "sleeping" {
		t.Fatal(status)
	}
	response, err = http.Get(server.URL + "/v1/work")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "done" {
		t.Fatal(string(body))
	}
}

func TestDiskResultsDoNotWakeChildAndExpire(t *testing.T) {
	s := fixture(t)
	root := t.TempDir()
	s.results = &resultStore{root: root, ttl: time.Hour}
	id := "00000000-0000-4000-8000-000000000001"
	folder := filepath.Join(root, id)
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"status.json": `{"task_status":"success"}`, "result.json": `{"document":{"md_content":"saved"}}`} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		for _, path := range []string{"/v1/status/poll/" + id, "/v1/result/" + id} {
			r := httptest.NewRecorder()
			s.handler().ServeHTTP(r, httptest.NewRequest("GET", path, nil))
			if r.Code != 200 {
				t.Fatal(r.Code, r.Body.String())
			}
		}
	}
	if s.starts != 0 {
		t.Fatal("result reads woke processing child")
	}
	invalid := httptest.NewRecorder()
	s.handler().ServeHTTP(invalid, httptest.NewRequest("GET", "/v1/result/../../outside", nil))
	if invalid.Code != 404 || s.starts != 0 {
		t.Fatal("unsafe result path accepted")
	}
	outside := filepath.Join(root, "outside")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(folder, "result.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(folder, "result.json")); err != nil {
		t.Fatal(err)
	}
	escaped := httptest.NewRecorder()
	s.handler().ServeHTTP(escaped, httptest.NewRequest("GET", "/v1/result/"+id, nil))
	if escaped.Code != 503 || escaped.Body.String() == "private" {
		t.Fatal("result reader escaped its directory")
	}
	old := time.Now().Add(-2 * time.Hour)
	_ = os.Chtimes(folder, old, old)
	s.results.expire()
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Fatal("result not expired")
	}
	r := httptest.NewRecorder()
	s.handler().ServeHTTP(r, httptest.NewRequest("GET", "/v1/result/"+id, nil))
	if r.Code != 404 || s.starts != 0 {
		t.Fatal("expired lookup woke child", r.Code)
	}
}
