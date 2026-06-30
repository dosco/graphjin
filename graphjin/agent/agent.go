package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	ax "github.com/ax-llm/ax/packages/go"
	axgoja "github.com/ax-llm/ax/packages/go/runtime/goja"
	"github.com/dosco/graphjin/core/v3"
)

const (
	ModeSafe          = "safe"
	ModeDiscoveryOnly = "discovery_only"
	ModeRawAllowed    = "raw_allowed"

	StatusAnswered           = "answered"
	StatusNeedsClarification = "needs_clarification"
	StatusBlocked            = "blocked"
	StatusError              = "error"

	defaultProvider       = "openai"
	defaultAPIKeyEnv      = "OPENAI_API_KEY"
	defaultMaxSteps       = 8
	minTimeoutSeconds     = 50
	defaultTimeoutSeconds = minTimeoutSeconds
)

var (
	ErrMissingInstruction = errors.New("agent instruction is required")
	ErrInvalidMode        = errors.New("agent mode must be safe, discovery_only, or raw_allowed")
	ErrMissingAPIKey      = errors.New("agent provider API key is not configured")
	ErrMissingGraphJin    = errors.New("graphjin core instance is required")
)

type Config struct {
	Enabled         bool   `mapstructure:"enabled" jsonschema:"title=Enable GraphJin Agent,default=false"`
	Provider        string `mapstructure:"provider" jsonschema:"title=Agent Provider,default=openai"`
	Model           string `mapstructure:"model" jsonschema:"title=Agent Model"`
	APIKeyEnv       string `mapstructure:"api_key_env" jsonschema:"title=Agent API Key Environment Variable,default=OPENAI_API_KEY"`
	BaseURL         string `mapstructure:"base_url" jsonschema:"title=Agent Provider Base URL"`
	MaxSteps        int    `mapstructure:"max_steps" jsonschema:"title=Agent Max Steps,default=8"`
	TimeoutSeconds  int    `mapstructure:"timeout_seconds" jsonschema:"title=Agent Timeout Seconds,default=50"`
	AllowRawGraphQL bool   `mapstructure:"allow_raw_graphql" jsonschema:"title=Allow Agent Raw GraphQL,default=false"`
	AllowMutations  bool   `mapstructure:"allow_mutations" jsonschema:"title=Allow Agent Mutations,default=false"`
	ReturnTrace     bool   `mapstructure:"return_trace" jsonschema:"title=Return Agent Trace,default=false"`
}

type Request struct {
	Instruction string         `json:"instruction"`
	Context     map[string]any `json:"context,omitempty"`
	Namespace   string         `json:"namespace,omitempty"`
	Mode        string         `json:"mode,omitempty"`
	MaxSteps    int            `json:"max_steps,omitempty"`
	ReturnTrace *bool          `json:"return_trace,omitempty"`

	// Capabilities is the caller's role/visibility profile. It is intentionally
	// json:"-" so it can never be supplied or spoofed from the REST body or MCP
	// arguments; the service populates it after unmarshalling the wire request.
	// It is read-only policy input and is not forwarded into the LLM prompt.
	Capabilities *CapabilityProfile `json:"-"`
}

// CapabilityProfile is an opaque, caller-derived snapshot of what this request is
// allowed to see and do. It is built by the service from the same machinery that
// powers the MCP capability profile and handed to the agent as read-only input.
//
// Invariant: the *SystemRoots fields only ever contain the fixed gj_* system roots
// (gj_catalog, gj_security, gj_runtime, gj_config, gj_workflow, gj_workflow_execution,
// gj_artifacts). Application/database roots (potentially tens of thousands of tables)
// are NEVER enumerated here — they stay behind the catalog and progressive discovery,
// and their authorization remains core RLS per-table at execution.
type CapabilityProfile struct {
	RoleClass             string   `json:"role_class,omitempty"`
	Authenticated         bool     `json:"authenticated"`
	Mode                  string   `json:"mode,omitempty"`
	CatalogRevision       string   `json:"catalog_revision,omitempty"`
	AvailableTools        []string `json:"available_tools,omitempty"`
	AvailableSystemRoots  []string `json:"available_system_roots,omitempty"`
	BlockedSystemRoots    []string `json:"blocked_system_roots,omitempty"`
	RecommendedEntrypoint string   `json:"recommended_entrypoint,omitempty"`
	SafetyNotes           []string `json:"safety_notes,omitempty"`
}

type Response struct {
	Status   string      `json:"status"`
	Answer   string      `json:"answer,omitempty"`
	Data     any         `json:"data,omitempty"`
	Evidence any         `json:"evidence,omitempty"`
	Actions  any         `json:"actions,omitempty"`
	Next     any         `json:"next,omitempty"`
	Errors   []ErrorInfo `json:"errors,omitempty"`
	Usage    any         `json:"usage,omitempty"`
	Trace    any         `json:"trace,omitempty"`
	TraceID  string      `json:"trace_id,omitempty"`
}

