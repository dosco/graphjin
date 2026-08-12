package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/openapi"
)

type openapiBridge struct {
	caller   *openapi.Caller
	pathName string
	op       *openapi.OpDescriptor
	topLevel bool
}

func (b *openapiBridge) Resolve(ctx context.Context, req ResolverReq) ([]byte, error) {
	if b.topLevel {
		return b.callTopLevel(ctx, req.Sel)
	}
	params := openapi.CallParams{}
	if b.pathName != "" {
		params.PathValues = map[string]string{b.pathName: req.ID}
	}
	return b.caller.Call(ctx, params)
}

func (b *openapiBridge) callTopLevel(ctx context.Context, sel *qcode.Select) ([]byte, error) {
	if sel == nil {
		return nil, fmt.Errorf("openapi: top-level resolve called without a select")
	}
	if b.op == nil {
		return nil, fmt.Errorf("openapi: top-level bridge missing operation metadata")
	}
	p, err := b.op.ResolveCallParams(sel.ExtraArgs)
	if err != nil {
		return nil, err
	}
	return b.caller.Call(ctx, p)
}

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

	synth := synthesiseResolvers(res.Registry)
	if len(synth) == 0 {
		return nil
	}
	if err := gj.validateOpenAPINoCollisions(synth); err != nil {
		return err
	}
	if err := gj.preRegisterOpenAPITables(res.Registry); err != nil {
		return err
	}
	gj.conf.Resolvers = append(gj.conf.Resolvers, synth...)
	return nil
}

// preRegisterOpenAPITables registers synthetic remote tables for top-level
// operations into the primary DB's dbinfo before initResolvers walks the
// resolver list. Row-join tables are added later by initRemote itself.
func (gj *graphjinEngine) preRegisterOpenAPITables(reg *openapi.Registry) error {
	pdb := gj.primaryDB()
	if pdb == nil || pdb.dbinfo == nil {
		return nil
	}
	schema := pdb.dbinfo.Schema
	for _, sp := range reg.Specs {
		for i := range sp.Operations {
			op := &sp.Operations[i]
			if op.Mode != openapi.OpModeSingleByID && op.Mode != openapi.OpModeList {
				continue
			}
			t := sdata.NewDBTable(schema, op.ExposeAs, "remote", openapi.SynthesiseColumns(schema, op.ExposeAs, op.ResponseSchema, op.ResultPath))
			t.Args = openapi.SynthesiseArgs(schema, op.ExposeAs, *op)
			pdb.dbinfo.AddTable(t)
		}
	}
	return nil
}

func (gj *graphjinEngine) validateOpenAPINoCollisions(synth []ResolverConfig) error {
	realTables := make(map[string]string)
	pdb := gj.primaryDB()
	if pdb != nil && pdb.dbinfo != nil {
		primarySchema := pdb.dbinfo.Schema
		for _, t := range pdb.dbinfo.Tables {
			if t.Type == "remote" {
				continue
			}
			if t.Schema == primarySchema || t.Schema == "" {
				realTables[t.Name] = t.Schema
			}
		}
	}

	aliases := make(map[string]struct{})
	for _, t := range gj.conf.Tables {
		if t.Name != "" && t.Name != t.Table {
			aliases[t.Name] = struct{}{}
		}
	}

	existingResolvers := make(map[string]struct{})
	for _, r := range gj.conf.Resolvers {
		existingResolvers[r.Name] = struct{}{}
	}

	seen := make(map[string]string)
	for _, rc := range synth {
		specKey, _ := rc.Props["spec_key"].(string)
		opID, _ := rc.Props["operation_id"].(string)
		opIdent := fmt.Sprintf("%s/%s", specKey, opID)

		if schema, ok := realTables[rc.Name]; ok {
			if schema == "" {
				schema = "(default)"
			}
			return fmt.Errorf(
				"openapi: operation %s exposes as %q which collides with a real table in schema %s; set 'expose_as' under openapi.%s.joins.%s to a unique name",
				opIdent, rc.Name, schema, specKey, opID,
			)
		}

		if firstOp, ok := seen[rc.Name]; ok {
			return fmt.Errorf(
				"openapi: operations %s and %s both expose as %q; set 'expose_as' on one of them to disambiguate",
				firstOp, opIdent, rc.Name,
			)
		}
		seen[rc.Name] = opIdent

		if _, ok := aliases[rc.Name]; ok {
			gj.log.Printf("openapi: operation %s exposes as %q which matches a configured table alias; the OpenAPI remote will take precedence", opIdent, rc.Name)
		}
		if _, ok := existingResolvers[rc.Name]; ok {
			gj.log.Printf("openapi: operation %s exposes as %q which collides with an existing resolver named the same; later registration will overwrite earlier", opIdent, rc.Name)
		}
	}
	return nil
}

