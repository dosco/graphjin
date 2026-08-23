// Package openapi consumes OpenAPI 3 specifications dropped into a config
// directory and exposes GET operations as remote-joinable fields and top-level
// virtual tables. Explicitly allowlisted non-GET operations are exposed as
// GraphQL mutations. It is the upstream-API counterpart to GraphJin's existing
// remote_api resolver.
package openapi

import "time"

// DefaultTimeout bounds one upstream operation (including authentication)
// when a spec does not set an explicit timeout. It stays below GraphJin's
// standalone HTTP write deadline so an upstream stall can still be returned
// as a GraphQL error instead of expiring the response socket first.
const DefaultTimeout = 8 * time.Second

// SpecConfig is the per-spec section a user adds to the GraphJin config
// (keyed by spec filename without extension). Everything that cannot be
// derived from the OpenAPI document itself lives here: credentials, base
// URL overrides, DB-to-API join wiring, concurrency caps.
//
// Fields are intentionally permissive at parse time; validation happens in
// the loader after the spec has been parsed and operations classified, so
// errors can be reported in terms of operationIds the user recognises.
type SpecConfig struct {
	// SourceName is set internally while sources[] is normalized. It is not a
	// user-configurable field: authorization must always use the owning source
	// entry rather than data supplied inside a spec config.
	SourceName string `mapstructure:"-" json:"-" yaml:"-"`

	// BaseURL overrides the spec's servers[0].url. Useful when the spec
	// hard-codes a sandbox/prod URL that doesn't match the deployment, or
	// when env-var interpolation is needed (e.g. https://${IS_ACCOUNT}...).
	BaseURL string `mapstructure:"base_url" json:"base_url" yaml:"base_url"`

	// Auth carries credentials and the auth flow shape. One AuthConfig per
	// spec — we don't currently support per-operation auth.
	Auth AuthConfig `mapstructure:"auth" json:"auth" yaml:"auth"`

	// Joins maps an operationId to a DB table/column. Without an entry, an
	// operation is still queryable as a top-level field; with an entry, it
	// also becomes a child field on the named DB table.
	Joins map[string]JoinConfig `mapstructure:"joins" json:"joins" yaml:"joins"`

	// Operations carries per-operation overrides that aren't joins (e.g.
	// renaming the auto-exposed top-level field, overriding result_path).
	// Optional; auto-classification provides sensible defaults.
	Operations map[string]OperationOverride `mapstructure:"operations" json:"operations" yaml:"operations"`

	// Concurrency bounds the goroutine fan-out and request rate per spec.
	// Without this, a 1000-row parent select would spawn 1000 parallel
	// HTTP calls to the upstream — a reliable way to get rate-limited.
	Concurrency ConcurrencyConfig `mapstructure:"concurrency" json:"concurrency" yaml:"concurrency"`

	// Timeout bounds each upstream request, including token acquisition. Zero
	// selects DefaultTimeout. Keeping the bound per spec lets slower services
	// opt into a larger budget when GraphJin is embedded behind a compatible
	// outer HTTP deadline.
	Timeout time.Duration `mapstructure:"timeout" json:"timeout" yaml:"timeout" jsonschema:"title=Upstream Request Timeout"`

	// MaxRequestBytes and MaxResponseBytes bound encoded request bodies and
	// upstream responses. Zero selects conservative package defaults.
	MaxRequestBytes  int64 `mapstructure:"max_request_bytes" json:"max_request_bytes" yaml:"max_request_bytes"`
	MaxResponseBytes int64 `mapstructure:"max_response_bytes" json:"max_response_bytes" yaml:"max_response_bytes"`
}

