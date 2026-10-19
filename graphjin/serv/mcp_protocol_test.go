package serv

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	"github.com/dosco/graphjin/core/v3"
	compatmcp "github.com/dosco/graphjin/serv/v3/internal/mcpcompat/mcp"
	compatserver "github.com/dosco/graphjin/serv/v3/internal/mcpcompat/server"
	officialmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpRequestRecorder struct {
	mu      sync.Mutex
	methods []string
	metas   []map[string]any
	next    http.Handler
}

func (h *mcpRequestRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil && r.Method == http.MethodPost {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		var message struct {
			Method string `json:"method"`
			Params struct {
				Meta map[string]any `json:"_meta"`
			} `json:"params"`
		}
		if json.Unmarshal(body, &message) == nil && message.Method != "" {
			h.mu.Lock()
			h.methods = append(h.methods, message.Method)
			h.metas = append(h.metas, message.Params.Meta)
			h.mu.Unlock()
		}
	}
	ctx := r.Context()
	if userID := strings.TrimSpace(r.Header.Get("X-Test-User")); userID != "" {
		ctx = context.WithValue(ctx, core.UserIDKey, userID)
		ctx = context.WithValue(ctx, core.UserRoleKey, "user")
		ctx = context.WithValue(ctx, core.IdentityVarsKey, map[string]interface{}{
			"user_id": userID, "account_id": "acct_1",
		})
	}
	h.next.ServeHTTP(w, r.WithContext(ctx))
}

func (h *mcpRequestRecorder) snapshot() ([]string, []map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.methods...), append([]map[string]any(nil), h.metas...)
}

func newAxHTTPClient(t *testing.T, endpoint, userID string) (*ax.AxMCPClient, *ax.AxMCPStreamableHTTPTransport) {
	t.Helper()
	options := map[string]ax.Value{}
	options["ssrfProtection"] = map[string]ax.Value{
		"requireHttps":   false,
		"allowLocalhost": true,
	}
	if userID != "" {
		options["headers"] = map[string]ax.Value{"X-Test-User": userID}
	}
	transport, err := ax.NewAxMCPStreamableHTTPTransport(endpoint, options)
	if err != nil {
		t.Fatalf("new Ax MCP transport: %v", err)
	}
	client := ax.NewAxMCPClient(transport, map[string]ax.Value{"era": "auto"})
	t.Cleanup(func() { _ = client.Close() })
	return client, transport
}

func TestMCPModernAxDiscoveryAndCacheMetadata(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Core.Watches.Enabled = false
	ms = ms.service.newMCPServerWithContext(context.Background())
	recorder := &mcpRequestRecorder{next: compatserver.NewDualStreamableHTTPServer(ms.srv)}
	httpServer := httptest.NewServer(recorder)
	t.Cleanup(httpServer.Close)

	client, transport := newAxHTTPClient(t, httpServer.URL, "user_1")
	if err := client.Init(); err != nil {
		t.Fatalf("Ax MCP init: %v", err)
	}
	if client.GetEra() != "modern" || client.ProtocolVersion() != "2026-07-28" {
		t.Fatalf("Ax negotiation era=%q version=%q", client.GetEra(), client.ProtocolVersion())
	}
	if transport.SessionID != "" {
		t.Fatalf("modern transport retained session ID %q", transport.SessionID)
	}
	discovery, err := client.Discover()
	if err != nil {
		t.Fatalf("server/discover: %v", err)
	}
	snapshot, err := client.InspectCatalog(false)
	if err != nil {
		t.Fatalf("inspect Ax catalog: %v", err)
	}
	serverInfo := snapshot.ServerInfo
	if serverInfo["name"] != "graphjin" {
		t.Fatalf("server identity = %#v discovery=%#v", serverInfo, discovery)
	}
	capabilities, _ := discovery["capabilities"].(map[string]any)
	if _, advertised := capabilities["sampling"]; advertised {
		t.Fatalf("sampling capability advertised: %#v", capabilities)
	}
	tools, err := client.ListTools("")
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	if tools["resultType"] != "complete" || tools["cacheScope"] != "private" {
		t.Fatalf("modern tools/list metadata = %#v", tools)
	}
	if _, ok := tools["ttlMs"]; !ok {
		t.Fatalf("modern tools/list omitted ttlMs: %#v", tools)
	}
	if transport.LastHeaders["MCP-Protocol-Version"] != "2026-07-28" || transport.LastHeaders["Mcp-Method"] != "tools/list" {
		t.Fatalf("modern required headers = %#v", transport.LastHeaders)
	}
	if _, ok := transport.LastHeaders["MCP-Session-Id"]; ok {
		t.Fatalf("modern request sent a session header: %#v", transport.LastHeaders)
	}

	methods, metas := recorder.snapshot()
	for _, method := range methods {
		if method == "initialize" {
			t.Fatalf("modern Ax client sent initialize: %v", methods)
		}
	}
	if len(methods) == 0 || methods[0] != "server/discover" {
		t.Fatalf("modern negotiation methods = %v", methods)
	}
	for i, method := range methods {
		meta := metas[i]
		if meta["io.modelcontextprotocol/protocolVersion"] != "2026-07-28" {
			t.Fatalf("%s request metadata = %#v", method, meta)
		}
	}
}

