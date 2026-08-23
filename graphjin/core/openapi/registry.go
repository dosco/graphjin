package openapi

import (
	"fmt"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
)

// OpMode is the GraphJin exposure mode an operation falls into after
// classification. Each mode corresponds to a distinct integration path:
// row joins reuse the existing remote_join machinery, top-level modes
// register virtual tables, and skipped operations are recorded with a
// reason for boot-time logging.
type OpMode int

const (
	// OpModeSkipped — the operation cannot be auto-exposed (async,
	// binary response, mutating verb, etc.). The reason is stored on
	// OpDescriptor.SkipReason for surfacing in startup logs.
	OpModeSkipped OpMode = iota

	// OpModeRowJoin — single-record path operation (e.g.
	// GET /users/{userId}) that has a JoinConfig in user config and
	// will be exposed as a child field on the parent DB table.
	OpModeRowJoin

	// OpModeSingleByID — single-record path operation without a
	// JoinConfig. Exposed at the top level with a required argument
	// matching the path parameter.
	OpModeSingleByID

	// OpModeList — collection operation (e.g. GET /audit-logs).
	// Exposed at the top level as a virtual table whose query-param
	// filters become GraphQL arguments.
	OpModeList

	// OpModeMutation — an explicitly allowlisted POST, PUT, PATCH, or DELETE
	// operation exposed only on the GraphQL mutation root.
	OpModeMutation
)

// String returns a human-readable mode label for log output.
func (m OpMode) String() string {
	switch m {
	case OpModeRowJoin:
		return "row-join"
	case OpModeSingleByID:
		return "top-level (single)"
	case OpModeList:
		return "top-level (list)"
	case OpModeMutation:
		return "mutation"
	default:
		return "skipped"
	}
}

// ParamLocation identifies where a request parameter is carried.
type ParamLocation string

const (
	ParamInPath   ParamLocation = "path"
	ParamInQuery  ParamLocation = "query"
	ParamInHeader ParamLocation = "header"
)

// ParamSpec is the loader-resolved view of an OpenAPI parameter. It retains
// the schema so primitive values can be checked against enum/range/format and
// other declared constraints before any network call.
type ParamSpec struct {
	Name     string
	In       ParamLocation
	Required bool
	Type     string // OpenAPI primitive type: string | integer | number | boolean
	Format   string // optional secondary type info (date-time, uuid, etc.)
	Schema   *openapi3.SchemaRef
}

// OpDescriptor is the per-operation registry entry produced by the loader.
// Everything the resolver needs at runtime — URL template, auth, params,
// result extraction path, exposure config — is denormalised onto this
// struct so the resolver hot path doesn't re-walk the OpenAPI document.
type OpDescriptor struct {
	SourceName   string // owning sources[].name (set only by normalization)
	SpecKey      string // YAML filename without extension (e.g. "interaction_studio")
	OperationID  string // OpenAPI operationId (synthesised when the spec omits it)
	Method       string // GET, POST, PUT, PATCH, or DELETE
	PathTemplate string // OpenAPI path template, e.g. "/users/{userId}"

	Mode       OpMode
	SkipReason string // populated only when Mode == OpModeSkipped

	// Parameters bucketed by location — populated for non-skipped operations.
	PathParams   []ParamSpec
	QueryParams  []ParamSpec
	HeaderParams []ParamSpec

	// ResultPath strips the response down to the actual payload (e.g. when
	// a list endpoint wraps results in {data: [...]}). Empty means "use
	// the response body as-is".
	ResultPath []string

	// IsArrayResponse signals list-shape responses to the GraphQL adapter
	// so it knows whether to expose a singular or plural field.
	IsArrayResponse bool

	// ExposeAs is the GraphQL field name. Defaulted to
	// <spec_key>_<operation_id_snake> by the loader; user config can override.
	ExposeAs string

	// Join carries the user-supplied parent-table mapping when the
	// operation is wired as a row join (Mode == OpModeRowJoin). Nil for
	// auto-exposed top-level operations.
	Join *JoinConfig

	// ResponseSchema points at the success-response body schema. Used
	// by the bridge to synthesise DBColumn entries for top-level virtual
	// tables so they are visible to GraphQL introspection.
	ResponseSchema *openapi3.SchemaRef

	// Mutation request/response contract retained from the OpenAPI operation.
	RequestMediaType    string
	RequestBodySchema   *openapi3.SchemaRef
	RequestBodyRequired bool
	SuccessStatuses     []int
	AllowedRoles        []string
	RetryOnAuthFailure  bool
	MaxRequestBytes     int64
	MaxResponseBytes    int64
	JSONSchema2020      bool // OpenAPI 3.1+ request schemas use JSON Schema 2020-12

	// Defaults: fallback values for path/query params; caller args win.
	Defaults map[string]string
}

// ResolveCallParams maps args onto CallParams, falling back to op.Defaults.
// Returns an error if a required path param has neither an arg nor a default.
func (op *OpDescriptor) ResolveCallParams(args map[string]string) (CallParams, error) {
	p := CallParams{
		PathValues:  map[string]string{},
		QueryValues: map[string]string{},
	}
	for _, ps := range op.PathParams {
		v, ok := args[ps.Name]
		if !ok {
			if d, has := op.Defaults[ps.Name]; has {
				v, ok = d, true
			}
		}
		if !ok {
			if ps.Required {
				return p, fmt.Errorf("openapi: required path param %q missing for %s", ps.Name, op.OperationID)
			}
			continue
		}
		p.PathValues[ps.Name] = v
	}
	for _, qs := range op.QueryParams {
		v, ok := args[qs.Name]
		if !ok {
			if d, has := op.Defaults[qs.Name]; has {
				v, ok = d, true
			}
		}
		if ok {
			p.QueryValues[qs.Name] = v
		}
	}
	return p, nil
}

// Spec is the loader's per-file output: the parsed OpenAPI document, the
// resolved per-spec configuration, and the classified operations.
type Spec struct {
	Key              string
	SourceName       string
	SourcePath       string
	Doc              *openapi3.T
	BaseURL          string
	Auth             AuthConfig
	Concurrency      ConcurrencyConfig
	Timeout          time.Duration
	MaxRequestBytes  int64
	MaxResponseBytes int64
	Operations       []OpDescriptor

	// SkippedCount is a precomputed tally for boot logging; the per-op
	// reasons live on individual OpDescriptors.
	SkippedCount int
}

// Registry holds every loaded spec, keyed by spec key, in load order.
// It is the loader's product and the resolver's input; nothing else
// in the package mutates it after construction.
type Registry struct {
	Specs []*Spec
	index map[string]*Spec
}

// Get looks up a spec by key. The second return is false when no spec
// with that key has been loaded.
func (r *Registry) Get(key string) (*Spec, bool) {
	if r == nil || r.index == nil {
		return nil, false
	}
	s, ok := r.index[key]
	return s, ok
}

// AllOperations returns every classified operation across every loaded
// spec, in (spec, operation) order. Useful for boot logging and tests;
// the resolver uses Registry.Get to find a spec by key instead.
func (r *Registry) AllOperations() []OpDescriptor {
	if r == nil {
		return nil
	}
	var out []OpDescriptor
	for _, s := range r.Specs {
		out = append(out, s.Operations...)
	}
	return out
}

// add appends a spec to the registry and updates the index. Internal —
// the loader is the only caller.
func (r *Registry) add(s *Spec) {
	r.Specs = append(r.Specs, s)
	if r.index == nil {
		r.index = make(map[string]*Spec)
	}
	r.index[s.Key] = s
}