type ErrorInfo struct {
	Message    string         `json:"message"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

type Program interface {
	Forward(context.Context, ax.AIClient, map[string]ax.Value, map[string]ax.Value) (ax.Value, error)
	GetActionLog() ax.Value
	GetUsage() ax.Value
	ExportTrace() ax.Value
}

type ClientFactory func(Config) (ax.AIClient, error)
type ProgramFactory func(string, map[string]ax.Value) Program

type Option func(*Agent)

type GraphRuntime interface {
	GraphQLHelp(context.Context, map[string]any) (any, error)
	QueryCatalog(context.Context, map[string]any) (any, error)
	ValidateWhereClause(context.Context, map[string]any) (any, error)
	ExecuteSavedQuery(context.Context, map[string]any) (any, error)
	ExecuteGraphQL(context.Context, map[string]any) (any, error)
}

type Agent struct {
	config       Config
	runtime      GraphRuntime
	newClient    ClientFactory
	newProgram   ProgramFactory
	now          func() time.Time
	agentMessage string
}

func New(gj *core.GraphJin, config Config, options ...Option) (*Agent, error) {
	if gj == nil {
		return nil, ErrMissingGraphJin
	}
	return newAgent(config, newCoreRuntime(gj, config), options...), nil
}

func NewCoreRuntime(gj *core.GraphJin, config Config) (GraphRuntime, error) {
	if gj == nil {
		return nil, ErrMissingGraphJin
	}
	return newCoreRuntime(gj, config), nil
}

func newAgent(config Config, rt GraphRuntime, options ...Option) *Agent {
	a := &Agent{
		config:       config.withDefaults(),
		runtime:      rt,
		newClient:    DefaultClientFactory,
		newProgram:   func(signature string, options map[string]ax.Value) Program { return ax.NewAgent(signature, options) },
		now:          time.Now,
		agentMessage: defaultAgentMessage,
	}
	for _, option := range options {
		if option != nil {
			option(a)
		}
	}
	return a
}

func WithClientFactory(factory ClientFactory) Option {
	return func(a *Agent) {
		if factory != nil {
			a.newClient = factory
		}
	}
}

func WithRuntime(rt GraphRuntime) Option {
	return func(a *Agent) {
		if rt != nil {
			a.runtime = rt
		}
	}
}

func WithProgramFactory(factory ProgramFactory) Option {
	return func(a *Agent) {
		if factory != nil {
			a.newProgram = factory
		}
	}
}

func WithNow(now func() time.Time) Option {
	return func(a *Agent) {
		if now != nil {
			a.now = now
		}
	}
}

func (a *Agent) Ask(ctx context.Context, req Request) (Response, error) {
	return a.Run(ctx, req)
}

func (a *Agent) Run(ctx context.Context, req Request) (resp Response, err error) {
	if strings.TrimSpace(req.Instruction) == "" {
		return Response{}, ErrMissingInstruction
	}
	mode, err := normalizeMode(req.Mode)
	if err != nil {
		return Response{}, err
	}
	cfg := a.config.withDefaults()
	maxSteps := effectiveMaxSteps(cfg.MaxSteps, req.MaxSteps)
	returnTrace := cfg.ReturnTrace
	if req.ReturnTrace != nil {
		returnTrace = *req.ReturnTrace
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	client, err := a.newClient(cfg)
	if err != nil {
		return Response{}, err
	}

	traceID := a.traceID()
	var program Program
	var protocol *protocolRuntime
	defer func() {
		if recovered := recover(); recovered != nil {
			resp = Response{
				Status:  StatusError,
				TraceID: traceID,
				Errors:  []ErrorInfo{{Message: fmt.Sprintf("agent runtime panic: %v", recovered)}},
			}
			if program != nil {
				resp = attachProgramMetadata(resp, program, returnTrace)
				if finalResp, ok := responseFromFinalActionLog(program.GetActionLog(), traceID); ok {
					resp = attachProgramMetadata(finalResp, program, returnTrace)
				}
			}
			if protocol != nil {
				resp = protocol.state.finalize(resp)
			}
			err = nil
		}
	}()

	protocol = newProtocolRuntime(a.runtime, strings.TrimSpace(req.Instruction), req.Namespace)
	seed, err := protocol.Seed(ctx)
	if err != nil {
		resp := Response{
			Status:  StatusBlocked,
			Answer:  "I could not begin GraphJin catalog discovery for this request.",
			TraceID: traceID,
			Errors: []ErrorInfo{{
				Message: err.Error(),
				Extensions: map[string]any{
					"code":     "catalog_seed_failed",
					"protocol": "graphjin_discovery",
				},
			}},
		}
		return protocol.state.finalize(resp), nil
	}

	selected := selectSkill(strings.TrimSpace(req.Instruction), seed, mode, req.Capabilities)
	protocol.state.setSkill(selected)

	runReq := req
	runReq.Context = cloneContext(req.Context)
	runReq.Context[protocolContextKey] = seed
	tools := a.tools(ctx, runReq, mode, cfg, protocol, selected)
	runtime := axgoja.NewRuntime()
	for _, tool := range tools {
		t := tool
		runtime.RegisterCallable(t.Name, func(params ax.Value) (ax.Value, error) {
			return t.Handler(asMap(params))
		})
	}

	options := map[string]ax.Value{
		"instruction":       composeInstruction(a.agentMessage, selected),
		"functions":         toolArray(tools),
		"functionDiscovery": false,
		"contextFields":     []ax.Value{},
		"runtime": map[string]ax.Value{
			"language":          "JavaScript",
			"usageInstructions": runtimeUsageInstructions,
		},
		"max_actor_steps": maxSteps,
	}
	program = a.newProgram(agentSignature, options)
	output, err := program.Forward(ctx, client, map[string]ax.Value{
		"instruction": strings.TrimSpace(req.Instruction),
		"context":     runReq.Context,
		"namespace":   req.Namespace,
		"mode":        mode,
	}, map[string]ax.Value{
		"runtime":         runtime,
		"max_actor_steps": maxSteps,
	})
	if err != nil {
		if finalResp, ok := responseFromFinalActionLog(program.GetActionLog(), traceID); ok {
			return protocol.state.finalize(attachProgramMetadata(finalResp, program, returnTrace)), nil
		}
		return protocol.state.finalize(responseFromError(err, traceID, program, returnTrace)), nil
	}
	resp = responseFromValue(output, traceID)
	if resp.Status == "" {
		resp.Status = StatusAnswered
	}
	return protocol.state.finalize(attachProgramMetadata(resp, program, returnTrace)), nil
}

func DefaultClientFactory(cfg Config) (ax.AIClient, error) {
	cfg = cfg.withDefaults()
	apiKey := os.Getenv(cfg.APIKeyEnv)
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("%w: set %s", ErrMissingAPIKey, cfg.APIKeyEnv)
	}
	options := map[string]ax.Value{
		"apiKey":  apiKey,
		"api_key": apiKey,
	}
	if cfg.Model != "" {
		options["model"] = cfg.Model
	}
	if cfg.BaseURL != "" {
		options["baseUrl"] = cfg.BaseURL
		options["base_url"] = cfg.BaseURL
	}
	return ax.NewAI(cfg.Provider, options), nil
}

func (c Config) withDefaults() Config {
	if c.Provider == "" {
		c.Provider = defaultProvider
	}
	if c.APIKeyEnv == "" {
		c.APIKeyEnv = defaultAPIKeyEnv
	}
	if c.MaxSteps <= 0 {
		c.MaxSteps = defaultMaxSteps
	}
	c.TimeoutSeconds = EffectiveTimeoutSeconds(c.TimeoutSeconds)
	return c
}

func EffectiveTimeoutSeconds(timeoutSeconds int) int {
	if timeoutSeconds < minTimeoutSeconds {
		return minTimeoutSeconds
	}
	return timeoutSeconds
}

func effectiveMaxSteps(configMax, requestMax int) int {
	if configMax <= 0 {
		configMax = defaultMaxSteps
	}
	if requestMax <= 0 || requestMax > configMax {
		return configMax
	}
	return requestMax
}

func normalizeMode(mode string) (string, error) {
	switch strings.TrimSpace(mode) {
	case "", ModeSafe:
		return ModeSafe, nil
	case ModeDiscoveryOnly:
		return ModeDiscoveryOnly, nil
	case ModeRawAllowed:
		return ModeRawAllowed, nil
	default:
		return "", ErrInvalidMode
	}
}

func (a *Agent) tools(ctx context.Context, req Request, mode string, cfg Config, rt GraphRuntime, selected skill) []ax.Tool {
	tools := []ax.Tool{
		a.tool("graphql_help", "Get GraphJin discovery, query, config, and safety guidance from the catalog-backed help surface.",
			[]ax.Field{field("for", "string", "Help topic such as discovery, query, mutation, config, security, workflows, or runtime.", false)},
			func(args map[string]ax.Value) (ax.Value, error) {
				return a.call(ctx, req.Namespace, args, rt.GraphQLHelp)
			}),
		a.tool("query_catalog", "Search or fetch GraphJin catalog rows for tables, relationships, saved queries, workflows, syntax, security, and runtime evidence.",
			[]ax.Field{
				field("id", "string", "Optional catalog item id for a detailed row.", true),
				field("search", "string", "Optional full-text search based on the user's goal.", true),
				field("where", "json", "Optional GraphJin-style filter object.", true),
				field("order_by", "json", "Optional sort object.", true),
				field("explain", "boolean", "Include match reasons when search is present.", true),
				field("kind", "string", "Compatibility shorthand for where.kind.eq.", true),
				field("database", "string", "Compatibility shorthand for where.database_name.eq.", true),
				field("schema", "string", "Compatibility shorthand for where.schema_name.eq.", true),
				field("table", "string", "Compatibility shorthand for where.table_name.eq.", true),
				field("column", "string", "Compatibility shorthand for where.column_name.eq.", true),
				field("limit", "number", "Maximum catalog rows to return.", true),
			},
			func(args map[string]ax.Value) (ax.Value, error) {
				return a.call(ctx, req.Namespace, args, rt.QueryCatalog)
			}),
		a.tool("validate_where_clause", "Validate a GraphJin where clause against table schema and the compiler before execution.",
			[]ax.Field{
				field("table", "string", "Table name for validation.", false),
				field("where", "json", "Where clause object or JSON string.", false),
				field("database", "string", "Optional configured database name.", true),
			},
			func(args map[string]ax.Value) (ax.Value, error) {
				return a.call(ctx, req.Namespace, args, rt.ValidateWhereClause)
			}),
	}
	if mode != ModeDiscoveryOnly {
		tools = append(tools, a.tool("execute_saved_query", "Execute a pre-approved saved query by name. This is rejected unless this run already called query_catalog({id:\"saved_query:<same name>\"}); the initial seed and saved-query lists do not satisfy this detail requirement. Prefer this over raw GraphQL. The result shape is { data, errors }; read rows from result.data.",
			[]ax.Field{
				field("name", "string", "Saved query name.", false),
				field("variables", "json", "Optional variables object.", true),
				field("namespace", "string", "Optional namespace override.", true),
			},
			func(args map[string]ax.Value) (ax.Value, error) {
				return a.call(ctx, req.Namespace, args, rt.ExecuteSavedQuery)
			}))
	}
	if mode == ModeRawAllowed && cfg.AllowRawGraphQL {
		tools = append(tools, a.tool("execute_graphql", "Execute raw GraphJin GraphQL only after catalog discovery and validation.",
			[]ax.Field{
				field("query", "string", "GraphJin GraphQL query or mutation.", false),
				field("variables", "json", "Optional variables object.", true),
				field("namespace", "string", "Optional namespace override.", true),
			},
			func(args map[string]ax.Value) (ax.Value, error) {
				return a.call(ctx, req.Namespace, args, rt.ExecuteGraphQL)
			}))
	}
	return filterToolsBySkill(tools, selected)
}

// filterToolsBySkill intersects the mode/config-gated tool set with the selected
// skill's allow predicate. A skill may only remove tools, never widen the surface.
func filterToolsBySkill(tools []ax.Tool, selected skill) []ax.Tool {
	if selected.allowTool == nil {
		return tools
	}
	out := make([]ax.Tool, 0, len(tools))
	for _, t := range tools {
		if selected.allowTool(t.Name) {
			out = append(out, t)
		}
	}
	return out
}

// composeInstruction appends the selected skill's focused fragment to the base agent
// message. Fragments are fixed-size and schema-independent (no app-root enumeration).
func composeInstruction(base string, selected skill) string {
	if strings.TrimSpace(selected.instruction) == "" {
		return base
	}
	return base + "\n\n" + selected.instruction
}

func (a *Agent) tool(name, description string, args []ax.Field, handler func(map[string]ax.Value) (ax.Value, error)) ax.Tool {
	tool := ax.Fn(name).WithHandler(handler)
	tool.Description = description
	tool.Args = map[string]ax.Field{}
	for _, arg := range args {
		tool.Args[arg.Name] = arg
	}
	return tool
}

func (a *Agent) call(ctx context.Context, namespace string, args map[string]ax.Value, fn func(context.Context, map[string]any) (any, error)) (ax.Value, error) {
	callArgs := map[string]any{}
	for key, value := range args {
		callArgs[key] = normalizeValue(value)
	}
	if namespace != "" {
		if _, ok := callArgs["namespace"]; !ok {
			callArgs["namespace"] = namespace
		}
	}
	out, err := fn(ctx, callArgs)
	if err != nil {
		return nil, err
	}
	return normalizeValue(out), nil
}

func field(name, typ, description string, optional bool) ax.Field {
	return ax.Field{
		Name:        name,
		Type:        ax.FieldType{Name: typ},
		Description: description,
		IsOptional:  optional,
	}
}

func toolArray(tools []ax.Tool) ax.Value {
	values := make([]ax.Value, 0, len(tools))
	for _, tool := range tools {
		values = append(values, tool)
	}
	return ax.Array(values...)
}

func asMap(value ax.Value) map[string]ax.Value {
	switch v := value.(type) {
	case map[string]ax.Value:
		return v
	default:
		if m, ok := normalizeValue(value).(map[string]any); ok {
			out := make(map[string]ax.Value, len(m))
			for key, item := range m {
				out[key] = item
			}
			return out
		}
		return map[string]ax.Value{}
	}
}

func cloneContext(ctx map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range ctx {
		out[key] = value
	}
	return out
}

func responseFromValue(value ax.Value, traceID string) Response {
	var resp Response
	if text, ok := value.(string); ok {
		resp.Answer = text
		resp.Status = StatusAnswered
		resp.TraceID = traceID
		return resp
	}
	if data, err := json.Marshal(normalizeValue(value)); err == nil {
		_ = json.Unmarshal(data, &resp)
	}
	if resp.Answer == "" {
		if m, ok := normalizeValue(value).(map[string]any); ok {
			if answer, ok := m["answer"].(string); ok {
				resp.Answer = answer
			}
		}
	}
	resp.TraceID = traceID
	return resp
}

func responseFromError(err error, traceID string, program Program, returnTrace bool) Response {
	resp := Response{
		Status:  StatusError,
		TraceID: traceID,
		Errors:  []ErrorInfo{{Message: err.Error()}},
	}
	if isNoRuntimeCodeError(err) {
		resp.Status = StatusBlocked
		resp.Answer = "I could not complete the GraphJin agent loop because the model did not emit a runnable action. No unsafe GraphJin execution was performed."
		resp.Errors = []ErrorInfo{{
			Message: err.Error(),
			Extensions: map[string]any{
				"code":     "agent_no_runtime_code",
				"protocol": "graphjin_discovery",
			},
		}}
	}
	var axErr ax.AxError
	if errors.As(err, &axErr) && axErr.Category == "clarification" {
		resp.Status = StatusNeedsClarification
		resp.Answer = clarificationAnswer(axErr.Payload)
		resp.Errors = nil
	}
	if program != nil {
		resp = attachProgramMetadata(resp, program, returnTrace)
	}
	return resp
}

func isNoRuntimeCodeError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "did not return runtime code field")
}

func clarificationAnswer(payload any) string {
	value := normalizeValue(payload)
	if question := clarificationQuestion(value); question != "" {
		return question
	}
	if text := stringify(value); strings.TrimSpace(text) != "" {
		return text
	}
	return "I need a little more detail before I can answer safely."
}

func clarificationQuestion(value any) string {
	switch v := value.(type) {
	case map[string]any:
		if question := strings.TrimSpace(stringValue(v["question"])); question != "" {
			return question
		}
		if args, ok := v["args"]; ok {
			if question := clarificationQuestion(args); question != "" {
				return question
			}
		}
	case []any:
		for _, item := range v {
			if question := clarificationQuestion(item); question != "" {
				return question
			}
		}
	}
	return ""
}

func attachProgramMetadata(resp Response, program Program, returnTrace bool) Response {
	if program == nil {
		return resp
	}
	if returnTrace {
		if resp.Actions == nil {
			resp.Actions = normalizeValue(program.GetActionLog())
		}
		resp.Trace = normalizeValue(program.ExportTrace())
	}
	if resp.Usage == nil {
		resp.Usage = normalizeValue(program.GetUsage())
	}
	return resp
}

func responseFromFinalActionLog(actionLog any, traceID string) (Response, bool) {
	items, ok := normalizeValue(actionLog).([]any)
	if !ok {
		return Response{}, false
	}
	for i := len(items) - 1; i >= 0; i-- {
		item, ok := items[i].(map[string]any)
		if !ok {
			continue
		}
		payload, ok := item["completion_payload"].(map[string]any)
		if !ok {
			payload, _ = item["result"].(map[string]any)
		}
		if stringValue(payload["type"]) != "final" {
			continue
		}
		resp := Response{Status: StatusAnswered, TraceID: traceID}
		args, _ := payload["args"].([]any)
		if len(args) == 0 {
			return resp, true
		}
		switch v := args[0].(type) {
		case string:
			if len(args) < 2 || emptyObject(args[1]) {
				continue
			}
			answer := answerFromFinalEvidence(args[1])
			if answer == "" {
				continue
			}
			resp.Answer = answer
			resp.Data = args[1]
		case map[string]any:
			if stringValue(v["status"]) == "" && stringValue(v["answer"]) == "" {
				continue
			}
			data, err := json.Marshal(v)
			if err == nil {
				_ = json.Unmarshal(data, &resp)
			}
			if resp.Status == "" {
				resp.Status = StatusAnswered
			}
			resp.TraceID = traceID
		default:
			resp.Answer = stringify(v)
		}
		return resp, true
	}
	return Response{}, false
}

func answerFromFinalEvidence(value any) string {
	m, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"answer", "summary", "result", "data"} {
		if text := evidenceText(m[key]); text != "" {
			return text
		}
	}
	return ""
}

func evidenceText(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text := evidenceText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func emptyObject(value any) bool {
	m, ok := value.(map[string]any)
	return ok && len(m) == 0
}

func normalizeValue(value any) any {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return value
	}
	return out
}

func stringify(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

func (a *Agent) traceID() string {
	return fmt.Sprintf("agent-%d", a.now().UnixNano())
}

type coreRuntime struct {
	gj     *core.GraphJin
	config Config
}

type catalogResult struct {
	GeneratedAt string                       `json:"generated_at"`
	Revision    string                       `json:"revision,omitempty"`
	Count       int                          `json:"count"`
	Limit       int                          `json:"limit,omitempty"`
	Truncated   bool                         `json:"truncated,omitempty"`
	Cards       []core.CatalogCard           `json:"cards"`
	Details     []core.CatalogCardDetail     `json:"details,omitempty"`
	Edges       []core.CatalogEdge           `json:"edges,omitempty"`
	Matches     map[string]core.CatalogMatch `json:"matches,omitempty"`
	Next        any                          `json:"next,omitempty"`
}

type executeResult struct {
	Data   any         `json:"data,omitempty"`
	Errors []ErrorInfo `json:"errors,omitempty"`
}

type whereValidationResult struct {
	Valid          bool                           `json:"valid"`
	Errors         []whereValidationError         `json:"errors,omitempty"`
	Warnings       []string                       `json:"warnings,omitempty"`
	CompilerErrors []string                       `json:"compiler_errors,omitempty"`
	ValidatedBy    string                         `json:"validated_by"`
	Table          string                         `json:"table"`
	Database       string                         `json:"database,omitempty"`
	Columns        map[string]whereColumnTypeInfo `json:"columns,omitempty"`
	ExampleQuery   string                         `json:"example_query,omitempty"`
}

type whereValidationError struct {
	Path       string `json:"path"`
	Error      string `json:"error"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

type whereColumnTypeInfo struct {
	Type           string   `json:"type"`
	ValidOperators []string `json:"valid_operators"`
}

func newCoreRuntime(gj *core.GraphJin, config Config) *coreRuntime {
	return &coreRuntime{gj: gj, config: config.withDefaults()}
}

func (r *coreRuntime) GraphQLHelp(ctx context.Context, args map[string]any) (any, error) {
	topic := strings.ToLower(strings.TrimSpace(stringArg(args, "for")))
	if topic == "" {
		topic = "discovery"
	}
	snap, err := r.gj.CatalogSnapshot()
	if err != nil {
		return nil, err
	}
	id := "help:" + topic
	if card, ok := snap.Card(id); ok {
		return catalogResult{
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Revision:    snap.Revision,
			Count:       1,
			Cards:       []core.CatalogCard{card},
			Details:     snap.CardDetails(id),
			Edges:       snap.CardEdges(id),
			Next:        catalogNext("query_catalog", "Continue with filtered catalog discovery or execute an approved saved query when this detail row supports it."),
		}, nil
	}
	q := core.CatalogQuery{Search: topic, Kind: "help", Limit: 5, Explain: true}
	result, err := snap.QueryResult(q)
	if err != nil {
		return nil, err
	}
	return catalogResult{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Revision:    snap.Revision,
		Count:       len(result.Cards),
		Limit:       q.Limit,
		Truncated:   len(result.Cards) >= q.Limit,
		Cards:       result.Cards,
		Matches:     result.Matches,
		Next:        catalogNext("query_catalog", "Inspect a returned help row by id, or continue with filtered catalog discovery."),
	}, nil
}

func (r *coreRuntime) QueryCatalog(ctx context.Context, args map[string]any) (any, error) {
	snap, err := r.gj.CatalogSnapshot()
	if err != nil {
		return nil, err
	}
	if id := stringArg(args, "id"); id != "" {
		card, ok := snap.Card(id)
		if !ok {
			return catalogResult{
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
				Revision:    snap.Revision,
			}, nil
		}
		return catalogResult{
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Revision:    snap.Revision,
			Count:       1,
			Cards:       []core.CatalogCard{card},
			Details:     snap.CardDetails(id),
			Edges:       snap.CardEdges(id),
			Next:        catalogNext("query_catalog", "Use this detail row's evidence, examples, safety notes, and edges before selecting an action."),
		}, nil
	}
	q := core.CatalogQuery{
		Search:   stringArg(args, "search"),
		Where:    mapArg(args, "where"),
		OrderBy:  stringMapArg(args, "order_by"),
		Explain:  boolArg(args, "explain"),
		Kind:     stringArg(args, "kind"),
		Database: stringArg(args, "database"),
		Schema:   stringArg(args, "schema"),
		Table:    stringArg(args, "table"),
		Column:   stringArg(args, "column"),
		Limit:    intArg(args, "limit"),
	}
	if q.Limit <= 0 {
		q.Limit = 20
	}
	result, err := snap.QueryResult(q)
	if err != nil {
		return nil, err
	}
	return catalogResult{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Revision:    snap.Revision,
		Count:       len(result.Cards),
		Limit:       q.Limit,
		Truncated:   len(result.Cards) >= q.Limit,
		Cards:       result.Cards,
		Matches:     result.Matches,
		Next:        catalogNextForQuery(q, result.Cards),
	}, nil
}

func catalogNext(tool, reason string) map[string]any {
	return map[string]any{
		"recommended_tool": tool,
		"reason":           reason,
	}
}

func catalogNextForQuery(q core.CatalogQuery, cards []core.CatalogCard) map[string]any {
	count := len(cards)
	kind := catalogQueryKind(q)
	if count == 0 {
		next := catalogNext("query_catalog", "No catalog rows matched. Broaden the search before acting.")
		args := map[string]any{}
		if kind != "" {
			args["kind"] = kind
		}
		if q.Database != "" {
			args["database"] = q.Database
		}
		if q.Table != "" {
			args["table"] = q.Table
		}
		if q.Limit > 0 {
			args["limit"] = q.Limit
		}
		if kind == "saved_query" && q.Search != "" {
			next["reason"] = "No saved_query rows matched the search text. Live row values are not catalog metadata; broaden to query_catalog({kind:\"saved_query\"}), then inspect a returned saved_query:<name> detail before execute_saved_query."
		}
		if len(args) != 0 {
			next["args"] = args
		}
		return next
	}
	if kind == "saved_query" {
		next := catalogNext("query_catalog", "Inspect the most relevant saved_query:<name> row with query_catalog({id:\"saved_query:<name>\"}) before execute_saved_query.")
		if count == 1 && cards[0].ID != "" {
			next["args"] = map[string]any{"id": cards[0].ID}
			next["reason"] = "Inspect this saved query detail before execute_saved_query."
		} else {
			ids := make([]string, 0, len(cards))
			for _, card := range cards {
				if card.ID != "" {
					ids = append(ids, card.ID)
				}
			}
			if len(ids) != 0 {
				next["candidate_ids"] = ids
			}
		}
		return next
	}
	return catalogNext("query_catalog", "Inspect the best returned catalog row with query_catalog({id:\"...\"}) before acting.")
}

func catalogQueryKind(q core.CatalogQuery) string {
	if q.Kind != "" {
		return q.Kind
	}
	if q.Where == nil {
		return ""
	}
	kind, ok := q.Where["kind"].(map[string]any)
	if !ok {
		return ""
	}
	if eq, ok := kind["eq"].(string); ok {
		return eq
	}
	return ""
}

func (r *coreRuntime) ValidateWhereClause(ctx context.Context, args map[string]any) (any, error) {
	table := stringArg(args, "table")
	database := stringArg(args, "database")
	rawWhere, hasWhere := args["where"]
	if table == "" {
		return nil, fmt.Errorf("table name is required")
	}
	if !hasWhere || rawWhere == nil {
		return nil, fmt.Errorf("where clause is required")
	}

	var schema *core.TableSchema
	var err error
	if database != "" {
		schema, err = r.gj.GetTableSchemaForDatabase(database, table)
	} else {
		schema, err = r.gj.GetTableSchema(table)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get schema for table %q: %w", table, err)
	}

	whereData, whereLiteral, err := parseWhereClauseInput(rawWhere)
	if err != nil {
		return whereValidationResult{
			Valid:       false,
			Table:       table,
			Database:    database,
			ValidatedBy: "parser",
			Errors: []whereValidationError{{
				Error:      "parse_error",
				Message:    err.Error(),
				Suggestion: "Pass the where clause as an object like { price: { gt: 50 } }, or as a valid JSON string.",
			}},
		}, nil
	}

	field := validationSelectField(schema)
	query, err := buildWhereValidationQuery(table, database, whereLiteral, field)
	if err != nil {
		return nil, err
	}
	result := whereValidationResult{
		Valid:        true,
		Table:        table,
		Database:     database,
		ValidatedBy:  "compiler",
		ExampleQuery: query,
		Columns:      map[string]whereColumnTypeInfo{},
	}
	columnTypes := map[string]core.ColumnInfo{}
	for _, col := range schema.Columns {
		columnTypes[col.Name] = col
		result.Columns[col.Name] = whereColumnTypeInfo{Type: col.Type, ValidOperators: validOperators(col.Type, col.Array)}
	}
	if whereData != nil {
		result.Errors = validateWhereClause(whereData, columnTypes, "")
	}

	var exp *core.QueryExplanation
	if database != "" {
		exp, err = r.gj.ExplainQueryForDatabase(database, query, nil, "")
	} else {
		exp, err = r.gj.ExplainQuery(query, nil, "")
	}
	if err != nil {
		result.CompilerErrors = append(result.CompilerErrors, err.Error())
	} else if exp != nil && len(exp.Errors) != 0 {
		result.CompilerErrors = append(result.CompilerErrors, exp.Errors...)
	}
	result.Valid = len(result.Errors) == 0 && len(result.CompilerErrors) == 0
	return result, nil
}

func (r *coreRuntime) ExecuteSavedQuery(ctx context.Context, args map[string]any) (any, error) {
	name := stringArg(args, "name")
	if name == "" {
		return nil, fmt.Errorf("query name is required")
	}
	varsJSON, err := variablesJSON(args["variables"])
	if err != nil {
		return nil, err
	}
	var rc core.RequestConfig
	if namespace := stringArg(args, "namespace"); namespace != "" {
		rc.SetNamespace(namespace)
	}
	res, err := r.gj.GraphQLByName(ctx, name, varsJSON, &rc)
	return executeResultFromCore(res, err), nil
}

func (r *coreRuntime) ExecuteGraphQL(ctx context.Context, args map[string]any) (any, error) {
	query := stringArg(args, "query")
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if !r.config.AllowRawGraphQL {
		return nil, fmt.Errorf("raw GraphQL is not allowed")
	}
	if ContainsMutationOperation(query) && !r.config.AllowMutations {
		return nil, fmt.Errorf("mutations are not allowed")
	}
	varsJSON, err := variablesJSON(args["variables"])
	if err != nil {
		return nil, err
	}
	var rc core.RequestConfig
	if namespace := stringArg(args, "namespace"); namespace != "" {
		rc.SetNamespace(namespace)
	}
	res, err := r.gj.GraphQL(ctx, query, varsJSON, &rc)
	return executeResultFromCore(res, err), nil
}

func executeResultFromCore(res *core.Result, err error) executeResult {
	result := executeResult{}
	if err != nil {
		result.Errors = []ErrorInfo{{Message: err.Error()}}
		return result
	}
	if res == nil {
		return result
	}
	if len(res.Data) != 0 {
		var data any
		if err := json.Unmarshal(res.Data, &data); err == nil {
			result.Data = data
		} else {
			result.Data = json.RawMessage(res.Data)
		}
	}
	for _, e := range res.Errors {
		result.Errors = append(result.Errors, ErrorInfo{Message: e.Message, Extensions: e.Extensions})
	}
	return result
}

func variablesJSON(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		return raw, nil
	}
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, nil
		}
		if !json.Valid([]byte(text)) {
			return nil, fmt.Errorf("variables must be valid JSON")
		}
		return json.RawMessage(text), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("invalid variables: %w", err)
	}
	return data, nil
}