func TestMCPLegacyInitializeSessionAndSamplingRemoval(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Core.Watches.Enabled = false
	ms = ms.service.newMCPServerWithContext(context.Background())
	httpServer := httptest.NewServer(compatserver.NewDualStreamableHTTPServer(ms.srv))
	t.Cleanup(httpServer.Close)

	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy-test","version":"1"}}}`
	req, _ := http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(initialize))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacy initialize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("legacy initialize status=%d body=%s", resp.StatusCode, body)
	}
	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("legacy initialize did not establish a session")
	}
	body, _ := io.ReadAll(resp.Body)
	if bytes.Contains(body, []byte(`"sampling"`)) {
		t.Fatalf("legacy initialize advertised sampling: %s", body)
	}

	initialized := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req, _ = http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(initialized))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacy initialized notification: %v", err)
	}
	_ = resp.Body.Close()

	listTools := `{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}`
	req, _ = http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(listTools))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacy tools/list: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"tools"`)) {
		t.Fatalf("legacy tools/list status=%d body=%s", resp.StatusCode, body)
	}

	sampling := `{"jsonrpc":"2.0","id":2,"method":"sampling/createMessage","params":{}}`
	req, _ = http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(sampling))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacy sampling request: %v", err)
	}
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`"code":-32601`)) {
		t.Fatalf("sampling request did not return method-not-found: %s", body)
	}
}

func TestMCPModernPOSTCancellationReachesToolHandler(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	srv := compatserver.NewMCPServer("graphjin", "test")
	srv.AddTool(compatmcp.NewTool("wait_for_cancel"), func(ctx context.Context, _ compatmcp.CallToolRequest) (*compatmcp.CallToolResult, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	})
	httpServer := httptest.NewServer(compatserver.NewDualStreamableHTTPServer(srv))
	t.Cleanup(httpServer.Close)

	ctx, cancel := context.WithCancel(context.Background())
	body := `{"jsonrpc":"2.0","id":"cancel","method":"tools/call","params":{"name":"wait_for_cancel","arguments":{},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"cancel-test","version":"1"}}}}`
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, httpServer.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", "wait_for_cancel")
	requestDone := make(chan struct{})
	go func() {
		resp, _ := http.DefaultClient.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		close(requestDone)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("modern tool handler did not start")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled modern POST did not cancel its tool handler")
	}
	select {
	case <-requestDone:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled modern POST did not return")
	}
}

func TestMCPModernMultiRoundTripResult(t *testing.T) {
	srv := compatserver.NewMCPServer("graphjin", "test")
	officialmcp.AddTool[map[string]any, any](
		srv.SDK(),
		&officialmcp.Tool{Name: "confirm_action"},
		func(context.Context, *officialmcp.CallToolRequest, map[string]any) (*officialmcp.CallToolResult, any, error) {
			return &officialmcp.CallToolResult{
				InputRequests: officialmcp.InputRequestMap{
					"confirm": &officialmcp.ElicitParams{Message: "Continue?"},
				},
				RequestState: "signed-request-state",
			}, nil, nil
		},
	)
	httpServer := httptest.NewServer(compatserver.NewDualStreamableHTTPServer(srv))
	t.Cleanup(httpServer.Close)

	meta := `{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"mrtr-test","version":"1"}}`
	body := `{"jsonrpc":"2.0","id":"mrtr","method":"tools/call","params":{"name":"confirm_action","arguments":{},"_meta":` + meta + `}}`
	req, _ := http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", "confirm_action")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("modern MRTR request: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("modern MRTR status=%d body=%s", resp.StatusCode, data)
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "data:") {
				data = []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				break
			}
		}
	}
	var envelope struct {
		Result struct {
			ResultType    string         `json:"resultType"`
			InputRequests map[string]any `json:"inputRequests"`
			RequestState  string         `json:"requestState"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode modern MRTR response: %v: %s", err, data)
	}
	if envelope.Result.ResultType != "input_required" || envelope.Result.RequestState != "signed-request-state" {
		t.Fatalf("unexpected modern MRTR result: %#v", envelope.Result)
	}
	if _, ok := envelope.Result.InputRequests["confirm"]; !ok {
		t.Fatalf("modern MRTR result omitted confirm input request: %s", data)
	}
}

