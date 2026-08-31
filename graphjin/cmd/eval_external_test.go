package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/serv/v3"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// startExternalTestServer boots one real world that records the tool calls made
// against it, exactly as `env serve --external` does.
func startExternalTestServer(t *testing.T) (*envServer, *externalServer, *httptest.Server, func()) {
	t.Helper()
	project := t.TempDir()
	if err := extractDefaultDemo(project); err != nil {
		t.Fatal(err)
	}
	originalPath, originalConf, originalDB, originalOpened := cpath, conf, db, dbOpened
	t.Setenv("GO_ENV", "dev")

	recorder := newMCPToolRecorder()
	environment := evalEnvironment{MCPRecorder: recorder.record}
	pool, err := newEvalInstancePool(context.Background(), func(int) evalEnvironment { return environment },
		gjeval.EnvSpec{
			Target: gjeval.TargetDemo, ConfigPath: project, Seed: 23, FreezeTime: "2026-08-01T12:00:00Z",
		}, 1)
	if err != nil {
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
		t.Fatal(err)
	}
	server := &envServer{
		pool: pool, suite: envTestSuite(t), profile: gjeval.RewardProfileRL, side: "train",
		byID: map[string]gjeval.Task{}, bySlug: map[string]gjeval.Task{},
		recorders: map[gjeval.Instance]*mcpToolRecorder{pool.instances[0]: recorder},
	}
	if err := server.indexTasks(); err != nil {
		t.Fatal(err)
	}
	harness := newExternalServer(server, 2*time.Minute)
	// Mounted on a real listener, because an external agent reaches the world
	// through this server and nowhere else: what it is handed has to be an
	// address that resolves to here.
	mux := http.NewServeMux()
	harness.register(mux)
	front := httptest.NewServer(mux)
	return server, harness, front, func() {
		front.Close()
		_ = pool.Close()
		cpath, conf, db, dbOpened = originalPath, originalConf, originalDB, originalOpened
	}
}

func startExternalEpisode(t *testing.T, front *httptest.Server, slug string) externalStartResponse {
	t.Helper()
	body, err := json.Marshal(externalStartRequest{Slug: slug})
	if err != nil {
		t.Fatal(err)
	}
	response, err := front.Client().Post(front.URL+"/external/episodes", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close() //nolint:errcheck
	var start externalStartResponse
	if err := json.NewDecoder(response.Body).Decode(&start); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("start status = %d: %+v", response.StatusCode, start)
	}
	return start
}

func submitExternalAnswer(t *testing.T, front *httptest.Server, id, answer string) (int, externalAnswerResponse) {
	t.Helper()
	body, err := json.Marshal(externalAnswerRequest{Answer: answer})
	if err != nil {
		t.Fatal(err)
	}
	response, err := front.Client().Post(
		front.URL+"/external/episodes/"+id+"/answer", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close() //nolint:errcheck
	var graded externalAnswerResponse
	_ = json.NewDecoder(response.Body).Decode(&graded)
	return response.StatusCode, graded
}

// connectMCP opens an MCP session against the world, the way an external agent
// would.
func connectMCP(t *testing.T, start externalStartResponse) *sdk.ClientSession {
	t.Helper()
	transport := &sdk.StreamableClientTransport{
		Endpoint:             start.MCPURL,
		HTTPClient:           &http.Client{Timeout: 30 * time.Second},
		DisableStandaloneSSE: true,
	}
	client := sdk.NewClient(&sdk.Implementation{Name: "external-harness-test", Version: "0.0.0"}, nil)
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("could not connect to %s: %v", start.MCPURL, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// An agent this harness does not host does the work over MCP and submits an
// answer. What makes it a measurement rather than a self-report is that the
// server recorded every call, so the same method and behavior rules apply.
func TestExternalAgentIsGradedOnWhatItActuallyDid(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	_, _, front, stop := startExternalTestServer(t)
	defer stop()

	start := startExternalEpisode(t, front, "count-accounts")
	if start.Prompt == "" || start.MCPURL == "" || start.GraphQLURL == "" {
		t.Fatalf("an external agent needs the task and somewhere to do it: %+v", start)
	}
	// The caveat has to travel with the score, not live in documentation nobody
	// reads next to a number they are about to compare with a benchmark row.
	if !strings.Contains(start.Note, "efficiency") {
		t.Fatalf("the reward caveat was not stated: %q", start.Note)
	}

	session := connectMCP(t, start)
	ctx := context.Background()
	// The same discovery the protocol requires of any caller.
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name: "query_catalog", Arguments: map[string]any{"id": "table:app:main.accounts"},
	}); err != nil {
		t.Fatalf("query_catalog: %v", err)
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name: "execute_graphql", Arguments: map[string]any{"query": "query { accounts { count_id } }"},
	})
	if err != nil {
		t.Fatalf("execute_graphql: %v", err)
	}
	count := countFromToolResult(t, result)

	code, graded := submitExternalAnswer(t, front, start.EpisodeID,
		"There are "+count+" accounts.")
	if code != http.StatusOK {
		t.Fatalf("answer status = %d: %+v", code, graded)
	}
	if graded.ToolCalls < 2 {
		t.Fatalf("the server did not record the work: %d calls", graded.ToolCalls)
	}
	if !graded.Pass {
		t.Fatalf("an agent that did the work and answered correctly failed: %+v", graded.Score)
	}
	if graded.Reward <= 0 {
		t.Fatalf("reward = %f", graded.Reward)
	}
}