func stringArg(args map[string]any, name string) string {
	if args == nil {
		return ""
	}
	if value, ok := args[name].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func boolArg(args map[string]any, name string) bool {
	if args == nil {
		return false
	}
	value, _ := args[name].(bool)
	return value
}

func intArg(args map[string]any, name string) int {
	if args == nil {
		return 0
	}
	switch v := args[name].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	default:
		return 0
	}
}

func mapArg(args map[string]any, name string) map[string]any {
	if args == nil {
		return nil
	}
	value, _ := args[name].(map[string]any)
	return value
}

func stringMapArg(args map[string]any, name string) map[string]string {
	out := map[string]string{}
	for key, value := range mapArg(args, name) {
		if s, ok := value.(string); ok {
			out[key] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseWhereClauseInput(value any) (map[string]any, string, error) {
	switch v := value.(type) {
	case map[string]any:
		literal, err := graphQLInputLiteral(v)
		if err != nil {
			return nil, "", err
		}
		return v, literal, nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil, "", fmt.Errorf("empty where clause")
		}
		var whereData map[string]any
		if err := json.Unmarshal([]byte(trimmed), &whereData); err == nil {
			literal, err := graphQLInputLiteral(whereData)
			if err != nil {
				return nil, "", err
			}
			return whereData, literal, nil
		}
		if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
			return nil, "", fmt.Errorf("where literal must be an object")
		}
		return nil, trimmed, nil
	default:
		return nil, "", fmt.Errorf("unsupported where clause type %T", value)
	}
}

func graphQLInputLiteral(value any) (string, error) {
	switch v := normalizeValue(value).(type) {
	case nil:
		return "null", nil
	case string:
		return quoteGraphQL(v), nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case float64:
		return fmt.Sprintf("%v", v), nil
	case []any:
		items := make([]string, 0, len(v))
		for _, item := range v {
			literal, err := graphQLInputLiteral(item)
			if err != nil {
				return "", err
			}
			items = append(items, literal)
		}
		return "[" + strings.Join(items, ", ") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			if !validGraphQLName(key) {
				return "", fmt.Errorf("unsupported where field %q", key)
			}
			literal, err := graphQLInputLiteral(v[key])
			if err != nil {
				return "", err
			}
			parts = append(parts, key+": "+literal)
		}
		return "{ " + strings.Join(parts, ", ") + " }", nil
	default:
		return "", fmt.Errorf("unsupported where value %T", value)
	}
}

