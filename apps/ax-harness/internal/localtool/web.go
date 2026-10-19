package localtool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/open-neko/openneko/apps/ax-harness/internal/agent"
)

// maxWebChars is the stored-text ceiling for one extracted page. The agent
// result layer keeps up to 100,000 characters inline and saves the rest.
const (
	maxWebBody      = 10 << 20
	maxWebChars     = 2_000_000
	maxWebRedirects = 5
	webTimeout      = 30 * time.Second
)

// Web is a GET-only fetch tool limited to an admin host allowlist.
type Web struct {
	hosts   []string
	client  *http.Client
	allowIP func(net.IP) bool
}

// OpenWeb takes a comma list of hosts. "*.example.com" matches subdomains
// and "*" matches any public host.
func OpenWeb(allowlist string) (*Web, error) {
	w := &Web{allowIP: publicIP}
	for _, host := range strings.Split(allowlist, ",") {
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			continue
		}
		if strings.ContainsAny(host, "/:@ ") || strings.Count(host, "*") > 1 || strings.Contains(host, "*") && host != "*" && !strings.HasPrefix(host, "*.") {
			return nil, fmt.Errorf("invalid web host %q", host)
		}
		w.hosts = append(w.hosts, host)
	}
	if len(w.hosts) == 0 {
		return nil, fmt.Errorf("the web host allowlist is empty")
	}
	// The sandbox egress proxy may sit on a private address. Every other
	// connection must reach a public address.
	proxy := ""
	if u, _ := http.ProxyFromEnvironment(&http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}}); u != nil {
		proxy = u.Host
		if u.Port() == "" {
			proxy = net.JoinHostPort(u.Hostname(), map[string]string{"http": "80", "https": "443"}[u.Scheme])
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true, TLSHandshakeTimeout: 10 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address == proxy {
				return dialer.DialContext(ctx, network, address)
			}
			public := *dialer
			public.Control = func(_, address string, _ syscall.RawConn) error {
				host, _, err := net.SplitHostPort(address)
				if ip := net.ParseIP(host); err != nil || ip == nil || !w.allowIP(ip) {
					return fmt.Errorf("web_fetch reaches public addresses only")
				}
				return nil
			}
			return public.DialContext(ctx, network, address)
		}}
	w.client = &http.Client{Transport: transport, Timeout: webTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxWebRedirects {
				return fmt.Errorf("too many redirects")
			}
			return w.check(req.URL)
		}}
	return w, nil
}

func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}

func (w *Web) check(u *url.URL) error {
	if u.Scheme != "https" && u.Scheme != "http" || u.User != nil || u.Hostname() == "" {
		return fmt.Errorf("web_fetch takes an http or https URL")
	}
	host := strings.ToLower(u.Hostname())
	for _, rule := range w.hosts {
		if rule == "*" || rule == host || strings.HasPrefix(rule, "*.") && strings.HasSuffix(host, rule[1:]) {
			return nil
		}
	}
	return fmt.Errorf("host %s is not on the web allowlist", host)
}

func (w *Web) Capability() agent.Capability {
	return agent.Capability{Name: "web_fetch", Version: "1", Origin: "web", Effect: "read",
		Description: "GET a web page or API response from an allowed host. HTML returns as plain text; JSON, XML and text return as received. Returns {url,status,content_type,content,truncated}. Allowed hosts: " + strings.Join(w.hosts, ", ") + ".",
		InputSchema: json.RawMessage(`{"type":"object","required":["url"],"properties":{"url":{"type":"string","minLength":1,"maxLength":4096}},"additionalProperties":false}`),
		Call:        w.fetch}
}

func (w *Web) fetch(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var input struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, fmt.Errorf("invalid web fetch")
	}
	target, err := url.Parse(input.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL")
	}
	if err := w.check(target); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "OpenNeko-Ax/1")
	req.Header.Set("Accept", "text/html, application/json, text/*;q=0.9, */*;q=0.1")
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWebBody+1))
	if err != nil {
		return nil, err
	}
	truncated := len(body) > maxWebBody
	if truncated {
		body = body[:maxWebBody]
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	var content string
	switch {
	case mediaType == "text/html" || mediaType == "application/xhtml+xml":
		content = htmlText(body)
	case strings.HasPrefix(mediaType, "text/") || mediaType == "application/json" || mediaType == "application/xml" ||
		strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") || mediaType == "" && utf8.Valid(body):
		content = strings.ToValidUTF8(string(body), "�")
	default:
		return nil, fmt.Errorf("web_fetch reads text only, not %s", mediaType)
	}
	if utf8.RuneCountInString(content) > maxWebChars {
		content, truncated = runePrefix(content, maxWebChars), true
	}
	return json.Marshal(struct {
		URL         string `json:"url"`
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		Content     string `json:"content"`
		Truncated   bool   `json:"truncated"`
	}{resp.Request.URL.String(), resp.StatusCode, mediaType, content, truncated})
}

var skipTags = map[string]bool{"script": true, "style": true, "noscript": true, "svg": true, "template": true, "head": true}
var blockTags = map[string]bool{"p": true, "div": true, "br": true, "li": true, "tr": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "section": true, "article": true, "pre": true, "table": true, "ul": true, "ol": true}

// htmlText keeps the visible text of a page, one block per line.
func htmlText(body []byte) string {
	var out strings.Builder
	tokens := html.NewTokenizer(bytes.NewReader(body))
	skip := 0
	for {
		kind := tokens.Next()
		switch kind {
		case html.ErrorToken:
			lines := strings.Split(out.String(), "\n")
			kept := lines[:0]
			for _, line := range lines {
				if line = strings.Join(strings.Fields(line), " "); line != "" {
					kept = append(kept, line)
				}
			}
			return strings.Join(kept, "\n")
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := tokens.TagName()
			if skipTags[string(name)] && kind == html.StartTagToken {
				skip++
			} else if blockTags[string(name)] {
				out.WriteByte('\n')
			}
		case html.EndTagToken:
			name, _ := tokens.TagName()
			if skipTags[string(name)] && skip > 0 {
				skip--
			} else if blockTags[string(name)] {
				out.WriteByte('\n')
			}
		case html.TextToken:
			if skip == 0 {
				out.Write(tokens.Text())
				out.WriteByte(' ')
			}
		}
	}
}