// The shortcut this whole surface has to refuse: submitting the right answer
// without touching the database. Without the recording there would be nothing
// to tell the two apart.
func TestExternalAnswerWithoutDoingTheWorkScoresZero(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	_, _, front, stop := startExternalTestServer(t)
	defer stop()

	// Learn the true answer honestly in one episode...
	start := startExternalEpisode(t, front, "count-accounts")
	session := connectMCP(t, start)
	ctx := context.Background()
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name: "query_catalog", Arguments: map[string]any{"id": "table:app:main.accounts"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name: "execute_graphql", Arguments: map[string]any{"query": "query { accounts { count_id } }"},
	})
	if err != nil {
		t.Fatal(err)
	}
	count := countFromToolResult(t, result)
	if _, graded := submitExternalAnswer(t, front, start.EpisodeID, "There are "+count+" accounts."); !graded.Pass {
		t.Fatalf("the honest episode must pass or the contrast means nothing: %+v", graded.Score)
	}

	// ...then submit it in a second episode without doing anything at all.
	cheat := startExternalEpisode(t, front, "count-accounts")
	code, graded := submitExternalAnswer(t, front, cheat.EpisodeID, "There are "+count+" accounts.")
	if code != http.StatusOK {
		t.Fatalf("answer status = %d", code)
	}
	if graded.ToolCalls != 0 {
		t.Fatalf("expected no recorded work, got %d calls", graded.ToolCalls)
	}
	if graded.Pass {
		t.Fatalf("a correct answer with no work behind it passed: %+v", graded.Score)
	}
}

