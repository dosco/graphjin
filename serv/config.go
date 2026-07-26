package serv

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3/internal/util"
	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/afero"
	"github.com/spf13/viper"
)

type (
	Core      = core.Config
	Auth      = auth.Auth
	JWTConfig = auth.JWTConfig
)

//go:generate go run ./internal/tools -o config.schema.json

// Configuration for the GraphJin service
type Config struct {
	// Configuration for the GraphJin compiler core
	Core `mapstructure:",squash" jsonschema:"title=Compiler Configuration"`

	// Configuration for the GraphJin Service
	Serv `mapstructure:",squash" jsonschema:"title=Service Configuration"`

	// Name of another config file in the same directory to inherit and
	// override (one level only; the inherited file cannot itself inherit).
	// Example: prod.yml sets `inherits: dev` to start from dev.yml values.
	Inherits string `mapstructure:"inherits" jsonschema:"title=Inherit From Config File"`

	// Configuration for admin service
	// Admin `mapstructure:",squash" jsonschema:"title=Admin Configuration"`

	hostPort string
	hash     string
	name     string
	dirty    bool
	viper    *viper.Viper

	webUIExplicit bool

	// parsedConfig is true only for configurations created through ReadInConfig
	// or NewConfig. Mode-aware defaults intentionally do not apply to callers
	// that construct Config values directly in Go.
	parsedConfig         bool
	managedArtifactStore bool
	explicitSettings     map[string]bool
}

// Configuration for admin service
// type Admin struct {
// 	// Enables the ability to hot-deploy a new configuration
// 	HotDeploy bool `mapstructure:"hot_deploy" jsonschema:"title=Enable Hot Deploy"`
//
// 	// Secret key used to control access to the admin api
// 	AdminSecretKey string `mapstructure:"admin_secret_key" jsonschema:"title=Admin API Secret Key"`
// }

// Configuration for the GraphJin Service
type Serv struct {
	// Application name is used in log and debug messages
	AppName string `mapstructure:"app_name" jsonschema:"title=Application Name"`

	// Legacy production switch. Prefer top-level mode for new configs.
	Production bool `jsonschema:"title=Production Mode,default=false"`

	// The default path to find all configuration files and scripts
	ConfigPath string `mapstructure:"config_path" jsonschema:"title=Config Path"`

	// Logging level must be one of debug, error, warn, info
	LogLevel string `mapstructure:"log_level" jsonschema:"title=Log Level,enum=debug,enum=error,enum=warn,enum=info"`

	// Logging Format: "auto" (default, colored console in dev, JSON in production),
	// "json" (always JSON), or "simple" (always colored console)
	LogFormat string `mapstructure:"log_format" jsonschema:"title=Logging Format,enum=auto,enum=json,enum=simple"`

	// The host and port the service runs on. Example localhost:8080
	HostPort string `mapstructure:"host_port" jsonschema:"title=Host and Port"`

	// Host to run the service on
	Host string `jsonschema:"title=Host"`

	// Port to run the service on
	Port string `jsonschema:"title=Port"`

	// Enables HTTP compression
	HTTPGZip bool `mapstructure:"http_compress" jsonschema:"title=Enable Compression,default=true"`

	// Sets the API rate limits
	RateLimiter RateLimiter `mapstructure:"rate_limiter" jsonschema:"title=Set API Rate Limiting"`

	// Enables the Server-Timing HTTP header
	ServerTiming bool `mapstructure:"server_timing" jsonschema:"title=Server Timing HTTP Header,default=true"`

	// Enable the web UI. Defaults on in dev and agentic modes, off in prod.
	WebUI bool `mapstructure:"web_ui" jsonschema:"title=Enable Web UI,default=false"`

	// Enable OpenTrace request tracing
	EnableTracing bool `mapstructure:"enable_tracing" jsonschema:"title=Enable Tracing,default=true"`

	// Enables reloading the service on config changes. Disabled in production
	WatchAndReload bool `mapstructure:"reload_on_config_change" jsonschema:"title=Reload Config"`

	// Enable blocking requests with a HTTP 401 on auth failure
	AuthFailBlock bool `mapstructure:"auth_fail_block" jsonschema:"title=Block Request on Authorization Failure"`

	// Sets the HTTP CORS Access-Control-Allow-Origin header
	AllowedOrigins []string `mapstructure:"cors_allowed_origins" jsonschema:"title=HTTP CORS Allowed Origins"`

	// Sets the HTTP CORS Access-Control-Allow-Headers header
	AllowedHeaders []string `mapstructure:"cors_allowed_headers" jsonschema:"title=HTTP CORS Allowed Headers"`

	// Enables debug logs for CORS
	DebugCORS bool `mapstructure:"cors_debug" jsonschema:"title=Log CORS"`

	// Sets the HTTP Cache-Control header
	CacheControl string `mapstructure:"cache_control" jsonschema:"title=Enable Cache-Control"`

	// Telemetry struct contains OpenCensus metrics and tracing related config
	// Telemetry Telemetry

	// Sets the default authentication used by the service
	Auth Auth `jsonschema:"title=Authentication"`

	// Database configuration
	DB Database `mapstructure:"database" jsonschema:"title=Database"`

	// MCP (Model Context Protocol) server configuration
	MCP MCPConfig `mapstructure:"mcp" jsonschema:"title=MCP Configuration"`

	// Server-side GraphJin discovery/answer agent configuration
	Agent AgentConfig `mapstructure:"agent" jsonschema:"title=GraphJin Agent Configuration"`

	// Local encrypted secrets configuration
	Secrets SecretsConfig `mapstructure:"secrets" jsonschema:"title=Secrets"`

	// Redis configuration
	Redis RedisConfig `mapstructure:"redis" jsonschema:"title=Redis Configuration"`

	// Coordinated, filesystem-backed database discovery cache.
	DiscoveryCache DiscoveryCacheConfig `mapstructure:"discovery_cache" jsonschema:"title=Discovery Cache Configuration"`

	// Catalog retrieval configuration.
	CatalogSearch CatalogSearchConfig `mapstructure:"catalog_search" jsonschema:"title=Catalog Search Configuration"`

	// Runtime event configuration
	RuntimeEvents RuntimeEventsConfig `mapstructure:"runtime_events" jsonschema:"title=Runtime Events Configuration"`

	// Response caching configuration
	Caching CachingConfig `mapstructure:"caching" jsonschema:"title=Caching Configuration"`

	// Multipart upload configuration (graphql-multipart-request-spec)
	Uploads UploadsConfig `mapstructure:"uploads" jsonschema:"title=Multipart Uploads"`

	// Built-in OIDC login that mints local JWTs for the CLI / MCP client
	AuthLogin AuthLogin `mapstructure:"auth_login" jsonschema:"title=Built-in Login (OIDC)"`
}

// SecretsConfig configures secret storage for write-only config inputs.
type SecretsConfig struct {
	Keystore KeystoreConfig `mapstructure:"keystore" jsonschema:"title=Encrypted Keystore"`
}

// KeystoreConfig configures the local encrypted keystore.
type KeystoreConfig struct {
	Key  string `mapstructure:"key" jsonschema:"title=Keystore Key" jsonschema_extras:"x-graphjin-sensitive=secret"`
	Path string `mapstructure:"path" jsonschema:"title=Keystore Path"`
}

