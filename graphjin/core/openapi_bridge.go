package core

import (
	"context"
	"fmt"
	"net/http"

	"github.com/dosco/graphjin/core/v3/openapi"
)

// openapiBridge adapts a single openapi.Caller to the core.Resolver
// interface. The existing remote-join machinery (resolve.go,
// remote_join.go) consumes core.Resolver implementations without
// caring whether they're backed by remote_api, the OpenAPI sub-package,
// or any user-supplied implementation — the bridge is just glue.
//
// Construction is cheap; one bridge is built per registered operation
// and held for the engine's lifetime.
type openapiBridge struct {
	caller   *openapi.Caller
	pathName string // name of the path parameter that receives the join key
}

// Resolve is invoked once per parent row in a row-join scenario. The
// req.ID carries the parent column's value (already extracted by the
// remote-join orchestrator) and is mapped onto the operation's path
// parameter. Pass-through-from-headers is intentionally not wired up
// in this iteration — RequestConfig doesn't currently carry inbound
// HTTP headers, and adding that surface is a separate change.
func (b *openapiBridge) Resolve(ctx context.Context, req ResolverReq) ([]byte, error) {
	params := openapi.CallParams{}
	if b.pathName != "" {
		params.PathValues = map[string]string{b.pathName: req.ID}
	}
	return b.caller.Call(ctx, params)
}

// loadOpenAPIIntegration runs at the start of the resolver-init phase
// of GraphJin boot. It walks config/specs, classifies operations, and
// for every row-joinable operation it synthesises a ResolverConfig of
// type "openapi" so the existing initResolvers loop can register the
// remote table in the same code path that handles remote_api today.
//
// Returns early without error when the specs directory is missing —
// OpenAPI integration is optional, not required.
func (gj *graphjinEngine) loadOpenAPIIntegration() error {
	res, err := openapi.Load(
		openapi.LoaderOptions{SpecsDir: gj.conf.OpenAPISpecsDir},
		gj.conf.OpenAPI,
		gj.log,
	)
	if err != nil {
		return fmt.Errorf("openapi load: %w", err)
	}
	if res == nil || res.Registry == nil || len(res.Registry.Specs) == 0 {
		// No specs found and no error — feature is dormant for this
		// deployment. Surface any warnings so a misnamed config doesn't
		// silently disappear.
		for _, w := range res.Warnings {
			gj.log.Println(w)
		}
		return nil
	}

	httpClient := gj.trace.NewHTTPClient()
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	rt, rtWarnings, err := openapi.NewRuntime(res.Registry, httpClient)
	if err != nil {
		return fmt.Errorf("openapi runtime: %w", err)
	}
	gj.openapiRuntime = rt

	for _, w := range res.Warnings {
		gj.log.Println(w)
	}
	for _, w := range rtWarnings {
		gj.log.Println(w)
	}

	// Synthesise ResolverConfigs for row-joinable operations and prepend
	// to gj.conf.Resolvers. The existing initRemote loop consumes them
	// indistinguishably from manually-declared remote_api resolvers.
	synth := synthesiseRowJoinResolvers(res.Registry)
	if len(synth) > 0 {
		gj.conf.Resolvers = append(gj.conf.Resolvers, synth...)
	}

	return nil
}

// synthesiseRowJoinResolvers walks every loaded spec and emits one
// ResolverConfig per OpModeRowJoin operation. The Props carry the
// spec/operation identifiers the openapi rtmap factory needs to find
// the right Caller; the rest of the ResolverConfig fields drive
// initRemote's synthetic-table registration.
func synthesiseRowJoinResolvers(reg *openapi.Registry) []ResolverConfig {
	var out []ResolverConfig
	for _, sp := range reg.Specs {
		for i := range sp.Operations {
			op := &sp.Operations[i]
			if op.Mode != openapi.OpModeRowJoin || op.Join == nil {
				continue
			}
			// Use the operation's actual path param as the substitution
			// key. Validated at classification: row-joinable operations
			// have exactly one path param.
			pathName := ""
			if len(op.PathParams) > 0 {
				pathName = op.PathParams[0].Name
			}
			out = append(out, ResolverConfig{
				Name:   op.ExposeAs,
				Type:   "openapi",
				Table:  op.Join.ParentTable,
				Column: op.Join.ParentColumn,
				Props: ResolverProps{
					"spec_key":     op.SpecKey,
					"operation_id": op.OperationID,
					"path_param":   pathName,
				},
			})
		}
	}
	return out
}

// newOpenAPIResolverFn produces the rtmap factory for type "openapi".
// It is invoked once per synthesised ResolverConfig at initRemote time
// and returns a Resolver bound to a specific spec/operation.
func (gj *graphjinEngine) newOpenAPIResolverFn() ResolverFn {
	return func(props ResolverProps) (Resolver, error) {
		specKey, _ := props["spec_key"].(string)
		opID, _ := props["operation_id"].(string)
		pathName, _ := props["path_param"].(string)
		if gj.openapiRuntime == nil {
			return nil, fmt.Errorf("openapi: runtime not initialised")
		}
		caller, ok := gj.openapiRuntime.Caller(specKey, opID)
		if !ok {
			return nil, fmt.Errorf("openapi: operation %q in spec %q not found at runtime", opID, specKey)
		}
		return &openapiBridge{caller: caller, pathName: pathName}, nil
	}
}