func TestMCPModernRequiresHeadersAndPerRequestMetadata(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{})
	ms.service.conf.Core.Watches.Enabled = false
	ms = ms.service.newMCPServerWithContext(context.Background())
	httpServer := httptest.NewServer(compatserver.NewDualStreamableHTTPServer(ms.srv))
	t.Cleanup(httpServer.Close)

	completeMeta := `{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"validation-test","version":"1"}}`
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":` + completeMeta + `}}`
	req, _ := http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("modern missing-header request: %v", err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !bytes.Contains(data, []byte("missing required Mcp-Method header")) {
		t.Fatalf("modern missing-header status=%d body=%s", resp.StatusCode, data)
	}

	body = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	req, _ = http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "tools/list")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("modern missing-metadata request: %v", err)
	}
	data, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !bytes.Contains(data, []byte("clientCapabilities")) {
		t.Fatalf("modern missing-metadata status=%d body=%s", resp.StatusCode, data)
	}

	body = `{"jsonrpc":"2.0","id":3,"method":"sampling/createMessage","params":{"_meta":` + completeMeta + `}}`
	req, _ = http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "sampling/createMessage")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("modern removed sampling request: %v", err)
	}
	data, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || !bytes.Contains(data, []byte(`"code":-32601`)) {
		t.Fatalf("modern removed sampling status=%d body=%s", resp.StatusCode, data)
	}
}