// AuthLogin configures the built-in OIDC sign-in flow that issues local JWTs
// for `graphjin cli` / `graphjin mcp` clients. When Enabled, the service
// registers /api/v1/auth/* endpoints implementing RFC 8628 device-code flow
// against a generic OIDC identity provider.
type AuthLogin struct {
	// Enable the built-in login flow
	Enabled bool `jsonschema:"title=Enable Built-in Login,default=false"`

	// TTL of issued local JWTs (default: 720h = 30 days)
	TokenTTL time.Duration `mapstructure:"token_ttl" jsonschema:"title=Issued Token TTL"`

	// Issuer value stamped onto every locally-minted JWT. Must match
	// `auth.jwt.issuer` if that is set.
	Issuer string `jsonschema:"title=JWT Issuer Claim"`

	// Audience value stamped onto every locally-minted JWT. Must match
	// `auth.jwt.audience` if that is set. Defaults to "graphjin-cli".
	Audience string `jsonschema:"title=JWT Audience Claim"`

	// AudienceGraphjin is a convenience shortcut: when true, the issued JWT's
	// `aud` claim is set to the canonical value "graphjin-cli". Saves having
	// to remember / type the magic string. Mutually exclusive with Audience.
	AudienceGraphjin bool `mapstructure:"audience_graphjin" jsonschema:"title=Use GraphJin Default Audience (graphjin-cli)"`

	// OIDC identity provider used to authenticate humans.
	OIDC AuthLoginOIDC `jsonschema:"title=OIDC Provider"`
}

// AuthLoginOIDC configures a generic OIDC identity provider for AuthLogin.
type AuthLoginOIDC struct {
	// OIDC issuer URL — discovery happens at
	// <issuer_url>/.well-known/openid-configuration
	IssuerURL string `mapstructure:"issuer_url" jsonschema:"title=OIDC Issuer URL,example=https://accounts.google.com"`

	// Registered OAuth client ID / secret for this GraphJin deployment.
	ClientID     string `mapstructure:"client_id" jsonschema:"title=OAuth Client ID"`
	ClientSecret string `mapstructure:"client_secret" jsonschema:"title=OAuth Client Secret"`

	// Scopes requested. Defaults to ["openid","email","profile"].
	Scopes []string `jsonschema:"title=Requested Scopes"`

	// Optional post-authentication allow-lists. If both are empty, any verified
	// identity is accepted.
	AllowedEmails  []string `mapstructure:"allowed_emails" jsonschema:"title=Allowed Email Addresses"`
	AllowedDomains []string `mapstructure:"allowed_domains" jsonschema:"title=Allowed Email Domains"`
}

// Database configuration
type Database struct {
	ConnString string `mapstructure:"connection_string" jsonschema:"title=Connection String"`
	Type       string `jsonschema:"title=Type,enum=postgres,enum=mysql,enum=mariadb,enum=mssql,enum=sqlite,enum=oracle,enum=mongodb,enum=snowflake,enum=cassandra,enum=codesql"`
	Host       string `jsonschema:"title=Host"`
	Port       uint16 `jsonschema:"title=Port"`
	DBName     string `jsonschema:"title=Database Name"`
	User       string `jsonschema:"title=User"`
	Password   string `jsonschema:"title=Password"`
	Schema     string `jsonschema:"title=Postgres Schema"`

	// File path for SQLite databases
	Path string `jsonschema:"title=File Path (SQLite)"`

	// Size of database connection pool
	PoolSize int `mapstructure:"pool_size" jsonschema:"title=Connection Pool Size"`

	// Max number of active database connections allowed
	MaxConnections int `mapstructure:"max_connections" jsonschema:"title=Maximum Connections"`

	// Max time after which idle database connections are closed
	MaxConnIdleTime time.Duration `mapstructure:"max_connection_idle_time" jsonschema:"title=Connection Idel Time"`

	// Max time after which database connections are not reused
	MaxConnLifeTime time.Duration `mapstructure:"max_connection_life_time" jsonschema:"title=Connection Life Time"`

	// Database ping timeout is used for db health checking
	PingTimeout time.Duration `mapstructure:"ping_timeout" jsonschema:"title=Healthcheck Ping Timeout"`

	// Set up an secure TLS encrypted database connection
	EnableTLS bool `mapstructure:"enable_tls" jsonschema:"title=Enable TLS"`

	// Required for TLS. For example with Google Cloud SQL it's
	// <gcp-project-id>:<cloud-sql-instance>
	ServerName string `mapstructure:"server_name" jsonschema:"title=TLS Server Name"`

	// Required for TLS. Can be a file path or the contents of the PEM file
	ServerCert string `mapstructure:"server_cert" jsonschema:"title=Server Certificate"`

	// Required for TLS. Can be a file path or the contents of the PEM file
	ClientCert string `mapstructure:"client_cert" jsonschema:"title=Client Certificate"`

	// Required for TLS. Can be a file path or the contents of the pem file
	ClientKey string `mapstructure:"client_key" jsonschema:"title=Client Key"`

	// MSSQL: disable TLS encryption (default: true in go-mssqldb)
	Encrypt *bool `mapstructure:"encrypt" jsonschema:"title=MSSQL Encrypt"`

	// MSSQL: trust server certificate without validation
	TrustServerCertificate *bool `mapstructure:"trust_server_certificate" jsonschema:"title=MSSQL Trust Server Certificate"`

	// Snowflake key pair authentication (PKCS#8 PEM format).
	// Generate key: openssl genrsa 2048 | openssl pkcs8 -topk8 -inform PEM -out rsa_key.p8
	PrivateKeyPath string `mapstructure:"private_key_path" jsonschema:"title=Private Key File Path (Snowflake)"`
	PrivateKeyPEM  string `mapstructure:"private_key_pem" jsonschema:"title=Private Key PEM (Snowflake)"`
	KeyPassphrase  string `mapstructure:"key_passphrase" jsonschema:"title=Key Passphrase (Snowflake)"`

	// Cassandra / Amazon Keyspaces consistency level (default LOCAL_QUORUM).
	Consistency string `mapstructure:"consistency" jsonschema:"title=Cassandra Consistency Level"`
}

// RateLimiter sets the API rate limits
type RateLimiter struct {
	// The number of events per second
	Rate float64 `jsonschema:"title=Connection Rate"`

	// Bucket a burst of at most 'bucket' number of events
	Bucket int `jsonschema:"title=Bucket Size"`

	// The header that contains the client ip
	IPHeader string `mapstructure:"ip_header" jsonschema:"title=IP From HTTP Header,example=X-Forwarded-For"`
}

