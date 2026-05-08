package openapi

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// defaultSpecsDir is used when the LoaderOptions don't specify one.
const defaultSpecsDir = "./config/specs"

// LoadResult bundles the registry produced by a Load call along with any
// non-fatal warnings (per-spec parse errors, per-operation classification
// reasons) that the caller can surface in startup logs. A nil Registry
// indicates the directory was missing or empty — that is not an error,
// it just means OpenAPI integration is dormant for this deployment.
type LoadResult struct {
	Registry *Registry
	Warnings []string
}

// Load discovers OpenAPI specs in opts.SpecsDir, parses each one, applies
// per-spec user configuration, and classifies every operation. It is
// designed to be tolerant: a bad spec produces a warning and is dropped
// from the registry, but never aborts loading other specs. Per-operation
// classification failures are likewise recorded and surfaced via warnings.
//
// The configs map is keyed by spec key (filename without extension). Only
// specs whose key matches a configured entry get their AuthConfig populated;
// specs without configuration are still loaded and their list-shape
// operations are still queryable, but anything requiring auth will fail
// at request time with a clear error.
func Load(opts LoaderOptions, configs map[string]SpecConfig, logger *log.Logger) (*LoadResult, error) {
	dir := opts.SpecsDir
	if dir == "" {
		dir = defaultSpecsDir
	}

	res := &LoadResult{Registry: &Registry{}}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing dir is fine — OpenAPI integration is just dormant.
			return res, nil
		}
		return nil, fmt.Errorf("openapi: read specs dir %q: %w", dir, err)
	}

	// Process in deterministic order so registry iteration and log output
	// don't depend on filesystem enumeration order.
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = false

	for _, name := range files {
		path := filepath.Join(dir, name)
		key := strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml")

		doc, err := loader.LoadFromFile(path)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("openapi: skip %s: parse error: %v", name, err))
			continue
		}

		// kin-openapi's Validate is strict; we want to be lenient because
		// real-world specs frequently have minor non-conformances we can
		// still extract operations from. Validation failures become
		// warnings, not skips.
		if vErr := doc.Validate(loader.Context); vErr != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("openapi: %s: validation: %v (continuing)", name, vErr))
		}

		cfg := expandSpecConfig(configs[key])
		cfg = canonicaliseOpKeys(cfg, doc)

		baseURL := cfg.BaseURL
		if baseURL == "" && len(doc.Servers) > 0 {
			baseURL = doc.Servers[0].URL
		}

		spec := &Spec{
			Key:         key,
			SourcePath:  path,
			Doc:         doc,
			BaseURL:     baseURL,
			Auth:        cfg.Auth,
			Concurrency: cfg.Concurrency,
		}

		ops, opWarnings := classifyAll(spec, doc, cfg)
		spec.Operations = ops
		for _, op := range ops {
			if op.Mode == OpModeSkipped {
				spec.SkippedCount++
			}
		}
		res.Warnings = append(res.Warnings, opWarnings...)
		res.Registry.add(spec)

		if logger != nil {
			active := len(ops) - spec.SkippedCount
			logger.Printf("openapi: loaded %s (%d active, %d skipped)", name, active, spec.SkippedCount)
		}
	}

	return res, nil
}

// envVarPattern matches ${NAME} placeholders. Bare $NAME is intentionally
// not expanded — URLs and tokens often contain literal $ characters and
// silent expansion of the bare form is a footgun.
var envVarPattern = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

// expandEnv replaces ${VAR} placeholders with their environment values.
// Missing variables expand to empty string — we don't fail the whole load
// on a single missing credential because operations without credentials
// should still appear in the registry (they'll just fail at call time).
func expandEnv(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]
		return os.Getenv(name)
	})
}

// canonicaliseOpKeys rewrites user-supplied Joins/Operations map keys
// to match the spec's actual operationId casing. Viper lowercases all
// map keys when unmarshalling, so without this step a user-configured
// override for `exportUsers` ends up under `exportusers` and never
// matches the classifier's lookup.
func canonicaliseOpKeys(cfg SpecConfig, doc *openapi3.T) SpecConfig {
	if doc == nil || doc.Paths == nil {
		return cfg
	}
	canonical := map[string]string{}
	for _, item := range doc.Paths.Map() {
		if item == nil {
			continue
		}
		for _, op := range item.Operations() {
			if op != nil && op.OperationID != "" {
				canonical[strings.ToLower(op.OperationID)] = op.OperationID
			}
		}
	}

	if len(cfg.Joins) > 0 {
		out := make(map[string]JoinConfig, len(cfg.Joins))
		for k, v := range cfg.Joins {
			if c, ok := canonical[strings.ToLower(k)]; ok {
				out[c] = v
			} else {
				out[k] = v
			}
		}
		cfg.Joins = out
	}
	if len(cfg.Operations) > 0 {
		out := make(map[string]OperationOverride, len(cfg.Operations))
		for k, v := range cfg.Operations {
			if c, ok := canonical[strings.ToLower(k)]; ok {
				out[c] = v
			} else {
				out[k] = v
			}
		}
		cfg.Operations = out
	}
	return cfg
}

// expandSpecConfig walks SpecConfig and applies env-var expansion to
// every credential-bearing string. Maps and slices are copied so the
// caller's configs aren't mutated.
func expandSpecConfig(in SpecConfig) SpecConfig {
	out := in
	out.BaseURL = expandEnv(in.BaseURL)
	out.Auth = expandAuthConfig(in.Auth)
	if len(in.Joins) > 0 {
		out.Joins = make(map[string]JoinConfig, len(in.Joins))
		for k, v := range in.Joins {
			out.Joins[k] = v
		}
	}
	if len(in.Operations) > 0 {
		out.Operations = make(map[string]OperationOverride, len(in.Operations))
		for k, v := range in.Operations {
			out.Operations[k] = v
		}
	}
	return out
}

func expandAuthConfig(in AuthConfig) AuthConfig {
	out := in
	out.Token = expandEnv(in.Token)
	out.Username = expandEnv(in.Username)
	out.Password = expandEnv(in.Password)
	out.KeyValue = expandEnv(in.KeyValue)
	out.TokenURL = expandEnv(in.TokenURL)
	out.ClientID = expandEnv(in.ClientID)
	out.ClientSecret = expandEnv(in.ClientSecret)
	if in.Request != nil {
		req := *in.Request
		if len(in.Request.Body) > 0 {
			req.Body = make(map[string]interface{}, len(in.Request.Body))
			for k, v := range in.Request.Body {
				if s, ok := v.(string); ok {
					req.Body[k] = expandEnv(s)
				} else {
					req.Body[k] = v
				}
			}
		}
		if len(in.Request.Headers) > 0 {
			req.Headers = make(map[string]string, len(in.Request.Headers))
			for k, v := range in.Request.Headers {
				req.Headers[k] = expandEnv(v)
			}
		}
		out.Request = &req
	}
	return out
}