func TestMCPAxExactWatchSubscriptionAndRecovery(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("init artifacts: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	ctx := artifactUserCtx("user_1")
	row, err := newWatchControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{"name": "mcp_exact", "query": cursorOrdersWatchQuery("mcp_exact")},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	watchID := row["id"].(string)
	uri := watchEventsUnseenResourceURI(watchID)

	ms := svc.newMCPServerWithContext(context.Background())
	if err := ms.subscribeWatchResource(artifactUserCtx("user_1"), uri); err != nil {
		t.Fatalf("validate owner subscription: %v", err)
	}
	recorder := &mcpRequestRecorder{next: compatserver.NewDualStreamableHTTPServer(ms.srv)}
	httpServer := httptest.NewServer(recorder)
	t.Cleanup(httpServer.Close)

	client, _ := newAxHTTPClient(t, httpServer.URL, "user_1")
	if err := client.Init(); err != nil {
		t.Fatalf("initialize Ax watch client: %v", err)
	}
	// Prime the ownership set before AxMCPEventSource opens its listener. This
	// ensures its first subscriptions/listen request carries the concrete URI.
	if _, err := client.AcquireResourceSubscription(uri, "bootstrap"); err != nil {
		t.Fatalf("prime Ax watch subscription: %v", err)
	}
	events := make(chan ax.AxEventEnvelope, 2)
	source := ax.NewAxMCPEventSource(client, "graphjin", "user_1", "trusted", []string{uri})
	if err := source.Start(func(event ax.AxEventEnvelope) error {
		events <- event
		return nil
	}); err != nil {
		methods, _ := recorder.snapshot()
		t.Fatalf("start Ax MCP event source: %v request_count=%d", err, len(methods))
	}
	t.Cleanup(func() { _ = source.Close() })

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO "_graphjin_watch_events" (id, watch_id, data_hash, owner_id, account_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"evt_mcp_exact", watchID, "hash", "user_1", "acct_1", now, now); err != nil {
		t.Fatalf("insert watch event: %v", err)
	}
	if err := ms.srv.ResourceUpdated(context.Background(), uri); err != nil {
		t.Fatalf("publish resource update: %v", err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Subject == uri {
				goto notified
			}
		case <-deadline:
			t.Fatal("timed out waiting for exact watch notification")
		}
	}

notified:

	read, err := client.ReadResource(uri)
	if err != nil {
		t.Fatalf("durable resource reread: %v", err)
	}
	data, _ := json.Marshal(read)
	if !bytes.Contains(data, []byte("evt_mcp_exact")) {
		t.Fatalf("resource reread did not recover durable event: %s", data)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("disconnect Ax watch client: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO "_graphjin_watch_events" (id, watch_id, data_hash, owner_id, account_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"evt_mcp_reconnect", watchID, "hash-reconnect", "user_1", "acct_1", now, now); err != nil {
		t.Fatalf("insert disconnected watch event: %v", err)
	}
	reconnected, _ := newAxHTTPClient(t, httpServer.URL, "user_1")
	if err := reconnected.Init(); err != nil {
		t.Fatalf("reinitialize Ax watch client: %v", err)
	}
	read, err = reconnected.ReadResource(uri)
	if err != nil {
		t.Fatalf("resource reread after reconnect: %v", err)
	}
	data, _ = json.Marshal(read)
	if !bytes.Contains(data, []byte("evt_mcp_reconnect")) {
		t.Fatalf("reconnect reread did not recover disconnected event: %s", data)
	}

	assertModernListenRejected(t, httpServer.URL, "user_2", uri, "watch subscription denied")
	assertModernListenRejected(t, httpServer.URL, "user_1", WatchEventsUnseenResourceURI, "concrete GraphJin watch URI")

	aggregate, _ := newAxHTTPClient(t, httpServer.URL, "user_1")
	if err := aggregate.Init(); err != nil {
		t.Fatalf("initialize aggregate-read client: %v", err)
	}
	if _, err := aggregate.ReadResource(WatchEventsUnseenResourceURI); err != nil {
		t.Fatalf("aggregate resource should remain readable: %v", err)
	}
}

func assertModernListenRejected(t *testing.T, endpoint, userID, uri, message string) {
	t.Helper()
	meta := `{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"negative-subscription-test","version":"1"}}`
	body := `{"jsonrpc":"2.0","id":"negative-listen","method":"subscriptions/listen","params":{"notifications":{"resourceSubscriptions":["` + uri + `"]},"_meta":` + meta + `}}`
	req, _ := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "subscriptions/listen")
	req.Header.Set("X-Test-User", userID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rejected subscriptions/listen request: %v", err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Contains(data, []byte(message)) {
		t.Fatalf("subscriptions/listen for %s was not rejected with %q: status=%d body=%s", uri, message, resp.StatusCode, data)
	}
}

func TestWatchEventsUnseenResourceURIRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		watchID string
		suffix  string
	}{
		{watchID: "watch:0123456789abcdef", suffix: "watch%3A0123456789abcdef"},
		{watchID: "custom/watch id", suffix: "custom%2Fwatch%20id"},
	} {
		uri := watchEventsUnseenResourceURI(tc.watchID)
		if !strings.HasSuffix(uri, "/"+tc.suffix) {
			t.Fatalf("watch URI %q does not have encoded suffix %q", uri, tc.suffix)
		}
		got, ok := watchIDFromUnseenResourceURI(uri)
		if !ok || got != tc.watchID {
			t.Fatalf("watch URI round trip %q -> %q ok=%v", uri, got, ok)
		}
	}
	if _, ok := watchIDFromUnseenResourceURI("graphjin://watch-events/other"); ok {
		t.Fatal("unrelated resource URI should not parse as a watch-events resource")
	}
}

