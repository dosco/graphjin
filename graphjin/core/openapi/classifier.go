package openapi

import (
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// classifyAll walks every path/operation pair in doc and produces an
// OpDescriptor per operation. Operations that can't be auto-exposed are
// returned with Mode == OpModeSkipped and a populated SkipReason; nothing
// is dropped silently. Per-operation diagnostic warnings (typically
// internal inconsistencies the user might want to know about) are
// returned alongside.
func classifyAll(spec *Spec, doc *openapi3.T, cfg SpecConfig) ([]OpDescriptor, []string) {
	if doc == nil || doc.Paths == nil {
		return nil, nil
	}

	var ops []OpDescriptor
	var warnings []string

	// Iterate paths in deterministic order so registry output is stable
	// across boots — important for log diffing and tests.
	pathKeys := make([]string, 0, len(doc.Paths.Map()))
	for k := range doc.Paths.Map() {
		pathKeys = append(pathKeys, k)
	}
	sort.Strings(pathKeys)

	for _, path := range pathKeys {
		item := doc.Paths.Find(path)
		if item == nil {
			continue
		}

		// Method-by-method so the per-method skip reasons are accurate.
		// (Currently only GET is allowed; other methods are skipped with
		// an explicit "mutating verb" reason for log clarity.)
		methodOps := []struct {
			method string
			op     *openapi3.Operation
		}{
			{"GET", item.Get},
			{"POST", item.Post},
			{"PUT", item.Put},
			{"PATCH", item.Patch},
			{"DELETE", item.Delete},
		}

		for _, mo := range methodOps {
			if mo.op == nil {
				continue
			}
			op := classifyOne(spec.Key, path, mo.method, mo.op, item, cfg)
			ops = append(ops, op)
			if op.Mode == OpModeSkipped {
				warnings = append(warnings, fmt.Sprintf(
					"openapi: %s %s %s — skipped: %s",
					spec.Key, mo.method, path, op.SkipReason))
			}
		}
	}

	return ops, warnings
}

// classifyOne is the per-operation classifier. It first decides whether
// the operation can be exposed at all (mutating verbs, async patterns,
// non-JSON responses are immediately skipped) and then picks between
// RowJoin / SingleByID / List based on path shape and user configuration.
func classifyOne(
	specKey, path, method string,
	op *openapi3.Operation,
	pathItem *openapi3.PathItem,
	cfg SpecConfig,
) OpDescriptor {
	d := OpDescriptor{
		SpecKey:      specKey,
		OperationID:  operationID(op, method, path),
		Method:       method,
		PathTemplate: path,
		Mode:         OpModeSkipped,
	}

	// Read-only first cut: anything that mutates server state is out of
	// scope until the write-side feature lands.
	if method != "GET" {
		d.SkipReason = "mutating verb (write-side not yet supported)"
		return d
	}

	// Operations explicitly disabled via config never appear regardless
	// of how they would otherwise classify.
	if ov, ok := cfg.Operations[d.OperationID]; ok && ov.Disabled {
		d.SkipReason = "disabled by config"
		return d
	}

	// Find the success response. We only examine 200 — 201/202/204 are
	// either non-applicable to GET or signal async/empty-body patterns
	// that don't fit the join model.
	resp := findSuccessResponse(op)
	if resp == nil {
		d.SkipReason = "no 200 success response declared"
		return d
	}

	// Async / file-download responses can't participate in a query — they
	// don't return JSON in their response body.
	if hasLocationHeader(resp) {
		d.SkipReason = "async pattern (Location header on success response)"
		return d
	}

	jsonContent, mediaType := pickJSONContent(resp)
	if jsonContent == nil {
		d.SkipReason = fmt.Sprintf("non-JSON response (%s)", mediaType)
		return d
	}

	// Parameters live in two places — on the path item (shared across
	// methods) and on the operation itself. Merge and bucket by location.
	pathParams, queryParams, headerParams := collectParams(pathItem, op)
	d.PathParams = pathParams
	d.QueryParams = queryParams
	d.HeaderParams = headerParams

	// Detect a sensible result_path by walking the response schema. List
	// endpoints frequently wrap the array in {data: [...]} or
	// {results: [...]} — stripping that wrapper makes the GraphQL output
	// directly usable. A user override in cfg.Operations wins.
	d.ResultPath, d.IsArrayResponse = deriveResultPath(jsonContent.Schema)
	if ov, ok := cfg.Operations[d.OperationID]; ok && ov.ResultPath != "" {
		d.ResultPath = splitPath(ov.ResultPath)
	}
	d.ResponseSchema = jsonContent.Schema

	override := cfg.Operations[d.OperationID]
	switch len(pathParams) {
	case 0:
		d.Mode = OpModeList
	case 1:
		switch {
		case isSingleResourcePath(path):
			d.Mode = OpModeSingleByID
		case override.ExposeTopLevel:
			if d.IsArrayResponse {
				d.Mode = OpModeList
			} else {
				d.Mode = OpModeSingleByID
			}
		default:
			d.SkipReason = "path parameter not in trailing position (set expose_top_level: true to opt in)"
			return d
		}
	default:
		d.SkipReason = fmt.Sprintf("multi-segment path (%d path params) — needs explicit join config", len(pathParams))
		return d
	}

	// User-provided join wiring upgrades a SingleByID to a RowJoin so
	// the operation is also exposed as a child field on the parent table.
	if d.Mode == OpModeSingleByID {
		if jc, ok := cfg.Joins[d.OperationID]; ok && jc.ParentTable != "" {
			j := jc
			d.Join = &j
			d.Mode = OpModeRowJoin
		}
	}

	d.ExposeAs = exposeAs(specKey, d.OperationID, cfg.Operations[d.OperationID])
	if d.Join != nil && d.Join.ExposeAs != "" {
		d.ExposeAs = d.Join.ExposeAs
	}

	if len(override.Defaults) > 0 {
		d.Defaults = make(map[string]string, len(override.Defaults))
		for k, v := range override.Defaults {
			d.Defaults[k] = v
		}
	}

	return d
}

// operationID returns the spec-supplied operationId or, when missing,
// synthesises one from method + path so logs and registry keys still
// reference something meaningful. Synthesised IDs are stable across
// runs, which matters for log diffing.
func operationID(op *openapi3.Operation, method, path string) string {
	if op != nil && op.OperationID != "" {
		return op.OperationID
	}
	// Fall back to "<method>_<path>" with non-identifier characters
	// replaced. e.g. "GET /users/{userId}" → "get_users_userId".
	repl := strings.NewReplacer("/", "_", "{", "", "}", "", "-", "_")
	return strings.ToLower(method) + repl.Replace(path)
}

func findSuccessResponse(op *openapi3.Operation) *openapi3.Response {
	if op == nil || op.Responses == nil {
		return nil
	}
	if r := op.Responses.Status(200); r != nil && r.Value != nil {
		return r.Value
	}
	// Some specs put the success under "default". Treat as 200 if nothing
	// more specific exists.
	if r := op.Responses.Default(); r != nil && r.Value != nil {
		return r.Value
	}
	return nil
}

func hasLocationHeader(resp *openapi3.Response) bool {
	if resp == nil {
		return false
	}
	for name := range resp.Headers {
		if strings.EqualFold(name, "Location") {
			return true
		}
	}
	return false
}

// pickJSONContent finds the JSON content variant of a response and
// returns it along with the matched media type (for diagnostic messages
// when no JSON variant exists). It accepts both "application/json" and
// vendor variants like "application/vnd.api+json" — anything that
// declares a JSON payload is fair game.
func pickJSONContent(resp *openapi3.Response) (*openapi3.MediaType, string) {
	if resp == nil || resp.Content == nil {
		return nil, ""
	}
	for ct, mt := range resp.Content {
		if strings.Contains(strings.ToLower(ct), "json") {
			return mt, ct
		}
	}
	// Return first media type for diagnostic purposes only.
	for ct := range resp.Content {
		return nil, ct
	}
	return nil, ""
}

// collectParams merges PathItem-level parameters (shared) with
// Operation-level parameters (operation-specific) and buckets the result
// by location. We deduplicate on (name, in) — operation-level entries
// win on collision, matching the OpenAPI spec's resolution rule.
func collectParams(pathItem *openapi3.PathItem, op *openapi3.Operation) (path, query, header []ParamSpec) {
	merged := make(map[string]ParamSpec)

	for _, p := range pathItem.Parameters {
		if p == nil || p.Value == nil {
			continue
		}
		key := paramKey(p.Value.Name, p.Value.In)
		merged[key] = paramSpecFromOpenAPI(p.Value)
	}
	for _, p := range op.Parameters {
		if p == nil || p.Value == nil {
			continue
		}
		key := paramKey(p.Value.Name, p.Value.In)
		merged[key] = paramSpecFromOpenAPI(p.Value)
	}

	for _, ps := range merged {
		switch ps.In {
		case ParamInPath:
			path = append(path, ps)
		case ParamInQuery:
			query = append(query, ps)
		case ParamInHeader:
			header = append(header, ps)
		}
	}

	// Deterministic order for tests + log output.
	sort.Slice(path, func(i, j int) bool { return path[i].Name < path[j].Name })
	sort.Slice(query, func(i, j int) bool { return query[i].Name < query[j].Name })
	sort.Slice(header, func(i, j int) bool { return header[i].Name < header[j].Name })
	return
}

func paramKey(name, in string) string { return in + ":" + name }

func paramSpecFromOpenAPI(p *openapi3.Parameter) ParamSpec {
	ps := ParamSpec{
		Name:     p.Name,
		In:       ParamLocation(p.In),
		Required: p.Required,
	}
	if p.Schema != nil && p.Schema.Value != nil {
		s := p.Schema.Value
		if len(s.Type.Slice()) > 0 {
			ps.Type = s.Type.Slice()[0]
		}
		ps.Format = s.Format
	}
	return ps
}

// deriveResultPath inspects a response schema for the common "wrapper
// object containing the actual data" pattern. When the top-level schema
// is an object with exactly one property whose value is an array (or
// object), we return that property as the result path. This matches
// the data/items/results conventions most APIs use without forcing the
// user to declare them.
func deriveResultPath(schema *openapi3.SchemaRef) ([]string, bool) {
	if schema == nil || schema.Value == nil {
		return nil, false
	}
	s := schema.Value

	// Top-level array — no wrapper.
	if isType(s, "array") {
		return nil, true
	}

	// Object with exactly one likely-payload property. We restrict the
	// auto-detection to common names to avoid being too clever — users
	// can always set result_path explicitly when our heuristic misses.
	if isType(s, "object") {
		candidates := []string{"data", "items", "results", "records"}
		for _, name := range candidates {
			if s.Properties == nil {
				continue
			}
			prop, ok := s.Properties[name]
			if !ok || prop == nil || prop.Value == nil {
				continue
			}
			isArr := isType(prop.Value, "array")
			return []string{name}, isArr
		}
	}

	return nil, false
}

func isType(s *openapi3.Schema, t string) bool {
	if s == nil {
		return false
	}
	for _, x := range s.Type.Slice() {
		if x == t {
			return true
		}
	}
	return false
}

// isSingleResourcePath returns true when the path's only path parameter
// is at the trailing position (e.g. /users/{userId}, /accounts/{aid}).
// We deliberately reject paths like /users/{userId}/profile because their
// semantic mapping to a join key is ambiguous without explicit user input.
func isSingleResourcePath(path string) bool {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) == 0 {
		return false
	}
	last := segs[len(segs)-1]
	return strings.HasPrefix(last, "{") && strings.HasSuffix(last, "}")
}

