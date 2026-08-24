package openapi

import (
	"fmt"
	"net/http"
	"sort"
)

// SpecRuntime is the per-spec runtime built once at boot from a parsed
// Spec + the resolved AuthConfig. It owns the auth provider, the shared
// concurrency limiter, and one Caller per active operation.
//
// The bridge layer in core/ uses Runtime.Caller to find the right
// executor for an inbound GraphQL field; tests use it directly.
type SpecRuntime struct {
	spec    *Spec
	auth    AuthProvider
	limiter *limiter
	callers map[string]*Caller
}

// NewSpecRuntime constructs the runtime for one Spec. httpClient is
// used both by the auth provider (for token endpoints) and by every
// caller (for operation requests). One client across both is fine and
// preferred — pooled connections are shared.
func NewSpecRuntime(spec *Spec, httpClient *http.Client) (*SpecRuntime, error) {
	if spec == nil {
		return nil, fmt.Errorf("openapi: nil spec")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if spec.Timeout < 0 {
		return nil, fmt.Errorf("openapi: %s timeout must not be negative", spec.Key)
	}
	timeout := spec.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if httpClient.Timeout > 0 && httpClient.Timeout < timeout {
		timeout = httpClient.Timeout
	}
	client := *httpClient
	client.Timeout = timeout
	httpClient = &client

	auth, err := NewAuthProvider(spec.Auth, httpClient)
	if err != nil {
		return nil, fmt.Errorf("openapi: %s auth: %w", spec.Key, err)
	}

	lim := newLimiter(spec.Concurrency)

	r := &SpecRuntime{
		spec:    spec,
		auth:    auth,
		limiter: lim,
		callers: make(map[string]*Caller),
	}

	for i := range spec.Operations {
		op := &spec.Operations[i]
		if op.Mode == OpModeSkipped {
			continue
		}
		if _, exists := r.callers[op.OperationID]; exists {
			return nil, fmt.Errorf("openapi: %s contains duplicate active operationId %q", spec.Key, op.OperationID)
		}
		r.callers[op.OperationID] = NewCaller(op, spec.BaseURL, auth, lim, httpClient)
	}
	return r, nil
}

// Caller returns the executor for an operationId, or nil + false when
// the operation either doesn't exist on this spec or was skipped during
// classification.
func (r *SpecRuntime) Caller(operationID string) (*Caller, bool) {
	if r == nil {
		return nil, false
	}
	c, ok := r.callers[operationID]
	return c, ok
}

// Spec returns the underlying parsed spec. Useful for tests and for
// the bridge layer that needs to enumerate operations during resolver
// registration.
func (r *SpecRuntime) Spec() *Spec { return r.spec }

// Runtime is a registry of every loaded SpecRuntime, keyed by spec key.
// It mirrors Registry's role at the parse layer: the bridge layer in
// core/ uses Runtime to look up an operation across every spec.
type Runtime struct {
	specs map[string]*SpecRuntime
}

// NewRuntime builds runtimes for every loaded spec in reg. A failure
// for a single spec is logged via the warnings slice but does not
// abort runtime construction — other specs remain available so a
// misconfigured upstream doesn't take down the rest of GraphJin.
func NewRuntime(reg *Registry, httpClient *http.Client) (*Runtime, []string, error) {
	if reg == nil {
		return &Runtime{specs: map[string]*SpecRuntime{}}, nil, nil
	}
	out := &Runtime{specs: make(map[string]*SpecRuntime, len(reg.Specs))}
	var warnings []string
	for _, s := range reg.Specs {
		rt, err := NewSpecRuntime(s, httpClient)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("openapi: %s: %v (spec disabled)", s.Key, err))
			continue
		}
		out.specs[s.Key] = rt
	}
	return out, warnings, nil
}

// Operation returns the OpDescriptor for a (spec, operation) pair.
func (r *Runtime) Operation(specKey, operationID string) (*OpDescriptor, bool) {
	if r == nil {
		return nil, false
	}
	rt, ok := r.specs[specKey]
	if !ok || rt.spec == nil {
		return nil, false
	}
	for i := range rt.spec.Operations {
		if rt.spec.Operations[i].OperationID == operationID {
			return &rt.spec.Operations[i], true
		}
	}
	return nil, false
}

// Caller resolves an operation across every loaded spec. Returns nil
// when no spec contains an operation with that id.
func (r *Runtime) Caller(specKey, operationID string) (*Caller, bool) {
	if r == nil {
		return nil, false
	}
	rt, ok := r.specs[specKey]
	if !ok {
		return nil, false
	}
	return rt.Caller(operationID)
}

// MutationByRoot resolves the registry operation and caller for a synthetic
// GraphQL mutation root.
func (r *Runtime) MutationByRoot(root string) (*OpDescriptor, *Caller, bool) {
	if r == nil {
		return nil, nil, false
	}
	for _, specRuntime := range r.specs {
		if specRuntime == nil || specRuntime.spec == nil {
			continue
		}
		for i := range specRuntime.spec.Operations {
			op := &specRuntime.spec.Operations[i]
			if op.Mode == OpModeMutation && op.ExposeAs == root {
				caller, ok := specRuntime.callers[op.OperationID]
				return op, caller, ok
			}
		}
	}
	return nil, nil, false
}

// Spec returns the runtime for a spec key.
func (r *Runtime) Spec(specKey string) (*SpecRuntime, bool) {
	if r == nil {
		return nil, false
	}
	rt, ok := r.specs[specKey]
	return rt, ok
}

// AllSpecs returns every loaded spec runtime in deterministic spec-key order.
func (r *Runtime) AllSpecs() []*SpecRuntime {
	if r == nil {
		return nil
	}
	out := make([]*SpecRuntime, 0, len(r.specs))
	for _, rt := range r.specs {
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].spec.Key < out[j].spec.Key
	})
	return out
}