func TestMCPLegacyExactWatchSubscription(t *testing.T) {
	db, svc := newSQLiteWatchService(t, 20)
	if err := svc.initArtifactsBeforeCore(); err != nil {
		t.Fatalf("init artifacts: %v", err)
	}
	startSQLiteWatchCore(t, svc, db)
	ctx := artifactUserCtx("user_1")
	row, err := newWatchControlPlane(svc).mutateRow(ctx, core.ManagedMutationRoot{
		Table: watchesRootTable, Operation: "insert",
		Input: map[string]any{"name": "legacy_mcp_exact", "query": cursorOrdersWatchQuery("legacy_mcp_exact")},
	})
	if err != nil {
		t.Fatalf("insert watch: %v", err)
	}
	uri := watchEventsUnseenResourceURI(row["id"].(string))
	ms := svc.newMCPServerWithContext(context.Background())
	httpServer := httptest.NewServer(&mcpRequestRecorder{next: compatserver.NewDualStreamableHTTPServer(ms.srv)})
	t.Cleanup(httpServer.Close)

	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"legacy-watch-test","version":"1"}}}`
	req, _ := http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(initialize))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("X-Test-User", "user_1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacy initialize: %v", err)
	}
	sessionID := resp.Header.Get("Mcp-Session-Id")
	_ = resp.Body.Close()
	if sessionID == "" {
		t.Fatal("legacy watch client did not establish a session")
	}

	initialized := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req, _ = http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(initialized))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("X-Test-User", "user_1")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacy initialized notification: %v", err)
	}
	_ = resp.Body.Close()

	subscribe := `{"jsonrpc":"2.0","id":2,"method":"resources/subscribe","params":{"uri":"` + uri + `"}}`
	req, _ = http.NewRequest(http.MethodPost, httpServer.URL, strings.NewReader(subscribe))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	req.Header.Set("X-Test-User", "user_1")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacy resource subscribe: %v", err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || bytes.Contains(data, []byte(`"error"`)) {
		t.Fatalf("legacy resource subscribe status=%d body=%s", resp.StatusCode, data)
	}

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	req, _ = http.NewRequestWithContext(streamCtx, http.MethodGet, httpServer.URL, nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("Mcp-Protocol-Version", "2025-11-25")
	req.Header.Set("X-Test-User", "user_1")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("legacy notification stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	events := make(chan string, 4)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				events <- strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
	}()
	if err := ms.srv.ResourceUpdated(context.Background(), uri); err != nil {
		t.Fatalf("publish legacy resource update: %v", err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case event := <-events:
			if strings.Contains(event, `"notifications/resources/updated"`) && strings.Contains(event, uri) {
				cancelStream()
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for legacy exact resource notification")
		}
	}
}