// MCPConfig configures the Model Context Protocol (MCP) server
// MCP enables AI assistants to interact with GraphJin via function calling
//
// Transport is implicit based on context:
// - CLI (`graphjin mcp`) uses stdio transport for Claude Desktop and CLI clients
// - HTTP service uses SSE/HTTP transport at /api/v1/mcp endpoint
//
// Authentication:
// - HTTP transport uses the same auth as GraphQL/REST (JWT, headers, etc.)
// - Stdio transport uses env vars (GRAPHJIN_USER_ID, GRAPHJIN_USER_ROLE) or config values
type MCPConfig struct {
	// Disable the MCP server. MCP is enabled by default except for legacy
	// database-only prod/agentic configs, where it must be explicitly enabled.
	Disable bool `mapstructure:"disable" jsonschema:"title=Disable MCP Server,default=false"`

	disableExplicit bool

	// OAuth exposes standards-compatible authorization discovery for remote MCP clients.
	OAuth MCPOAuthConfig `mapstructure:"oauth" jsonschema:"title=MCP OAuth Configuration"`

	// Allow mutation operations via raw MCP GraphQL execution.
	// Auto-enabled in dev mode. Default: false
	AllowMutations bool `mapstructure:"allow_mutations" jsonschema:"title=Allow Mutations,default=false"`

	// Allow arbitrary GraphQL queries (vs only saved queries from allow-list)
	// Auto-enabled in dev mode. Default: false
	AllowRawQueries bool `mapstructure:"allow_raw_queries" jsonschema:"title=Allow Raw Queries,default=false"`

	// IncludeToolsWithAgent keeps the low-level MCP tools (query_catalog,
	// graphql_help, execute_saved_query, execute_graphql, validate_where_clause,
	// schema tools) registered even when the ask_graphjin_agent tool is enabled.
	// Dev and agentic configs expose both the agent and primitives by default.
	// Set false to make the agent the only front-door tool.
	IncludeToolsWithAgent bool `mapstructure:"include_tools_with_agent" jsonschema:"title=Include MCP Tools With Agent,description=Parsed dev and agentic configs default to true; prod and direct Go configs remain false"`

	// HTTPStateful enables persistent Streamable HTTP MCP sessions. This is
	// required for server-initiated client sampling over HTTP and should be used
	// behind sticky sessions in multi-node deployments. It defaults on in dev
	// and agentic modes and remains off by default in prod.
	HTTPStateful bool `mapstructure:"http_stateful" jsonschema:"title=Stateful MCP HTTP Transport,description=Parsed dev and agentic configs default to true; prod and direct Go configs remain false"`

	// LegacyDiscovery keeps legacy HTTP helper endpoints such as
	// /api/v1/discovery and /api/v1/workflows available for compatibility.
	// It no longer expands the MCP tool surface; MCP is catalog-first.
	LegacyDiscovery bool `mapstructure:"legacy_discovery" jsonschema:"title=Enable Legacy Discovery Tools,default=false"`

	// Default user ID for stdio transport (CLI). Can be overridden by GRAPHJIN_USER_ID env var.
	StdioUserID string `mapstructure:"stdio_user_id" jsonschema:"title=Stdio User ID"`

	// Default user role for stdio transport (CLI). Can be overridden by GRAPHJIN_USER_ROLE env var.
	StdioUserRole string `mapstructure:"stdio_user_role" jsonschema:"title=Stdio User Role"`

	// Run in MCP-only mode - disables GraphQL, REST saved-query API, WebUI, and OpenAPI endpoints.
	// Health check and MCP endpoints remain available. Legacy HTTP helper endpoints
	// also remain available only when legacy_discovery is enabled.
	Only bool `mapstructure:"only" jsonschema:"title=MCP Only Mode,default=false"`

	// CursorCacheTTL in seconds for cursor ID cache (default: 1800 = 30 min)
	// Cursors are cached to allow short numeric IDs for LLM-friendly pagination
	CursorCacheTTL int `mapstructure:"cursor_cache_ttl" jsonschema:"title=Cursor Cache TTL,default=1800"`

	// CursorCacheSize max entries for in-memory cursor cache fallback (default: 10000)
	CursorCacheSize int `mapstructure:"cursor_cache_size" jsonschema:"title=Cursor Cache Size,default=10000"`

	// AllowConfigUpdates enables MCP tools that can modify GraphJin configuration
	// WARNING: Allows LLMs to change database connections, table configs, and roles
	// Only enable in trusted environments. Default: false
	AllowConfigUpdates bool `mapstructure:"allow_config_updates" jsonschema:"title=Allow Config Updates,default=false"`

	// AllowSchemaReload enables the MCP reload_schema tool for manual schema refresh
	// Useful when user adds tables and wants immediate discovery without waiting for poll
	// Default: false (auto-enabled in dev mode if not explicitly set)
	AllowSchemaReload bool `mapstructure:"allow_schema_reload" jsonschema:"title=Allow Schema Reload,default=false"`

	// AllowSchemaUpdates enables MCP tools to preview and apply database schema changes (DDL)
	// Uses GraphJin DDL (db.ddl) format. Auto-enabled in dev mode. Default: false
	AllowSchemaUpdates bool `mapstructure:"allow_schema_updates" jsonschema:"title=Allow Schema Updates,default=false"`

	// AllowWorkflowUpdates enables workflow writes through gj_workflow.
	// Saved workflows are discoverable via workflow catalog items. Execution
	// through gj_workflow_execution is controlled by normal read_only table/source
	// configuration because it is an insert-shaped managed mutation.
	// Auto-enabled in dev mode. Default: false
	AllowWorkflowUpdates bool `mapstructure:"allow_workflow_updates" jsonschema:"title=Allow Workflow Updates,default=false"`

	// AllowWorkflowExecution keeps the legacy HTTP workflow execution surface
	// available when legacy discovery is enabled. MCP workflow execution uses
	// catalog discovery plus gj_workflow_execution.
	// Auto-enabled in dev mode. Default: false
	AllowWorkflowExecution bool `mapstructure:"allow_workflow_execution" jsonschema:"title=Allow Workflow Execution,default=false"`

	// AllowDevTools enables advanced introspection MCP tools (explain_query, explore_relationships, audit_role_permissions).
	// These expose SQL, relationship graphs, and role permissions — useful for development/debugging.
	// Auto-enabled in dev mode. Default: false
	AllowDevTools bool `mapstructure:"allow_dev_tools" jsonschema:"title=Allow Dev Tools,default=false"`

	// DefaultDBAllowed when true allows configuring and discovering system/default
	// databases (e.g. postgres, mysql, master). Default: false
	DefaultDBAllowed bool `mapstructure:"default_db_allowed" jsonschema:"title=Allow Default Databases,default=false"`

	// WorkflowTimeout in seconds for JavaScript workflow execution.
	// Workflows that exceed this duration are interrupted. Default: 5
	WorkflowTimeout int `mapstructure:"workflow_timeout" jsonschema:"title=Workflow Timeout (seconds),default=5"`
}

type AgentConfig = gjagent.Config

// MCPOAuthConfig configures standards-compatible OAuth discovery and token
// handling for hosted MCP HTTP clients.
type MCPOAuthConfig struct {
	// Enable OAuth metadata/challenge support for the MCP HTTP endpoint.
	Enabled bool `mapstructure:"enabled" jsonschema:"title=Enable MCP OAuth,default=false"`

	// Mode selects the authorization-server strategy: builtin uses auth_login
	// to run an authorization-code + PKCE flow; external advertises the
	// configured AuthorizationServers and validates bearer tokens via auth.jwt.
	Mode string `mapstructure:"mode" jsonschema:"title=MCP OAuth Mode,enum=builtin,enum=external"`

	// Resource is the expected OAuth audience/resource value. Defaults to the
	// public /api/v1/mcp URL for the incoming request.
	Resource string `mapstructure:"resource" jsonschema:"title=MCP OAuth Resource"`

	// Issuer is the authorization server issuer. In builtin mode it defaults to
	// the public origin of the incoming request.
	Issuer string `mapstructure:"issuer" jsonschema:"title=MCP OAuth Issuer"`

	// AuthorizationServers are advertised by protected-resource metadata in
	// external mode. Defaults to Issuer when empty.
	AuthorizationServers []string `mapstructure:"authorization_servers" jsonschema:"title=MCP OAuth Authorization Servers"`

	// Scopes advertised and accepted by the MCP authorization server.
	Scopes []string `mapstructure:"scopes" jsonschema:"title=MCP OAuth Scopes"`

	// DynamicClientRegistration exposes RFC 7591 registration in builtin mode.
	DynamicClientRegistration bool `mapstructure:"dynamic_client_registration" jsonschema:"title=Enable MCP OAuth Dynamic Client Registration,default=true"`

	// ClientIDMetadataDocuments lets clients use CIMD URLs instead of DCR.
	ClientIDMetadataDocuments bool `mapstructure:"client_id_metadata_documents" jsonschema:"title=Enable MCP OAuth Client ID Metadata Documents,default=true"`

	// AccessTokenTTL controls builtin-mode access token lifetime.
	AccessTokenTTL time.Duration `mapstructure:"access_token_ttl" jsonschema:"title=MCP OAuth Access Token TTL"`

	// RefreshTokenTTL controls builtin-mode refresh token lifetime.
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl" jsonschema:"title=MCP OAuth Refresh Token TTL"`
}