// splitPath turns "data.items" into ["data", "items"]. Empty input
// returns nil so downstream code can treat "no result path" as a single
// nil check.
func splitPath(p string) []string {
	if p == "" {
		return nil
	}
	return strings.Split(p, ".")
}

// exposeAs returns the GraphQL field name to surface this operation
// under. Precedence: per-operation override > default convention.
// The default is "<spec_key>_<operationId_snake>" to avoid collisions
// when multiple specs declare similarly-named operations.
func exposeAs(specKey, operationID string, override OperationOverride) string {
	if override.ExposeAs != "" {
		return override.ExposeAs
	}
	return specKey + "_" + toSnakeCase(operationID)
}

// toSnakeCase converts camelCase / PascalCase identifiers to snake_case.
// It's intentionally conservative: only inserts an underscore at a
// lower→upper transition. Acronyms (UserID → user_id) are handled by
// also breaking before the last upper in an upper run that's followed
// by a lower — the standard idiom for this conversion.
func toSnakeCase(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	runes := []rune(s)
	for i, r := range runes {
		isUpper := r >= 'A' && r <= 'Z'
		if i > 0 && isUpper {
			prev := runes[i-1]
			prevIsLower := prev >= 'a' && prev <= 'z'
			nextIsLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			prevIsUpper := prev >= 'A' && prev <= 'Z'
			if prevIsLower || (prevIsUpper && nextIsLower) {
				b.WriteByte('_')
			}
		}
		if isUpper {
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