func validationSelectField(schema *core.TableSchema) string {
	if schema == nil {
		return ""
	}
	if schema.PrimaryKey != "" {
		return schema.PrimaryKey
	}
	for _, col := range schema.Columns {
		if !col.Array {
			return col.Name
		}
	}
	if len(schema.Columns) != 0 {
		return schema.Columns[0].Name
	}
	return ""
}

func buildWhereValidationQuery(table, database, whereLiteral, field string) (string, error) {
	if !validGraphQLName(table) {
		return "", fmt.Errorf("unsupported table name %q", table)
	}
	if !validGraphQLName(field) {
		return "", fmt.Errorf("no selectable scalar field found for table %q", table)
	}
	directive := ""
	if database != "" {
		directive = fmt.Sprintf(" @database(name: %s)", quoteGraphQL(database))
	}
	return fmt.Sprintf("query __gj_validate_where { %s(where: %s, limit: 1)%s { %s } }", table, whereLiteral, directive, field), nil
}

func validateWhereClause(where map[string]any, columns map[string]core.ColumnInfo, path string) []whereValidationError {
	var errs []whereValidationError
	logicalOps := map[string]bool{"and": true, "or": true, "not": true}
	keys := make([]string, 0, len(where))
	for key := range where {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := where[key]
		currentPath := key
		if path != "" {
			currentPath = path + "." + key
		}
		if logicalOps[key] {
			switch v := value.(type) {
			case []any:
				for i, item := range v {
					if itemMap, ok := item.(map[string]any); ok {
						errs = append(errs, validateWhereClause(itemMap, columns, fmt.Sprintf("%s[%d]", currentPath, i))...)
					}
				}
			case map[string]any:
				errs = append(errs, validateWhereClause(v, columns, currentPath)...)
			default:
				errs = append(errs, whereValidationError{Path: currentPath, Error: "invalid_logical_operator", Message: "logical operator value must be an object or array"})
			}
			continue
		}
		col, ok := columns[key]
		if !ok {
			errs = append(errs, whereValidationError{Path: currentPath, Error: "unknown_column", Message: fmt.Sprintf("column %q does not exist", key), Suggestion: "Inspect query_catalog for valid table columns."})
			continue
		}
		opMap, ok := value.(map[string]any)
		if !ok {
			continue
		}
		valid := map[string]bool{}
		for _, op := range validOperators(col.Type, col.Array) {
			valid[op] = true
		}
		for op := range opMap {
			if !valid[op] {
				errs = append(errs, whereValidationError{Path: currentPath + "." + op, Error: "invalid_operator", Message: fmt.Sprintf("operator %q is not typical for %s", op, col.Type), Suggestion: "Inspect query_catalog operator_set rows for valid filters."})
			}
		}
	}
	return errs
}