// RedisConfig configures Redis connection
type RedisConfig struct {
	// Redis connection URL (e.g., redis://localhost:6379/0)
	URL string `mapstructure:"url" jsonschema:"title=Redis URL"`
}

// DiscoveryCacheConfig configures service-owned schema discovery generations.
// Enabled is a pointer so an embedded Go configuration can distinguish an
// omitted value (enabled) from an explicit false.
type DiscoveryCacheConfig struct {
	// Enabled starts GraphJin Service from validated filesystem generations and
	// moves schema refreshes onto the coordinated generation path.
	Enabled *bool `mapstructure:"enabled" jsonschema:"title=Enable Discovery Cache,default=true"`

	// Path stores immutable full schema snapshots, manifests, activation
	// receipts, and semantic indexes. Horizontal replicas must share this path.
	Path string `mapstructure:"path" jsonschema:"title=Discovery Cache Path,default=.graphjin/discovery"`

	// RefreshInterval controls background live-schema checks after startup.
	RefreshInterval time.Duration `mapstructure:"refresh_interval" jsonschema:"title=Discovery Refresh Interval,default=5m"`

	// StartupWait bounds cold follower and explicit refresh activation waits.
	StartupWait time.Duration `mapstructure:"startup_wait" jsonschema:"title=Discovery Startup Wait,default=2m"`

	// RetainGenerations is the number of activated generations kept when the
	// configured filesystem supports deletion.
	RetainGenerations int `mapstructure:"retain_generations" jsonschema:"title=Retained Discovery Generations,default=2"`
}

func (c DiscoveryCacheConfig) enabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// CatalogSearchConfig configures catalog retrieval extensions.
type CatalogSearchConfig struct {
	Semantic SemanticCatalogSearchConfig `mapstructure:"semantic" jsonschema:"title=Semantic Catalog Search"`
}

// SemanticCatalogSearchConfig configures the service-owned Ax embedding layer.
type SemanticCatalogSearchConfig struct {
	// Enabled adds Ax-backed semantic recall to the lexical catalog baseline.
	Enabled bool `mapstructure:"enabled" jsonschema:"title=Enable Semantic Catalog Search,default=false"`

	// Provider is the Ax embedding provider name.
	Provider string `mapstructure:"provider" jsonschema:"title=Embedding Provider,default=openai"`

	// EmbeddingModel is required when semantic search is enabled and forms part
	// of the persisted embedding-space fingerprint.
	EmbeddingModel string `mapstructure:"embedding_model" jsonschema:"title=Embedding Model"`

	// APIKeyEnv names the environment variable containing the provider key.
	APIKeyEnv string `mapstructure:"api_key_env" jsonschema:"title=Embedding API Key Environment Variable,default=OPENAI_API_KEY"`

	// BaseURL optionally overrides the provider endpoint and forms part of the
	// persisted embedding-space fingerprint.
	BaseURL string `mapstructure:"base_url" jsonschema:"title=Embedding Provider Base URL"`

	// Dimensions selects tiny (128), small (256), medium (512), or the
	// provider-native default. Named sizes are validated strictly.
	Dimensions string `mapstructure:"dimensions" jsonschema:"title=Embedding Dimensions,default=tiny,enum=tiny,enum=small,enum=medium,enum=default"`
}

func (c SemanticCatalogSearchConfig) dimensionCount() (int, bool, error) {
	switch strings.ToLower(strings.TrimSpace(c.Dimensions)) {
	case "", "tiny":
		return 128, true, nil
	case "small":
		return 256, true, nil
	case "medium":
		return 512, true, nil
	case "default":
		return 0, false, nil
	default:
		return 0, false, fmt.Errorf("catalog_search.semantic.dimensions must be tiny, small, medium, or default")
	}
}

func normalizeDiscoveryAndSemanticConfig(c *Config) error {
	if c == nil {
		return nil
	}
	d := &c.DiscoveryCache
	if strings.TrimSpace(d.Path) == "" {
		d.Path = ".graphjin/discovery"
	}
	if d.RefreshInterval <= 0 {
		d.RefreshInterval = 5 * time.Minute
	}
	if d.StartupWait <= 0 {
		d.StartupWait = 2 * time.Minute
	}
	if d.RetainGenerations <= 0 {
		d.RetainGenerations = 2
	}

	sem := &c.CatalogSearch.Semantic
	if strings.TrimSpace(sem.Provider) == "" {
		sem.Provider = "openai"
	}
	if strings.TrimSpace(sem.APIKeyEnv) == "" {
		sem.APIKeyEnv = "OPENAI_API_KEY"
	}
	if strings.TrimSpace(sem.Dimensions) == "" {
		sem.Dimensions = "tiny"
	}
	if _, _, err := sem.dimensionCount(); err != nil {
		return err
	}
	if sem.Enabled && strings.TrimSpace(sem.EmbeddingModel) == "" {
		return fmt.Errorf("catalog_search.semantic.embedding_model is required when semantic search is enabled")
	}
	return nil
}

// RuntimeEventsConfig configures bounded agentic runtime observability rows.
type RuntimeEventsConfig struct {
	// MaxEvents is the maximum number of recent event rows retained.
	MaxEvents int `mapstructure:"max_events" jsonschema:"title=Runtime Event Limit,default=1000"`

	// TTLSeconds controls how long event rows are retained.
	TTLSeconds int `mapstructure:"ttl_seconds" jsonschema:"title=Runtime Event TTL Seconds,default=86400"`
}

// UploadsConfig configures the multipart-form upload endpoint following
// the graphql-multipart-request-spec
// (https://github.com/jaydenseric/graphql-multipart-request-spec).
//
// When Enabled, /api/v1/graphql accepts multipart/form-data POSTs in
// addition to application/json. Files are then either:
//
//  1. Inlined as base64 into the variable at its declared path
//     (default — Storage is empty):
//
//     { filename, content_type, size, data (base64) }
//
//  2. Streamed to a configured filesystem table (Storage="avatars"),
//     with the variable replaced by the resulting object metadata:
//
//     { key, content_type, size, url, etag, modified_at }
//
// Mode 2 keeps large bodies out of the GraphQL request/response and
// lets mutations write a single JSONB column whose value is a stable,
// queryable file reference.
type UploadsConfig struct {
	// Enable multipart/form-data parsing on /api/v1/graphql
	Enabled bool `mapstructure:"enabled" jsonschema:"title=Enable Uploads,default=false"`

	// MaxSize is the per-request multipart body limit in bytes.
	// Defaults to 25 MB when zero.
	MaxSize int64 `mapstructure:"max_size" jsonschema:"title=Max Upload Size (bytes),default=26214400"`

	// AllowedMIME, when non-empty, restricts the accepted content types.
	// Glob patterns are supported: "image/*", "application/pdf".
	AllowedMIME []string `mapstructure:"allowed_mime" jsonschema:"title=Allowed MIME Types"`

	// Storage names a filesystems[].name to stream uploads into. When
	// empty, the legacy inline-base64 mode is used. When set, files
	// are written to the backend and the variable becomes the object
	// metadata instead of inlined data.
	Storage string `mapstructure:"storage" jsonschema:"title=Filesystem Table for Streaming"`

	// StorageKeyPrefix is prepended to the backend key for every
	// uploaded file. Useful for namespacing by tenant or role.
	// Example: "avatars/{date}/" — the {date} marker is substituted
	// at request time with YYYY/MM/DD.
	StorageKeyPrefix string `mapstructure:"storage_key_prefix" jsonschema:"title=Storage Key Prefix"`
}

