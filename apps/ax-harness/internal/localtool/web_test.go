package localtool

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWebFetchStaysOnAllowlist(t *testing.T) {
	var other *httptest.Server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
				t.Errorf("credentials sent")
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>T</title><script>var x=1</script></head><body><h1>Prices</h1><p>Widget <b>$4</b></p><svg/><p>Done</p></body></html>`))
		case "/data":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/image":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G'})
		case "/away":
			http.Redirect(w, r, other.URL+"/page", http.StatusFound)
		}
	}))
	defer server.Close()
	other = httptest.NewServer(http.NotFoundHandler())
	defer other.Close()
	// Both test servers listen on 127.0.0.1; name them by host to test the allowlist.
	rename := func(raw, host string) string {
		u, _ := url.Parse(raw)
		return "http://" + host + ":" + u.Port()
	}
	allowed, blocked := rename(server.URL, "localhost"), rename(other.URL, "127.0.0.1")
	other.URL = blocked
	web, err := OpenWeb("localhost, *.example.com")
	if err != nil {
		t.Fatal(err)
	}
	fetch := func(target string) (map[string]any, error) {
		input, _ := json.Marshal(map[string]string{"url": target})
		raw, err := web.Capability().Call(context.Background(), input)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return out, err
	}
	if _, err := fetch(allowed + "/page"); err == nil || !strings.Contains(err.Error(), "public addresses") {
		t.Fatalf("private address admitted: %v", err)
	}
	web.allowIP = func(ip net.IP) bool { return ip.IsLoopback() }
	page, err := fetch(allowed + "/page")
	if err != nil || page["content"] != "Prices\nWidget $4\nDone" || page["status"] != float64(200) {
		t.Fatalf("page=%v err=%v", page, err)
	}
	if data, err := fetch(allowed + "/data"); err != nil || data["content"] != `{"ok":true}` {
		t.Fatalf("data=%v err=%v", data, err)
	}
	if _, err := fetch(allowed + "/image"); err == nil {
		t.Fatal("binary content admitted")
	}
	if _, err := fetch(blocked + "/page"); err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("host outside the allowlist admitted: %v", err)
	}
	if _, err := fetch(allowed + "/away"); err == nil || !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("redirect off the allowlist admitted: %v", err)
	}
	for _, bad := range []string{"file:///etc/passwd", "http://user:pw@localhost/"} {
		if _, err := fetch(bad); err == nil {
			t.Fatalf("admitted %s", bad)
		}
	}
	for _, bad := range []string{"", "a.com/x", "*a.com", "a.*.com"} {
		if _, err := OpenWeb(bad); err == nil {
			t.Fatalf("allowlist %q admitted", bad)
		}
	}
	if web.check(&url.URL{Scheme: "https", Host: "api.example.com"}) != nil || web.check(&url.URL{Scheme: "https", Host: "example.com.evil.io"}) == nil {
		t.Fatal("wildcard match is wrong")
	}
}