// synthesiseResolvers emits one ResolverConfig per row-join AND per
// top-level operation. Row-join entries carry a parent table; top-level
// entries leave Table empty so initRemote takes the parent-less path.
func synthesiseResolvers(reg *openapi.Registry) []ResolverConfig {
	var out []ResolverConfig
	for _, sp := range reg.Specs {
		for i := range sp.Operations {
			op := &sp.Operations[i]
			switch op.Mode {
			case openapi.OpModeRowJoin:
				if op.Join == nil {
					continue
				}
				pathName := ""
				if len(op.PathParams) > 0 {
					pathName = op.PathParams[0].Name
				}
				out = append(out, ResolverConfig{
					Name:   op.ExposeAs,
					Type:   "openapi",
					Table:  op.Join.ParentTable,
					Column: op.Join.ParentColumn,
					// The join's response shape is known from the spec. Without this the
					// table registers with no columns at all, so the catalog publishes
					// "0 columns" for it and every consumer has to guess field names.
					remoteColumns: openapi.SynthesiseColumns("", op.ExposeAs, op.ResponseSchema, op.ResultPath),
					Props: ResolverProps{
						"spec_key":         op.SpecKey,
						"operation_id":     op.OperationID,
						"path_param":       pathName,
						"spec_fingerprint": openAPISpecFingerprint(sp),
					},
				})
			case openapi.OpModeSingleByID, openapi.OpModeList:
				out = append(out, ResolverConfig{
					Name: op.ExposeAs,
					Type: "openapi",
					// Table left empty: signals top-level to initRemote.
					Props: ResolverProps{
						"spec_key":         op.SpecKey,
						"operation_id":     op.OperationID,
						"spec_fingerprint": openAPISpecFingerprint(sp),
					},
				})
			}
		}
	}
	return out
}

func openAPISpecFingerprint(sp *openapi.Spec) string {
	h := sha256.New()
	if sp != nil {
		h.Write([]byte(sp.Key))
		h.Write([]byte{0})
		h.Write([]byte(sp.BaseURL))
		h.Write([]byte{0})
		writeFingerprintJSON(h, sp.Auth)
		writeFingerprintJSON(h, sp.Concurrency)
		writeFingerprintJSON(h, openAPIOperationFingerprintParts(sp.Operations))
		if sp.SourcePath != "" {
			if b, err := os.ReadFile(sp.SourcePath); err == nil {
				h.Write(b)
			} else {
				h.Write([]byte(sp.SourcePath))
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeFingerprintJSON(h hashWriter, v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.Write(b)
	h.Write([]byte{0})
}

type hashWriter interface {
	Write([]byte) (int, error)
}

type openAPIOpFingerprint struct {
	SpecKey         string
	OperationID     string
	Method          string
	PathTemplate    string
	Mode            string
	PathParams      []openapi.ParamSpec
	QueryParams     []openapi.ParamSpec
	HeaderParams    []openapi.ParamSpec
	ResultPath      []string
	IsArrayResponse bool
	ExposeAs        string
	Join            *openapi.JoinConfig
}

func openAPIOperationFingerprintParts(ops []openapi.OpDescriptor) []openAPIOpFingerprint {
	out := make([]openAPIOpFingerprint, 0, len(ops))
	for _, op := range ops {
		out = append(out, openAPIOpFingerprint{
			SpecKey:         op.SpecKey,
			OperationID:     op.OperationID,
			Method:          op.Method,
			PathTemplate:    op.PathTemplate,
			Mode:            op.Mode.String(),
			PathParams:      op.PathParams,
			QueryParams:     op.QueryParams,
			HeaderParams:    op.HeaderParams,
			ResultPath:      op.ResultPath,
			IsArrayResponse: op.IsArrayResponse,
			ExposeAs:        op.ExposeAs,
			Join:            op.Join,
		})
	}
	return out
}

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
		op, _ := gj.openapiRuntime.Operation(specKey, opID)
		b := &openapiBridge{caller: caller, pathName: pathName, op: op}
		if op != nil && (op.Mode == openapi.OpModeSingleByID || op.Mode == openapi.OpModeList) {
			b.topLevel = true
		}
		return b, nil
	}
}