// CachingConfig configures response caching
// Caching is enabled by default using in-memory LRU cache
// Configure redis.url to use Redis for distributed caching
// Responses are cached with automatic invalidation when mutations modify related tables
type CachingConfig struct {
	// Disable response caching (caching is enabled by default)
	Disable bool `mapstructure:"disable" jsonschema:"title=Disable Caching,default=false"`

	// Default TTL for cached responses in seconds (hard TTL)
	TTL int `mapstructure:"ttl" jsonschema:"title=Cache TTL,default=3600"`

	// Soft TTL for stale-while-revalidate in seconds (0 = disabled)
	FreshTTL int `mapstructure:"fresh_ttl" jsonschema:"title=Fresh TTL for SWR,default=300"`

	// Tables to exclude from caching
	ExcludeTables []string `mapstructure:"exclude_tables" jsonschema:"title=Exclude Tables"`
}

// Telemetry struct contains OpenCensus metrics and tracing related config
/*
type Telemetry struct {
	// Debug enables debug logging for metrics and tracing data.
	Debug bool

	// Interval to send out metrics and tracing data
	Interval *time.Duration

	Metrics struct {
		// Exporter is the name of the metrics exporter to use. Example: prometheus
		Exporter string

		// Endpoint to send the data to.
		Endpoint string

		// Namespace is set based on your exporter configration
		Namespace string

		// Key is set based on your exporter configuration
		Key string
	}

	Tracing struct {
		// Exporter is the name of the tracing exporter to use. Example: zipkin
		Exporter string

		// Endpoint to send the data to. Example: http://zipkin:9411/api/v3/spans
		Endpoint string

		// Sample sets how many requests to sample for tracing: Example: 0.6
		Sample string

		// IncludeQuery when set the GraphQL query is included in the tracing data
		IncludeQuery bool `mapstructure:"include_query"`

		// IncludeParams when set variables used with the query are included in the
		// tracing data
		IncludeParams bool `mapstructure:"include_params"`

		// ExcludeHealthCheck when set health check tracing is excluded from the
		// tracing data
		ExcludeHealthCheck bool `mapstructure:"exclude_health_check"`
	}
}
*/

// ReadInConfig function reads in the config file for the environment specified in the GO_ENV
// environment variable. This is the best way to create a new GraphJin config.
func ReadInConfig(configFile string) (*Config, error) {
	return readInConfig(configFile, nil)
}

// ReadInConfigFS is the same as ReadInConfig but it also takes a filesytem as an argument
func ReadInConfigFS(configFile string, fs afero.Fs) (*Config, error) {
	return readInConfig(configFile, fs)
}

// readInConfig function reads in the config file for the environment specified in the GO_ENV
func readInConfig(configFile string, fs afero.Fs) (*Config, error) {
	cp := filepath.Dir(configFile)
	configName := filepath.Base(configFile)
	selectedMode := modeFromConfigName(configName)
	viper := newViper(cp, configName)

	if fs != nil {
		viper.SetFs(fs)
	}

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	if pcf := viper.GetString("inherits"); pcf != "" {
		cf := viper.ConfigFileUsed()
		viper = newViper(cp, pcf)
		if fs != nil {
			viper.SetFs(fs)
		}

		if err := viper.ReadInConfig(); err != nil {
			return nil, err
		}

		if value := viper.GetString("inherits"); value != "" {
			return nil, fmt.Errorf("inherited config '%s' cannot itself inherit '%s'", pcf, value)
		}

		viper.SetConfigFile(cf)

		if err := viper.MergeInConfig(); err != nil {
			return nil, err
		}
	}

	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GJ_") || strings.HasPrefix(e, "SJ_") {
			kv := strings.SplitN(e, "=", 2)
			util.SetKeyValue(viper, kv[0], kv[1])
		}
	}
	applyConfigNameMode(viper, selectedMode)

	config := &Config{viper: viper}
	config.ConfigPath = cp
	config.parsedConfig = true
	config.explicitSettings = captureRuntimeDefaultExplicitSettings(viper)

	webUIExplicit := webUISettingExplicit(viper)
	if err := normalizeCatalogAutoBools(viper); err != nil {
		return nil, err
	}
	if err := viper.Unmarshal(&config, capabilityMapDecodeOption()); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}
	if err := normalizeConfigMode(config); err != nil {
		return nil, err
	}
	applyRuntimeModeDefaults(config)
	if err := normalizeDiscoveryAndSemanticConfig(config); err != nil {
		return nil, err
	}
	normalizeWebUIDefault(config, webUIExplicit)
	config.MCP.disableExplicit = viper.IsSet("mcp.disable")
	config.webUIExplicit = webUIExplicit

	return config, nil
}

// NewConfig function creates a new GraphJin configuration from the provided config string
func NewConfig(config, format string) (*Config, error) {
	if format == "" {
		format = "yaml"
	}

	// migrate old sg var prefixes to new gj prefixes
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "SG_") {
			continue
		}
		v := strings.SplitN(e, "=", 2)
		if err := os.Setenv(("GJ_" + v[0][3:]), v[1]); err != nil {
			return nil, err
		}
	}

	viper := newViperWithDefaults()
	viper.SetConfigType(format)

	if err := viper.ReadConfig(strings.NewReader(config)); err != nil {
		return nil, err
	}

	c := &Config{viper: viper, parsedConfig: true}
	c.explicitSettings = captureRuntimeDefaultExplicitSettings(viper)

	webUIExplicit := webUISettingExplicit(viper)
	if err := normalizeCatalogAutoBools(viper); err != nil {
		return nil, err
	}
	if err := viper.Unmarshal(&c, capabilityMapDecodeOption()); err != nil {
		return nil, fmt.Errorf("failed to decode config, %v", err)
	}
	if err := normalizeConfigMode(c); err != nil {
		return nil, err
	}
	applyRuntimeModeDefaults(c)
	if err := normalizeDiscoveryAndSemanticConfig(c); err != nil {
		return nil, err
	}
	normalizeWebUIDefault(c, webUIExplicit)
	c.MCP.disableExplicit = viper.IsSet("mcp.disable")
	c.webUIExplicit = webUIExplicit

	return c, nil
}

var boolMapType = reflect.TypeOf(map[string]bool{})