func validOperators(typ string, array bool) []string {
	common := []string{"eq", "neq", "in", "not_in", "is_null"}
	if array {
		return append(common, "contains", "contained_in", "overlaps")
	}
	switch strings.ToLower(typ) {
	case "int", "integer", "bigint", "smallint", "float", "double", "numeric", "decimal", "date", "datetime", "timestamp", "time":
		return append(common, "gt", "gte", "lt", "lte")
	case "text", "varchar", "char", "string", "uuid":
		return append(common, "like", "ilike", "regex", "iregex")
	default:
		return common
	}
}

func quoteGraphQL(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func validGraphQLName(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if i == 0 {
			if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
				return false
			}
			continue
		}
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func ContainsMutationOperation(query string) bool {
	i := 0
	depth := 0
	skipHeader := false
	for i < len(query) {
		c := query[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',' {
			i++
			continue
		}
		if c == '#' {
			for i < len(query) && query[i] != '\n' {
				i++
			}
			continue
		}
		if c == '"' {
			i = skipGraphQLString(query, i)
			continue
		}
		if c == '{' {
			if depth == 0 {
				skipHeader = false
			}
			depth++
			i++
			continue
		}
		if c == '}' {
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if !isGraphQLNameStart(c) {
			i++
			continue
		}
		start := i
		i++
		for i < len(query) && isGraphQLNameContinue(query[i]) {
			i++
		}
		if depth != 0 {
			continue
		}
		token := query[start:i]
		if skipHeader {
			continue
		}
		if strings.EqualFold(token, "mutation") {
			return true
		}
		if strings.EqualFold(token, "query") || strings.EqualFold(token, "subscription") || strings.EqualFold(token, "fragment") {
			skipHeader = true
		}
	}
	return false
}

func isGraphQLNameStart(c byte) bool {
	return c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func isGraphQLNameContinue(c byte) bool {
	return isGraphQLNameStart(c) || c >= '0' && c <= '9'
}

func skipGraphQLString(query string, i int) int {
	if i+2 < len(query) && query[i:i+3] == `"""` {
		i += 3
		for i+2 < len(query) {
			if query[i:i+3] == `"""` {
				return i + 3
			}
			i++
		}
		return len(query)
	}
	i++
	for i < len(query) {
		if query[i] == '\\' {
			i += 2
			continue
		}
		if query[i] == '"' {
			return i + 1
		}
		i++
	}
	return len(query)
}

const agentSignature = `"GraphJin-owned catalog-first discovery agent. Answer only from observed GraphJin catalog evidence, validations, or safe execution results."
instruction:string "The user's goal. GraphJin has already seeded context._graphjin_discovery with query_catalog(search: instruction).",
context?:json "Caller context plus _graphjin_discovery seed evidence. Context is not authoritative schema evidence.",
namespace?:string "Optional GraphJin namespace.",
mode?:string "safe, discovery_only, or raw_allowed."
-> status:class "answered, needs_clarification, blocked, error", answer:string "Concise evidence-backed answer.", data?:json "Rows/results from safe execution, usually execute_saved_query result.data.", evidence?:json "Catalog ids, detail rows, validations, execution names, and policy/capability evidence.", actions?:json "Ordered actions actually performed.", next?:json "Safe follow-up options or missing capability."`

const defaultAgentMessage = `You are GraphJin's server-side discovery and answer agent.
GraphJin has already run query_catalog({ search: instruction, explain: true, limit: 10 }) and placed the result in context._graphjin_discovery.
Use Agentic GraphJin's evidence loop before execution:
1. Read context._graphjin_discovery, then continue with query_catalog or graphql_help when more routing or detail is needed.
2. Use graphql_help only when the route, topic, or query shape is unclear. Learn GraphJin GraphQL syntax — operators, directives, and query/mutation patterns — from query_catalog (kinds: operator_set, directive, query_pattern, mutation_pattern) or graphql_help before composing a query.
3. Inspect query_catalog({ id: "..." }) for details_json, evidence_json, examples_json, safety_json, and edges_json before selecting tables, columns, relationships, workflows, actions, or code paths. Never assume a table or column exists — discover it.
4. For saved-query tasks, first list query_catalog({ kind: "saved_query", limit: 10 }) or inspect saved_query ids already present in the seed. Do not assume live row values are searchable catalog metadata. Pick by query name and fields, then you MUST make a separate model-driven call to query_catalog({ id: "saved_query:<name>" }) before execute_saved_query({ name: "<name>" }). The initial seed does not satisfy this detail requirement. Calling execute_saved_query before that detail lookup is rejected and wastes your step budget.
5. Validate where clauses when filters depend on column types, operators, real values, or user-provided variables.
6. Prefer approved saved queries and workflow/catalog evidence. Use execute_graphql only when raw_allowed exposes it; if direct GraphQL/workflow/code/security/runtime access is required but unavailable, return status "blocked" with the catalog evidence and missing capability.
7. execute_saved_query returns { data, errors }; always read saved-query rows from result.data, not from the top-level result object.
8. If a catalog search returns count: 0, broaden once immediately; do not repeat the same search. Observe results and errors, then return to query_catalog/graphql_help when the next step needs new facts.
9. When a tool result includes next.args.id, call query_catalog with that id immediately unless you are blocking. If next.args.id starts with "saved_query:", do that before any execute_saved_query call.
Call tools from JavaScript as runtime globals; do not describe a tool call instead of executing it. Examples:
- const seed = inputs.context && inputs.context._graphjin_discovery;
- const savedQueries = await query_catalog({ kind: "saved_query", limit: 10 });
- const detail = await query_catalog({ id: "saved_query:daily_roast_context" });
- const rows = await execute_saved_query({ name: "daily_roast_context" });
- const data = rows.data;
- await final({ status: "answered", answer: "concise answer", data, evidence: { seed, saved_query: detail.cards } });
Return a compact typed response with status, answer, optional data, evidence, actions, and next. If the available catalog or tools are insufficient, return status "needs_clarification" or "blocked" rather than guessing.`

const runtimeUsageInstructions = `JavaScript goja runtime profile. GraphJin callables are installed as runtime globals. Use query_catalog({...}), graphql_help({...}), validate_where_clause({...}), execute_saved_query({...}), and execute_graphql({...}) when present. The callable inventory may show qualified names such as tools.execute_saved_query, but executable JavaScript must call the bare global name execute_saved_query({...}). Never describe a callable instead of executing it when the request requires data. Start from inputs.context._graphjin_discovery, then inspect catalog detail rows with query_catalog({id:"..."}) before selecting nouns or actions. For saved-query discovery, call query_catalog({kind:"saved_query", limit:10}) first, choose by name and query fields, then inspect query_catalog({id:"saved_query:<name>"}) before execute_saved_query. execute_saved_query is rejected until the matching saved_query detail lookup has happened in this run; the initial seed does not count as that detail lookup. Live row values are not catalog metadata; if search returns count:0, broaden once instead of repeating the same search. If a result includes next.args.id, call query_catalog({id: next.args.id}) next. execute_saved_query returns { data, errors }; read rows from result.data. If direct GraphQL, workflow, code, security, or runtime access is required but unavailable, call final({status:"blocked", answer, evidence, next}) instead of guessing. Call final({ status: "answered", answer, data, evidence, actions, next }) only after required GraphJin callables have returned. Use askClarification(...) only when the missing information cannot be obtained from the available callables. Filesystem, network, process, module loading, and native host objects are not exposed by default.`