// AuthConfig describes how outgoing requests to the upstream are authenticated.
// Scheme selects which set of fields is meaningful — the others are ignored.
//
// All credential-bearing string fields support GraphJin's standard ${VAR}
// env-var expansion at load time.
type AuthConfig struct {
	// Scheme: bearer | basic | api_key | oauth2_client_credentials | token_exchange
	Scheme string `mapstructure:"scheme" json:"scheme" yaml:"scheme"`

	// Bearer: a static or env-supplied token. TokenFromRequest is reserved for
	// hosts that explicitly bridge inbound headers; the built-in GraphQL bridge
	// does not currently forward them.
	Token            string            `mapstructure:"token" json:"token" yaml:"token" jsonschema_extras:"x-graphjin-sensitive=secret"`
	TokenFromRequest *TokenFromRequest `mapstructure:"token_from_request" json:"token_from_request" yaml:"token_from_request"`

	// Basic auth.
	Username string `mapstructure:"username" json:"username" yaml:"username"`
	Password string `mapstructure:"password" json:"password" yaml:"password" jsonschema_extras:"x-graphjin-sensitive=secret"`

	// API key (header or query).
	KeyName  string `mapstructure:"key_name" json:"key_name" yaml:"key_name"`
	KeyValue string `mapstructure:"key_value" json:"key_value" yaml:"key_value" jsonschema_extras:"x-graphjin-sensitive=secret"`
	KeyIn    string `mapstructure:"key_in" json:"key_in" yaml:"key_in"` // "header" | "query"

	// OAuth2 client_credentials grant.
	TokenURL     string   `mapstructure:"token_url" json:"token_url" yaml:"token_url"`
	ClientID     string   `mapstructure:"client_id" json:"client_id" yaml:"client_id"`
	ClientSecret string   `mapstructure:"client_secret" json:"client_secret" yaml:"client_secret" jsonschema_extras:"x-graphjin-sensitive=secret"`
	Scopes       []string `mapstructure:"scopes" json:"scopes" yaml:"scopes"`

	// token_exchange — generic "POST credentials → JSON with token" flow.
	// Covers Salesforce Marketing Cloud Personalization and similar
	// vendor-specific token endpoints that aren't standard OAuth2.
	Request  *TokenExchangeRequest  `mapstructure:"request" json:"request" yaml:"request"`
	Response *TokenExchangeResponse `mapstructure:"response" json:"response" yaml:"response"`

	// How the resolved token attaches to outgoing operation requests.
	AttachAs     string `mapstructure:"attach_as" json:"attach_as" yaml:"attach_as"`             // "header" | "query"
	AttachName   string `mapstructure:"attach_name" json:"attach_name" yaml:"attach_name"`       // e.g. "Authorization"
	AttachFormat string `mapstructure:"attach_format" json:"attach_format" yaml:"attach_format"` // e.g. "Bearer {token}"

	// CacheTTL overrides the token cache TTL when the auth response
	// doesn't include an expires_in field. Format: Go duration ("3500s").
	CacheTTL string `mapstructure:"cache_ttl" json:"cache_ttl" yaml:"cache_ttl"`
}

// TokenFromRequest describes credentials carried on an incoming request. The
// auth provider supports this shape, but the built-in GraphQL bridge does not
// currently populate inbound headers, so static credentials are required there.
type TokenFromRequest struct {
	Header string `mapstructure:"header" json:"header" yaml:"header"`
	Query  string `mapstructure:"query" json:"query" yaml:"query"`
}

// TokenExchangeRequest describes the POST shape used by token_exchange auth.
// Body values participate in env-var expansion so credentials never appear
// in the config file directly.
type TokenExchangeRequest struct {
	Method     string                 `mapstructure:"method" json:"method" yaml:"method"`                // default POST
	BodyFormat string                 `mapstructure:"body_format" json:"body_format" yaml:"body_format"` // "json" | "form"
	Body       map[string]interface{} `mapstructure:"body" json:"body" yaml:"body" jsonschema_extras:"x-graphjin-sensitive=secret"`
	Headers    map[string]string      `mapstructure:"headers" json:"headers" yaml:"headers" jsonschema_extras:"x-graphjin-sensitive=secret"`
}

// TokenExchangeResponse describes how to extract the token and TTL from
// the auth endpoint's JSON response.
type TokenExchangeResponse struct {
	TokenField   string `mapstructure:"token_field" json:"token_field" yaml:"token_field"`
	ExpiresField string `mapstructure:"expires_field" json:"expires_field" yaml:"expires_field"` // seconds
}

// JoinConfig wires an operation to a DB table+column. With this set, the
// operation is exposed as a child field on the parent table; without it,
// the operation is still queryable as a top-level field with an explicit
// argument for the same parameter.
type JoinConfig struct {
	ParentTable  string `mapstructure:"parent_table" json:"parent_table" yaml:"parent_table"`
	ParentColumn string `mapstructure:"parent_column" json:"parent_column" yaml:"parent_column"`
	Param        string `mapstructure:"param" json:"param" yaml:"param"`             // OpenAPI param name receiving the column value
	ExposeAs     string `mapstructure:"expose_as" json:"expose_as" yaml:"expose_as"` // GraphQL field name on the parent
}