// capabilityMapDecodeOption preserves dotted capability names such as
// catalog.read. Viper treats dots as path separators in ordinary maps, so the
// decoded value can arrive as {catalog: {read: true}} even though capability
// keys are intentionally flat.
func capabilityMapDecodeOption() viper.DecoderConfigOption {
	return func(config *mapstructure.DecoderConfig) {
		config.DecodeHook = mapstructure.ComposeDecodeHookFunc(
			config.DecodeHook,
			mapstructure.DecodeHookFuncType(flattenBoolMapDecodeHook),
		)
	}
}

func flattenBoolMapDecodeHook(from, to reflect.Type, data any) (any, error) {
	if from == nil || to != boolMapType || from.Kind() != reflect.Map {
		return data, nil
	}
	out := make(map[string]bool)
	if err := flattenBoolMapValue("", reflect.ValueOf(data), out); err != nil {
		return nil, err
	}
	return out, nil
}

func flattenBoolMapValue(prefix string, value reflect.Value, out map[string]bool) error {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return fmt.Errorf("capability %q must be true or false", prefix)
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return fmt.Errorf("capability %q must be true or false", prefix)
	}
	switch value.Kind() {
	case reflect.Map:
		iter := value.MapRange()
		for iter.Next() {
			key := strings.TrimSpace(fmt.Sprint(iter.Key().Interface()))
			if key == "" {
				return fmt.Errorf("capability key cannot be empty")
			}
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if err := flattenBoolMapValue(path, iter.Value(), out); err != nil {
				return err
			}
		}
		return nil
	case reflect.Bool:
		out[prefix] = value.Bool()
		return nil
	case reflect.String:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value.String()))
		if err != nil {
			return fmt.Errorf("capability %q must be true or false", prefix)
		}
		out[prefix] = parsed
		return nil
	default:
		return fmt.Errorf("capability %q must be true or false", prefix)
	}
}

var runtimeDefaultKeys = []string{
	"artifacts.enabled",
	"artifacts.source",
	"watches.enabled",
	"watches.runner",
	"tasks.enabled",
	"agent.enabled",
	"agent.sampling",
	"mcp.http_stateful",
	"mcp.include_tools_with_agent",
}

func captureRuntimeDefaultExplicitSettings(v *viper.Viper) map[string]bool {
	explicit := make(map[string]bool, len(runtimeDefaultKeys))
	for _, key := range runtimeDefaultKeys {
		explicit[key] = configSettingExplicit(v, key)
	}
	return explicit
}