// An abandoned episode gives its world back, or a pool drains one episode at a
// time until nothing is left to run on.
func TestExternalEpisodeCanBeAbandoned(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	server, _, front, stop := startExternalTestServer(t)
	defer stop()

	start := startExternalEpisode(t, front, "count-accounts")
	request, err := http.NewRequest(http.MethodDelete, front.URL+"/external/episodes/"+start.EpisodeID, nil)
	if err != nil {
		t.Fatal(err)
	}
	abandoned, err := front.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = abandoned.Body.Close()
	if abandoned.StatusCode != http.StatusOK {
		t.Fatalf("abandon status = %d", abandoned.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	instance, err := server.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("the world was not given back: %v", err)
	}
	_ = server.pool.Release(instance)

	// And answering afterwards is refused rather than silently graded twice.
	if code, _ := submitExternalAnswer(t, front, start.EpisodeID, "anything"); code != http.StatusGone {
		t.Fatalf("answering an abandoned episode returned %d, want %d", code, http.StatusGone)
	}
}

// countFromToolResult digs the count out of whatever shape the tool returned.
func countFromToolResult(t *testing.T, result *sdk.CallToolResult) string {
	t.Helper()
	for _, content := range result.Content {
		text, ok := content.(*sdk.TextContent)
		if !ok {
			continue
		}
		var payload struct {
			Data struct {
				Accounts []struct {
					CountID json.Number `json:"count_id"`
				} `json:"accounts"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(text.Text), &payload); err == nil && len(payload.Data.Accounts) != 0 {
			return payload.Data.Accounts[0].CountID.String()
		}
	}
	t.Fatalf("no count in the tool result: %+v", result.Content)
	return ""
}

// The account of what happened has to be in the exact shape the scorer reads.
// A call with no summary counts as having succeeded, so a failed call that did
// not say so would grade a run of errors as competent work.
func TestRecordedCallsBecomeAScoreableAccount(t *testing.T) {
	response := responseFromMCPEvents(gjagent.StatusAnswered, "There are 4 accounts.", []serv.MCPToolEvent{
		{Tool: "query_catalog", Arguments: map[string]any{"id": "table:accounts"}},
		{Tool: "execute_graphql", Arguments: map[string]any{"query": "query { accounts { count_id } }"}},
		{Tool: "execute_graphql", Arguments: map[string]any{"query": "query { nope { x } }"}, IsError: true},
	})
	if response.Status != gjagent.StatusAnswered || response.Answer != "There are 4 accounts." {
		t.Fatalf("unexpected response: %+v", response)
	}
	actions, ok := response.Actions.([]map[string]any)
	if !ok || len(actions) != 3 {
		t.Fatalf("actions = %+v", response.Actions)
	}
	// The query is what every method rule matches against.
	args, _ := actions[1]["args"].(map[string]any)
	if args["query"] != "query { accounts { count_id } }" {
		t.Fatalf("the query did not survive: %+v", actions[1])
	}
	failed, _ := actions[2]["summary"].(map[string]any)
	if failed["error_count"] != 1 {
		t.Fatalf("a failed call was reported as successful: %+v", actions[2])
	}
	succeeded, _ := actions[1]["summary"].(map[string]any)
	if succeeded["error_count"] != 0 {
		t.Fatalf("a successful call was reported as failed: %+v", actions[1])
	}
}

// The address an external agent is handed has to be one it can reach.
//
// A/B: on master mcp_url is the leased world's own httptest listener — a
// loopback port that exists only inside the serving process. An agent on the
// same machine got away with it; a container hands out a URL that resolves to
// the agent's own loopback and nothing works. Rewriting the string would make
// it honest and still route nowhere, so the environment carries the traffic
// itself.
func TestExternalWorldIsReachableThroughTheServerThatLeasedIt(t *testing.T) {
	if testing.Short() {
		t.Skip("embedded service integration")
	}
	_, harness, front, stop := startExternalTestServer(t)
	defer stop()

	start := startExternalEpisode(t, front, "count-accounts")
	if !strings.HasPrefix(start.MCPURL, front.URL+"/") {
		t.Fatalf("mcp_url = %q, which is not on the server the agent reached (%s)", start.MCPURL, front.URL)
	}
	if !strings.HasPrefix(start.GraphQLURL, front.URL+"/") {
		t.Fatalf("graphql_url = %q", start.GraphQLURL)
	}
	if !strings.Contains(start.MCPURL, start.EpisodeID) {
		t.Fatalf("the path must name the lease it routes for: %q", start.MCPURL)
	}

	// And it is not merely well-formed: a real MCP session runs over it, end to
	// end, and the work is recorded and graded.
	session := connectMCP(t, start)
	ctx := context.Background()
	if _, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name: "query_catalog", Arguments: map[string]any{"id": "table:app:main.accounts"},
	}); err != nil {
		t.Fatalf("query_catalog through the proxy: %v", err)
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{
		Name: "execute_graphql", Arguments: map[string]any{"query": "query { accounts { count_id } }"},
	})
	if err != nil {
		t.Fatalf("execute_graphql through the proxy: %v", err)
	}
	count := countFromToolResult(t, result)

	// GraphQL over the same path, since an agent may prefer it.
	body, err := json.Marshal(map[string]any{"query": "query { accounts { count_id } }"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, start.GraphQLURL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range start.Headers {
		request.Header.Set(key, value)
	}
	graphql, err := front.Client().Do(request)
	if err != nil {
		t.Fatalf("graphql through the proxy: %v", err)
	}
	defer graphql.Body.Close() //nolint:errcheck
	if graphql.StatusCode != http.StatusOK {
		t.Fatalf("graphql status = %d", graphql.StatusCode)
	}

	code, graded := submitExternalAnswer(t, front, start.EpisodeID, "There are "+count+" accounts.")
	if code != http.StatusOK || !graded.Pass || graded.Reward <= 0 {
		t.Fatalf("work done through the advertised URL was not graded: %d %+v", code, graded)
	}

	// The lease is the authorization. Once it ends, the path routes nowhere.
	gone, err := front.Client().Get(start.GraphQLURL)
	if err != nil {
		t.Fatal(err)
	}
	defer gone.Body.Close() //nolint:errcheck
	if gone.StatusCode != http.StatusGone {
		t.Fatalf("a finished episode's world answered %d, want %d", gone.StatusCode, http.StatusGone)
	}
	if _, live := harness.lookup(start.EpisodeID); live {
		t.Fatal("a graded episode is still holding its world")
	}
}

// --advertise-url is for when the Host an agent arrives with is not an address
// it could dial again — a proxy that rewrites it, or a URL passed out of band.
func TestAdvertisedBaseNamesSomewhereReachable(t *testing.T) {
	harness := &externalServer{env: &envServer{listenAddr: "127.0.0.1:8090"}}

	request := httptest.NewRequest(http.MethodPost, "/external/episodes", nil)
	request.Host = "gj-env-7.cluster.local:8090"
	base, reachable := harness.advertisedBase(request)
	if base != "http://gj-env-7.cluster.local:8090" || !reachable {
		t.Fatalf("the Host that actually resolved here is the default: %q", base)
	}

	harness.env.advertiseURL = "https://envs.example.com/gj/"
	base, reachable = harness.advertisedBase(request)
	if base != "https://envs.example.com/gj" || !reachable {
		t.Fatalf("--advertise-url must win and lose its trailing slash: %q", base)
	}

	// Nothing to go on: say so rather than handing out a loopback address that
	// only works for whoever is already inside.
	harness.env.advertiseURL = ""
	request.Host = ""
	base, reachable = harness.advertisedBase(request)
	if base != "http://127.0.0.1:8090" || reachable {
		t.Fatalf("base = %q reachable = %v", base, reachable)
	}
}