// OperationOverride applies presentation, classification, and mutation-policy
// settings to an operation.
type OperationOverride struct {
	ExposeAs       string `mapstructure:"expose_as" json:"expose_as" yaml:"expose_as"`
	ResultPath     string `mapstructure:"result_path" json:"result_path" yaml:"result_path"`
	Disabled       bool   `mapstructure:"disabled" json:"disabled" yaml:"disabled"`
	ExposeTopLevel bool   `mapstructure:"expose_top_level" json:"expose_top_level" yaml:"expose_top_level"`
	// ExposeMutation is the explicit opt-in required for every non-GET
	// operation. Merely appearing in an OpenAPI document never exposes a write.
	ExposeMutation bool `mapstructure:"expose_mutation" json:"expose_mutation" yaml:"expose_mutation"`
	// AllowedRoles is an additional runtime allowlist layered on top of source
	// capability and access policy checks.
	AllowedRoles []string `mapstructure:"allowed_roles" json:"allowed_roles" yaml:"allowed_roles"`
	// RetryOnAuthFailure permits one 401 token-refresh retry. It defaults to
	// false for mutations because blindly replaying a write is unsafe.
	RetryOnAuthFailure bool `mapstructure:"retry_on_auth_failure" json:"retry_on_auth_failure" yaml:"retry_on_auth_failure"`

	// Defaults supplies fallback values for path/query params; caller args win.
	Defaults map[string]string `mapstructure:"defaults" json:"defaults" yaml:"defaults"`
}

// Clone returns an ownership-preserving deep copy of a spec configuration.
func (c SpecConfig) Clone() SpecConfig {
	out := c
	out.Auth = c.Auth.clone()
	if c.Joins != nil {
		out.Joins = make(map[string]JoinConfig, len(c.Joins))
		for k, v := range c.Joins {
			out.Joins[k] = v
		}
	}
	if c.Operations != nil {
		out.Operations = make(map[string]OperationOverride, len(c.Operations))
		for k, v := range c.Operations {
			if v.Defaults != nil {
				defaults := make(map[string]string, len(v.Defaults))
				for dk, dv := range v.Defaults {
					defaults[dk] = dv
				}
				v.Defaults = defaults
			}
			if v.AllowedRoles != nil {
				v.AllowedRoles = append([]string(nil), v.AllowedRoles...)
			}
			out.Operations[k] = v
		}
	}
	return out
}

func (c AuthConfig) clone() AuthConfig {
	out := c
	if c.TokenFromRequest != nil {
		value := *c.TokenFromRequest
		out.TokenFromRequest = &value
	}
	if c.Scopes != nil {
		out.Scopes = append([]string(nil), c.Scopes...)
	}
	if c.Request != nil {
		value := *c.Request
		if c.Request.Body != nil {
			value.Body = make(map[string]interface{}, len(c.Request.Body))
			for key, item := range c.Request.Body {
				value.Body[key] = cloneAuthValue(item)
			}
		}
		if c.Request.Headers != nil {
			value.Headers = make(map[string]string, len(c.Request.Headers))
			for key, item := range c.Request.Headers {
				value.Headers[key] = item
			}
		}
		out.Request = &value
	}
	if c.Response != nil {
		value := *c.Response
		out.Response = &value
	}
	return out
}

func cloneAuthValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			out[key] = cloneAuthValue(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, item := range typed {
			out[i] = cloneAuthValue(item)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	case map[string]string:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			out[key] = item
		}
		return out
	default:
		return value
	}
}

// ConcurrencyConfig bounds outgoing-request fan-out per spec.
//
// MaxConcurrent caps simultaneous in-flight requests; RateLimitPerSec is a
// token-bucket limit. Either can be zero to disable. Sensible defaults are
// applied at construction time when unset.
type ConcurrencyConfig struct {
	MaxConcurrent   int `mapstructure:"max_concurrent" json:"max_concurrent" yaml:"max_concurrent"`
	RateLimitPerSec int `mapstructure:"rate_limit_per_second" json:"rate_limit_per_second" yaml:"rate_limit_per_second"`
}

// LoaderOptions controls discovery of spec files. SpecsDir defaults to
// "./config/specs" when empty. The loader reads every *.yaml/*.yml file
// in that directory non-recursively.
type LoaderOptions struct {
	SpecsDir          string
	DefaultSourceName string
	RequireSource     bool
}