func configSettingExplicit(v *viper.Viper, key string) bool {
	if v != nil && v.InConfig(key) {
		return true
	}
	envKey := strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	for _, prefix := range []string{"GJ_", "SG_", "SJ_"} {
		if value, ok := os.LookupEnv(prefix + envKey); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func (c *Config) settingExplicit(key string) bool {
	if c == nil {
		return false
	}
	if c.explicitSettings != nil {
		if explicit, ok := c.explicitSettings[key]; ok {
			return explicit
		}
	}
	return configSettingExplicit(c.viper, key)
}

// applyRuntimeModeDefaults makes parsed dev and agentic configurations
// batteries-included while preserving every explicit YAML/environment value.
// Production and directly-constructed Go configurations retain literal values.
func applyRuntimeModeDefaults(c *Config) {
	if c == nil || !c.parsedConfig || (c.Core.Mode != "dev" && c.Core.Mode != "agentic") {
		return
	}

	artifactsEnabledExplicit := c.settingExplicit("artifacts.enabled")
	artifactsSourceExplicit := c.settingExplicit("artifacts.source")
	if !artifactsEnabledExplicit {
		c.Core.Artifacts.Enabled = true
		if !artifactsSourceExplicit {
			c.Core.Artifacts.Source = ""
			c.managedArtifactStore = true
		}
	}
	// An explicit enable without a source is the legacy contract: core will
	// select the first writable SQL source instead of moving existing data.
	if artifactsEnabledExplicit || artifactsSourceExplicit {
		c.managedArtifactStore = false
	}

	if !c.settingExplicit("watches.enabled") {
		c.Core.Watches.Enabled = c.Core.Artifacts.Enabled
	}
	if c.Core.Watches.Enabled && !c.settingExplicit("watches.runner") {
		c.Core.Watches.Runner = "all"
	}
	if !c.settingExplicit("tasks.enabled") {
		c.Core.Tasks.Enabled = c.Core.Artifacts.Enabled
	}

	if !c.settingExplicit("agent.enabled") {
		c.Agent.Enabled = true
	}
	if !c.settingExplicit("mcp.http_stateful") {
		c.MCP.HTTPStateful = true
	}
	if !c.settingExplicit("mcp.include_tools_with_agent") {
		c.MCP.IncludeToolsWithAgent = true
	}
}

func normalizeWebUIDefault(c *Config, explicit bool) {
	if c == nil || explicit {
		return
	}
	c.Serv.WebUI = c.Core.Mode == "dev" || c.Core.Mode == "agentic"
}

func webUISettingExplicit(v *viper.Viper) bool {
	if v == nil {
		return false
	}
	if v.InConfig("web_ui") {
		return true
	}
	for _, key := range []string{"GJ_WEB_UI", "SG_WEB_UI", "SJ_WEB_UI"} {
		if value, ok := os.LookupEnv(key); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func normalizeCatalogAutoBools(v *viper.Viper) error {
	prod, err := productionFromViperMode(v)
	if err != nil {
		return err
	}
	catalogEnabledAuto := !prod
	if err := normalizeAutoBoolSetting(v, "catalog.enabled", catalogEnabledAuto); err != nil {
		return err
	}

	autoCodeRelationsAuto := catalogEnabledAuto
	if v.IsSet("catalog.enabled") {
		autoCodeRelationsAuto = v.GetBool("catalog.enabled")
	}
	return normalizeAutoBoolSetting(v, "catalog.auto_code_relations", autoCodeRelationsAuto)
}

func applyConfigNameMode(v *viper.Viper, mode string) {
	if mode == "" {
		return
	}
	if mode == "agentic" {
		v.Set("mode", mode)
		return
	}
	if !v.IsSet("mode") {
		v.Set("mode", mode)
	}
}

func modeFromConfigName(configFile string) string {
	name := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(filepath.Base(configFile), filepath.Ext(configFile))))
	switch name {
	case "development", "dev":
		return "dev"
	case "production", "prod":
		return "prod"
	case "agent", "agentic":
		return "agentic"
	default:
		return ""
	}
}

func normalizeConfigMode(c *Config) error {
	if c == nil {
		return nil
	}
	if c.Serv.Production {
		c.Core.Production = true
	}
	if err := c.Core.NormalizeMode(); err != nil {
		return err
	}
	return nil
}

func productionFromViperMode(v *viper.Viper) (bool, error) {
	mode, err := core.CanonicalMode(v.GetString("mode"))
	if err != nil {
		return false, err
	}
	switch mode {
	case "":
		return v.GetBool("production"), nil
	case "dev":
		return false, nil
	case "prod", "agentic":
		return true, nil
	default:
		return false, nil
	}
}

func normalizeAutoBoolSetting(v *viper.Viper, key string, autoValue bool) error {
	if !v.IsSet(key) {
		return nil
	}

	raw := strings.TrimSpace(strings.ToLower(v.GetString(key)))
	switch raw {
	case "", "auto":
		v.Set(key, autoValue)
	case "true", "1", "yes", "on":
		v.Set(key, true)
	case "false", "0", "no", "off":
		v.Set(key, false)
	default:
		return fmt.Errorf("%s must be true, false, or auto", key)
	}
	return nil
}

// newViperWithDefaults returns a new viper instance with the default settings
func newViperWithDefaults() *viper.Viper {
	vi := viper.New()

	vi.SetDefault("host_port", "0.0.0.0:8080")
	vi.SetDefault("web_ui", false)
	vi.SetDefault("enable_tracing", false)
	vi.SetDefault("auth_fail_block", false)
	vi.SetDefault("seed_file", "seed.js")

	vi.SetDefault("log_level", "info")
	vi.SetDefault("log_format", "auto")

	vi.SetDefault("default_block", true)

	vi.SetDefault("database.type", "postgres")
	vi.SetDefault("database.host", "localhost")
	vi.SetDefault("database.port", 5432)
	vi.SetDefault("database.user", "postgres")
	vi.SetDefault("database.password", "")
	vi.SetDefault("database.schema", "public")
	vi.SetDefault("database.pool_size", 10)

	vi.SetDefault("discovery_cache.enabled", true)
	vi.SetDefault("discovery_cache.path", ".graphjin/discovery")
	vi.SetDefault("discovery_cache.refresh_interval", 5*time.Minute)
	vi.SetDefault("discovery_cache.startup_wait", 2*time.Minute)
	vi.SetDefault("discovery_cache.retain_generations", 2)

	vi.SetDefault("catalog_search.semantic.enabled", false)
	vi.SetDefault("catalog_search.semantic.provider", "openai")
	vi.SetDefault("catalog_search.semantic.embedding_model", "")
	vi.SetDefault("catalog_search.semantic.api_key_env", "OPENAI_API_KEY")
	vi.SetDefault("catalog_search.semantic.base_url", "")
	vi.SetDefault("catalog_search.semantic.dimensions", "tiny")

	// These section names contain underscores, so the generic GJ_* underscore-
	// to-dot compatibility mapper cannot identify their dotted boundary
	// unambiguously. Bind every public setting explicitly so container and smoke
	// deployments can override the YAML without generating a temporary config.
	vi.BindEnv("discovery_cache.enabled", "GJ_DISCOVERY_CACHE_ENABLED", "SG_DISCOVERY_CACHE_ENABLED", "SJ_DISCOVERY_CACHE_ENABLED")                                                                 //nolint:errcheck
	vi.BindEnv("discovery_cache.path", "GJ_DISCOVERY_CACHE_PATH", "SG_DISCOVERY_CACHE_PATH", "SJ_DISCOVERY_CACHE_PATH")                                                                             //nolint:errcheck
	vi.BindEnv("discovery_cache.refresh_interval", "GJ_DISCOVERY_CACHE_REFRESH_INTERVAL", "SG_DISCOVERY_CACHE_REFRESH_INTERVAL", "SJ_DISCOVERY_CACHE_REFRESH_INTERVAL")                             //nolint:errcheck
	vi.BindEnv("discovery_cache.startup_wait", "GJ_DISCOVERY_CACHE_STARTUP_WAIT", "SG_DISCOVERY_CACHE_STARTUP_WAIT", "SJ_DISCOVERY_CACHE_STARTUP_WAIT")                                             //nolint:errcheck
	vi.BindEnv("discovery_cache.retain_generations", "GJ_DISCOVERY_CACHE_RETAIN_GENERATIONS", "SG_DISCOVERY_CACHE_RETAIN_GENERATIONS", "SJ_DISCOVERY_CACHE_RETAIN_GENERATIONS")                     //nolint:errcheck
	vi.BindEnv("catalog_search.semantic.enabled", "GJ_CATALOG_SEARCH_SEMANTIC_ENABLED", "SG_CATALOG_SEARCH_SEMANTIC_ENABLED", "SJ_CATALOG_SEARCH_SEMANTIC_ENABLED")                                 //nolint:errcheck
	vi.BindEnv("catalog_search.semantic.provider", "GJ_CATALOG_SEARCH_SEMANTIC_PROVIDER", "SG_CATALOG_SEARCH_SEMANTIC_PROVIDER", "SJ_CATALOG_SEARCH_SEMANTIC_PROVIDER")                             //nolint:errcheck
	vi.BindEnv("catalog_search.semantic.embedding_model", "GJ_CATALOG_SEARCH_SEMANTIC_EMBEDDING_MODEL", "SG_CATALOG_SEARCH_SEMANTIC_EMBEDDING_MODEL", "SJ_CATALOG_SEARCH_SEMANTIC_EMBEDDING_MODEL") //nolint:errcheck
	vi.BindEnv("catalog_search.semantic.api_key_env", "GJ_CATALOG_SEARCH_SEMANTIC_API_KEY_ENV", "SG_CATALOG_SEARCH_SEMANTIC_API_KEY_ENV", "SJ_CATALOG_SEARCH_SEMANTIC_API_KEY_ENV")                 //nolint:errcheck
	vi.BindEnv("catalog_search.semantic.base_url", "GJ_CATALOG_SEARCH_SEMANTIC_BASE_URL", "SG_CATALOG_SEARCH_SEMANTIC_BASE_URL", "SJ_CATALOG_SEARCH_SEMANTIC_BASE_URL")                             //nolint:errcheck
	vi.BindEnv("catalog_search.semantic.dimensions", "GJ_CATALOG_SEARCH_SEMANTIC_DIMENSIONS", "SG_CATALOG_SEARCH_SEMANTIC_DIMENSIONS", "SJ_CATALOG_SEARCH_SEMANTIC_DIMENSIONS")                     //nolint:errcheck

	vi.SetDefault("env", "development")

	vi.BindEnv("env", "GO_ENV")                                 //nolint:errcheck
	vi.BindEnv("host", "HOST")                                  //nolint:errcheck
	vi.BindEnv("port", "PORT")                                  //nolint:errcheck
	vi.BindEnv("web_ui", "GJ_WEB_UI", "SG_WEB_UI", "SJ_WEB_UI") //nolint:errcheck

	vi.SetDefault("auth.subs_creds_in_vars", false)

	// MCP defaults. mcp.disable intentionally has no viper default so we can
	// tell whether production legacy configs explicitly enabled MCP.
	vi.SetDefault("mcp.legacy_discovery", false)
	vi.SetDefault("mcp.only", false)
	vi.SetDefault("mcp.include_tools_with_agent", false)
	vi.SetDefault("mcp.http_stateful", false)
	vi.SetDefault("mcp.cursor_cache_ttl", 1800)   // 30 minutes
	vi.SetDefault("mcp.cursor_cache_size", 10000) // max in-memory entries
	vi.SetDefault("mcp.oauth.enabled", false)
	vi.SetDefault("mcp.oauth.mode", "external")
	vi.SetDefault("mcp.oauth.scopes", []string{"mcp"})
	vi.SetDefault("mcp.oauth.dynamic_client_registration", true)
	vi.SetDefault("mcp.oauth.client_id_metadata_documents", true)
	vi.SetDefault("mcp.oauth.access_token_ttl", time.Hour)
	vi.SetDefault("mcp.oauth.refresh_token_ttl", 720*time.Hour)

	// Server-side agent defaults. Parsed dev and agentic configs enable the
	// endpoint and MCP tool through applyRuntimeModeDefaults.
	vi.SetDefault("agent.enabled", false)
	vi.SetDefault("agent.provider", "openai")
	vi.SetDefault("agent.model", "")
	vi.SetDefault("agent.api_key_env", "OPENAI_API_KEY")
	vi.SetDefault("agent.base_url", "")
	// Sampling is deprecated as a selection mode. Empty, auto, and require all
	// mean automatic server-first resolution; only off remains an opt-out.
	vi.SetDefault("agent.max_steps", 8)
	vi.SetDefault("agent.timeout_seconds", 50)
	vi.SetDefault("agent.read_only", false)
	vi.SetDefault("agent.return_trace", false)
	vi.SetDefault("agent.seed_limit", 10)
	vi.SetDefault("agent.catalog_default_limit", 20)
	// model and base_url both contain optional/underscored paths that are used
	// heavily by local OpenAI-compatible smoke and Ollama deployments. Bind them
	// explicitly so environment-only configurations do not silently fall back to
	// the public provider endpoint.
	vi.BindEnv("agent.model", "GJ_AGENT_MODEL", "SG_AGENT_MODEL", "SJ_AGENT_MODEL")                                                                     //nolint:errcheck
	vi.BindEnv("agent.base_url", "GJ_AGENT_BASE_URL", "SG_AGENT_BASE_URL", "SJ_AGENT_BASE_URL")                                                         //nolint:errcheck
	vi.BindEnv("agent.enabled", "GJ_AGENT_ENABLED", "SG_AGENT_ENABLED", "SJ_AGENT_ENABLED")                                                             //nolint:errcheck
	vi.BindEnv("agent.sampling", "GJ_AGENT_SAMPLING", "SG_AGENT_SAMPLING", "SJ_AGENT_SAMPLING")                                                         //nolint:errcheck
	vi.BindEnv("agent.api_key_env", "GJ_AGENT_API_KEY_ENV", "SG_AGENT_API_KEY_ENV", "SJ_AGENT_API_KEY_ENV")                                             //nolint:errcheck
	vi.BindEnv("mcp.http_stateful", "GJ_MCP_HTTP_STATEFUL", "SG_MCP_HTTP_STATEFUL", "SJ_MCP_HTTP_STATEFUL")                                             //nolint:errcheck
	vi.BindEnv("mcp.include_tools_with_agent", "GJ_MCP_INCLUDE_TOOLS_WITH_AGENT", "SG_MCP_INCLUDE_TOOLS_WITH_AGENT", "SJ_MCP_INCLUDE_TOOLS_WITH_AGENT") //nolint:errcheck

	// Local encrypted keystore defaults.
	vi.SetDefault("secrets.keystore.key", "")
	vi.SetDefault("secrets.keystore.path", "")

	// Artifact store defaults.
	vi.SetDefault("artifacts.auto_init", true)
	vi.SetDefault("artifacts.schema", "_graphjin")
	vi.SetDefault("artifacts.globals_path", "./config")
	vi.SetDefault("artifacts.poll_seconds", 15)
	vi.BindEnv("artifacts.enabled", "GJ_ARTIFACTS_ENABLED", "SG_ARTIFACTS_ENABLED", "SJ_ARTIFACTS_ENABLED") //nolint:errcheck
	vi.BindEnv("artifacts.source", "GJ_ARTIFACTS_SOURCE", "SG_ARTIFACTS_SOURCE", "SJ_ARTIFACTS_SOURCE")     //nolint:errcheck
	vi.BindEnv("watches.enabled", "GJ_WATCHES_ENABLED", "SG_WATCHES_ENABLED", "SJ_WATCHES_ENABLED")         //nolint:errcheck
	vi.BindEnv("watches.runner", "GJ_WATCHES_RUNNER", "SG_WATCHES_RUNNER", "SJ_WATCHES_RUNNER")             //nolint:errcheck
	vi.BindEnv("tasks.enabled", "GJ_TASKS_ENABLED", "SG_TASKS_ENABLED", "SJ_TASKS_ENABLED")                 //nolint:errcheck

	// Caching defaults
	vi.SetDefault("caching.enable", false)
	vi.SetDefault("caching.ttl", 3600)
	vi.SetDefault("caching.fresh_ttl", 300)

	// Built-in login defaults
	vi.SetDefault("auth_login.enabled", false)
	vi.SetDefault("auth_login.token_ttl", "720h")
	vi.SetDefault("auth_login.audience", "graphjin-cli")

	return vi
}

// newViper returns a new viper instance with the default settings
func newViper(configPath, configFile string) *viper.Viper {
	vi := newViperWithDefaults()
	vi.SetConfigName(strings.TrimSuffix(configFile, filepath.Ext(configFile)))

	if configPath == "" {
		vi.AddConfigPath("./config")
	} else {
		vi.AddConfigPath(configPath)
	}

	return vi
}

// func (c *Config) telemetryEnabled() bool {
// 	return c.Telemetry.Debug || c.Telemetry.Metrics.Exporter != "" || c.Telemetry.Tracing.Exporter != ""
// }

// AbsolutePath returns the absolute path of the file
func (c *Config) AbsolutePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.ConfigPath, p)
}

// EffectiveSettings returns all config settings after defaults, one-level
// inheritance, and GJ_/SJ_ environment overrides have been applied. Keys are
// viper's canonical lower-cased dotted keys. Intended for tooling such as
// `graphjin config get` and `graphjin config explain`.
func (c *Config) EffectiveSettings() map[string]any {
	if c == nil || c.viper == nil {
		return map[string]any{}
	}
	settings := c.viper.AllSettings()
	// Mode-aware defaults are applied after unmarshalling so direct Go struct
	// construction keeps literal semantics. Overlay those resolved values for
	// config tooling without persisting the private managed-store identifier.
	setEffectiveSetting(settings, "artifacts.enabled", c.Core.Artifacts.Enabled)
	setEffectiveSetting(settings, "watches.enabled", c.Core.Watches.Enabled)
	setEffectiveSetting(settings, "watches.runner", c.Core.Watches.Runner)
	setEffectiveSetting(settings, "tasks.enabled", c.Core.Tasks.Enabled)
	setEffectiveSetting(settings, "agent.enabled", c.Agent.Enabled)
	setEffectiveSetting(settings, "mcp.http_stateful", c.MCP.HTTPStateful)
	setEffectiveSetting(settings, "mcp.include_tools_with_agent", c.MCP.IncludeToolsWithAgent)
	return settings
}

func setEffectiveSetting(settings map[string]any, dotted string, value any) {
	parts := strings.Split(dotted, ".")
	current := settings
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

// SetHash sets the hash value of the configuration
func (c *Config) SetHash(hash string) {
	c.hash = hash
}

// SetName sets the name of the configuration
func (c *Config) SetName(name string) {
	c.name = name
}

// rateLimiterEnable returns true if the rate limiter is enabled
func (c *Config) rateLimiterEnable() bool {
	return c.RateLimiter.Rate > 0 && c.RateLimiter.Bucket > 0
}

// ShouldUseJSONLogs returns true if logs should be in JSON format.
// Returns true if log_format is "json" OR if log_format is "auto" and production mode is enabled.
// Returns false otherwise (colored console output for dev mode).
func (c *Config) ShouldUseJSONLogs() bool {
	if c.LogFormat == "json" {
		return true
	}
	if c.LogFormat == "auto" && c.Serv.Production {
		return true
	}
	return false
}

// GetConfigName returns the name of the configuration
func GetConfigName() string {
	goEnv := strings.TrimSpace(strings.ToLower(os.Getenv("GO_ENV")))

	switch goEnv {
	case "production", "prod":
		return "prod"

	case "agent", "agentic":
		return "agentic"

	case "staging", "stage":
		return "stage"

	case "testing", "test":
		return "test"

	case "development", "dev", "":
		return "dev"

	default:
		return goEnv
	}
}
