package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3/featurecap"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/openapi"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

// DefaultDBName is the canonical name used for the primary/default database
// after config normalization. It replaces the empty-string and "_default" sentinels.
const DefaultDBName = "default"

// SupportedDBTypes lists the database types supported for single-database mode
var SupportedDBTypes = []string{"postgres", "mysql", "mariadb", "sqlite", "oracle", "mssql", "mongodb", "snowflake", "bigquery", "redshift", "nanodb", "cassandra", "clickhouse"}

// SupportedMultiDBTypes lists the database types supported for multi-database mode
var SupportedMultiDBTypes = []string{"postgres", "mysql", "mariadb", "sqlite", "oracle", "mongodb", "mssql", "snowflake", "bigquery", "redshift", "nanodb", "cassandra", "clickhouse"}

// CanonicalMode normalizes the public top-level mode value.
func CanonicalMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "":
		return "", nil
	case "dev", "development":
		return sourcecap.ModeDev, nil
	case "prod", "production":
		return sourcecap.ModeProd, nil
	case "agent", "agentic":
		return sourcecap.ModeAgentic, nil
	default:
		return "", fmt.Errorf("unsupported mode %q: supported modes are dev, prod, agentic", mode)
	}
}

func isAgenticMode(c *Config) bool {
	if c == nil {
		return false
	}
	mode, err := CanonicalMode(c.Mode)
	return err == nil && mode == sourcecap.ModeAgentic
}

// NormalizeMode makes mode the single deployment-mode selector. When mode is
// omitted, existing production:true configs continue to imply prod mode.
func (c *Config) NormalizeMode() error {
	if c == nil {
		return nil
	}
	mode, err := CanonicalMode(c.Mode)
	if err != nil {
		return err
	}
	if mode == "" {
		switch {
		case c.Production:
			mode = sourcecap.ModeProd
		case c.IsSourcesUsed():
			// Fail closed (security, audit F1): in source mode an unspecified
			// deployment mode must NOT silently fall back to dev. Dev makes every
			// gj_* system root public and mounts the agentic surface; defaulting
			// to it on a missing `mode` is fail-open. Require an explicit
			// dev/agentic selection (GO_ENV, dev.yml/agentic.yml, or `mode:`);
			// anything ambiguous resolves to the locked-down prod posture.
			mode = sourcecap.ModeProd
		default:
			// Legacy (non-source) configs keep the long-standing dev default so
			// existing local development without `sources:` is unaffected.
			mode = sourcecap.ModeDev
		}
	}
	c.Mode = mode
	return nil
}

// ValidateDBType checks if the given database type is supported
func ValidateDBType(dbType string) error {
	if dbType == "" {
		return nil // Empty defaults to postgres, which is valid
	}
	for _, t := range SupportedDBTypes {
		if strings.EqualFold(dbType, t) {
			return nil
		}
	}
	return fmt.Errorf("unsupported database type %q: supported types are %s",
		dbType, strings.Join(SupportedDBTypes, ", "))
}

// ValidateMultiDBType checks if the given database type is supported for multi-database mode
func ValidateMultiDBType(dbType string) error {
	if dbType == "" {
		return nil // Empty defaults to postgres, which is valid
	}
	for _, t := range SupportedMultiDBTypes {
		if strings.EqualFold(dbType, t) {
			return nil
		}
	}
	return fmt.Errorf("unsupported database type %q: supported types are %s",
		dbType, strings.Join(SupportedMultiDBTypes, ", "))
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if err := c.NormalizeMode(); err != nil {
		return err
	}
	if !c.sourcesNormalized {
		if err := c.ValidateIsSourcesUsed(); err != nil {
			return err
		}
	}

	// Validate primary database type
	if err := ValidateDBType(c.DBType); err != nil {
		return err
	}

	// Validate multi-database types
	for name, dbConf := range c.Databases {
		if err := ValidateMultiDBType(dbConf.Type); err != nil {
			return fmt.Errorf("database %q: %w", name, err)
		}
	}

	// Validate partition configs
	for _, t := range c.Tables {
		if t.Partition != nil {
			if t.Partition.None {
				if t.Partition.Column != "" {
					return fmt.Errorf("table %q: partition.none and partition.column are mutually exclusive", t.Name)
				}
				if t.Partition.DefaultRangeDays != 0 {
					return fmt.Errorf("table %q: partition.none cannot be combined with default_range_days", t.Name)
				}
			} else {
				if t.Partition.Column == "" {
					return fmt.Errorf("table %q: partition column must not be empty", t.Name)
				}
				if t.Partition.DefaultRangeDays < 0 {
					return fmt.Errorf("table %q: partition default_range_days must not be negative", t.Name)
				}
			}
		}
	}

	return nil
}

// ValidateIsSourcesUsed checks the public config mode boundary before any
// source entries are normalized into legacy runtime fields.
func (c *Config) ValidateIsSourcesUsed() error {
	if c == nil {
		return nil
	}
	if err := c.validateFeatureConfig(); err != nil {
		return err
	}
	if c.IsSourcesUsed() {
		return c.validateIsSourcesUsed()
	}
	return c.validateLegacyMode()
}

func (c *Config) validateFeatureConfig() error {
	for key := range c.System.Capabilities {
		if _, ok := featurecap.Lookup(featurecap.KindSystem, key); !ok {
			return fmt.Errorf("system.capabilities.%s: unsupported capability (supported: %s)", key, featurecap.ValidKeyList(featurecap.KindSystem))
		}
	}
	for key := range c.Workflows.Capabilities {
		if _, ok := featurecap.Lookup(featurecap.KindWorkflows, key); !ok {
			return fmt.Errorf("workflows.capabilities.%s: unsupported capability (supported: %s)", key, featurecap.ValidKeyList(featurecap.KindWorkflows))
		}
	}
	for root, mode := range c.System.RootAccess {
		if !featurecap.ValidSystemRoot(root) {
			return fmt.Errorf("system.root_access.%s: unsupported system root (supported: %s)", root, strings.Join(featurecap.SystemRoots(), ", "))
		}
		if !validReadAccessMode(mode) {
			return fmt.Errorf("system.root_access.%s: unsupported access mode %q", root, mode)
		}
	}
	return nil
}

func (c *Config) validateLegacyMode() error {
	for name, dbConf := range c.Databases {
		if strings.EqualFold(strings.TrimSpace(dbConf.Type), "codesql") {
			return fmt.Errorf("database %q uses type codesql; move CodeSQL configuration to sources with kind: code", name)
		}
	}
	if len(c.Filesystems) != 0 {
		return fmt.Errorf("top-level filesystems is no longer supported; move file providers to sources with kind: file")
	}
	if strings.TrimSpace(c.OpenAPISpecsDir) != "" || len(c.OpenAPI) != 0 {
		return fmt.Errorf("top-level openapi/openapi_specs_dir is no longer supported; move API providers to sources with kind: api")
	}
	if c.metadataConfigured() {
		return fmt.Errorf("top-level metadata is no longer supported; use system.capabilities.catalog.read")
	}
	if c.catalogConfigured() {
		return fmt.Errorf("top-level catalog is no longer supported; use system.capabilities.catalog.read")
	}
	return nil
}

func (c *Config) validateIsSourcesUsed() error {
	identityQuery := strings.TrimSpace(c.Identity.Query)
	rolesQuery := strings.TrimSpace(c.RolesQuery)
	if identityQuery != "" && rolesQuery != "" && identityQuery != rolesQuery {
		return fmt.Errorf("identity.query and roles_query are aliases in source mode; configure only identity.query or keep both values identical")
	}
	if len(c.Databases) != 0 && !c.sourcesNormalized {
		return fmt.Errorf("databases is legacy database-only config; move SQL/CodeSQL providers to sources")
	}
	if len(c.Filesystems) != 0 && !c.sourcesNormalized {
		return fmt.Errorf("top-level filesystems is legacy config; move file providers to sources")
	}
	if (strings.TrimSpace(c.OpenAPISpecsDir) != "" || len(c.OpenAPI) != 0) && !c.sourcesNormalized {
		return fmt.Errorf("top-level openapi/openapi_specs_dir is legacy config; move API providers to sources")
	}
	if c.metadataConfigured() {
		return fmt.Errorf("top-level metadata is legacy config; configure GraphJin metadata through sources")
	}
	if c.catalogConfigured() {
		return fmt.Errorf("top-level catalog is legacy config; configure GraphJin catalog through sources")
	}

	seen := make(map[string]struct{}, len(c.Sources))
	seenFolded := make(map[string]string, len(c.Sources))
	for i, source := range c.Sources {
		name := strings.TrimSpace(source.Name)
		if name == "" {
			return fmt.Errorf("sources[%d]: name is required", i)
		}
		folded := strings.ToLower(name)
		if existing, ok := seenFolded[folded]; ok {
			return fmt.Errorf("sources: duplicate source names %q and %q differ only by case", existing, name)
		}
		seen[name] = struct{}{}
		seenFolded[folded] = name
		kind, err := sourcecap.CanonicalKind(source.Kind)
		if err != nil {
			return fmt.Errorf("sources[%q]: %w", name, err)
		}
		for key := range source.Capabilities {
			if _, ok := sourcecap.Lookup(kind, key); !ok {
				return fmt.Errorf("sources[%q].capabilities.%s: unsupported capability for kind %q (supported: %s)",
					name, key, kind, sourcecap.ValidKeyList(kind))
			}
		}
		if err := validateSourceAccessConfig(name, kind, source.Access); err != nil {
			return err
		}
	}
	for _, table := range c.Tables {
		if table.Generated {
			continue
		}
		if strings.TrimSpace(table.Source) == "" {
			return fmt.Errorf("tables[%q]: source is required when sources is configured", table.Name)
		}
		if table.Database != "" && !c.sourcesNormalized {
			return fmt.Errorf("tables[%q]: database is legacy config; use source instead", table.Name)
		}
		if _, ok := seen[table.Source]; !ok {
			return fmt.Errorf("tables[%q]: unknown source %q", table.Name, table.Source)
		}
	}
	for _, role := range c.Roles {
		for _, table := range role.Tables {
			if !table.Generated {
				return fmt.Errorf("roles[%q].tables is legacy table access config and is not supported when sources is configured; use sources[].access; next_action: query_catalog(search: \"migrate legacy roles tables to source access\")", role.Name)
			}
		}
	}
	if err := c.validateArtifactsConfig(); err != nil {
		return err
	}
	if err := c.validateWatchesConfig(); err != nil {
		return err
	}
	if err := c.validateTasksConfig(); err != nil {
		return err
	}
	return nil
}

func CanonicalSourceKind(kind string) (string, error) {
	return sourcecap.CanonicalKind(kind)
}

func (c *Config) metadataConfigured() bool {
	return c.Metadata.Enabled != nil ||
		strings.TrimSpace(c.Metadata.Database) != "" ||
		c.Metadata.AutoCodeRelations != nil ||
		len(c.Metadata.CodeDatabases) != 0
}

func (c *Config) catalogConfigured() bool {
	return c.Catalog.Enabled != nil ||
		strings.TrimSpace(c.Catalog.Database) != "" ||
		strings.TrimSpace(c.Catalog.Samples) != "" ||
		c.Catalog.AutoCodeRelations != nil ||
		len(c.Catalog.CodeDatabases) != 0
}

// clone returns a shallow copy of Config with deep copies of the mutable
// slices and maps (Tables, Databases, Roles, etc.) so that engine init
// never mutates the caller's original Config.
func (c *Config) clone() *Config {
	out := *c // shallow copy all scalar/string fields

	if c.Tables != nil {
		out.Tables = make([]Table, len(c.Tables))
		copy(out.Tables, c.Tables)
	}

	if c.Sources != nil {
		out.Sources = make([]SourceConfig, len(c.Sources))
		copy(out.Sources, c.Sources)
		for i := range out.Sources {
			out.Sources[i].Access = c.Sources[i].Access.clone()
			if c.Sources[i].Capabilities != nil {
				out.Sources[i].Capabilities = make(map[string]bool, len(c.Sources[i].Capabilities))
				for k, v := range c.Sources[i].Capabilities {
					out.Sources[i].Capabilities[k] = v
				}
			}
			if c.Sources[i].Specs != nil {
				out.Sources[i].Specs = make(map[string]openapi.SpecConfig, len(c.Sources[i].Specs))
				for k, v := range c.Sources[i].Specs {
					out.Sources[i].Specs[k] = v
				}
			}
		}
	}

	if c.Relationships != nil {
		out.Relationships = make([]RelationshipConfig, len(c.Relationships))
		copy(out.Relationships, c.Relationships)
	}

	if c.Databases != nil {
		out.Databases = make(map[string]DatabaseConfig, len(c.Databases))
		for k, v := range c.Databases {
			out.Databases[k] = v
		}
	}

	if c.Roles != nil {
		out.Roles = make([]Role, len(c.Roles))
		copy(out.Roles, c.Roles)
		for i, r := range c.Roles {
			if r.Tables != nil {
				out.Roles[i].Tables = make([]RoleTable, len(r.Tables))
				copy(out.Roles[i].Tables, r.Tables)
			}
		}
	}

	out.Identity = c.Identity.clone()
	out.Artifacts = c.Artifacts.clone()
	out.Watches = c.Watches.clone()
	out.Tasks = c.Tasks.clone()
	out.System = c.System.clone()
	out.Workflows = c.Workflows.clone()

	return &out
}

// IsSourcesUsed returns true when the public config is using the new sources
// boundary. Without sources, GraphJin stays in legacy database-only mode.
func (c *Config) IsSourcesUsed() bool {
	return c != nil && c.Sources != nil
}

const (
	AccessModeBlocked       = "blocked"
	AccessModePublic        = "public"
	AccessModeAuthenticated = "authenticated"
	AccessModeAccount       = "account"
	AccessModeOwner         = "owner"
	AccessModeAdmin         = "admin"

	MissingNamespaceBlock = "block"
	MissingNamespaceAllow = "allow"
)

func normalizeAccessMode(mode string) string {
	return strings.ToLower(strings.TrimSpace(mode))
}

func validReadAccessMode(mode string) bool {
	switch normalizeAccessMode(mode) {
	case "", AccessModeBlocked, AccessModePublic, AccessModeAuthenticated, AccessModeAccount, AccessModeOwner, AccessModeAdmin:
		return true
	default:
		return false
	}
}

func validWriteAccessMode(mode string) bool {
	switch normalizeAccessMode(mode) {
	case "", AccessModeBlocked, AccessModeAuthenticated, AccessModeAccount, AccessModeOwner, AccessModeAdmin:
		return true
	default:
		return false
	}
}

func validMissingNamespaceMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", MissingNamespaceBlock, MissingNamespaceAllow:
		return true
	default:
		return false
	}
}

func validateSourceAccessConfig(source, kind string, access SourceAccessConfig) error {
	if !validReadAccessMode(access.Read) {
		return fmt.Errorf("sources[%q].access.read: unsupported access mode %q", source, access.Read)
	}
	if !validWriteAccessMode(access.Write) {
		if normalizeAccessMode(access.Write) == AccessModePublic {
			return fmt.Errorf("sources[%q].access.write: public write is not supported", source)
		}
		return fmt.Errorf("sources[%q].access.write: unsupported access mode %q", source, access.Write)
	}
	if !validWriteAccessMode(access.Delete) {
		if normalizeAccessMode(access.Delete) == AccessModePublic {
			return fmt.Errorf("sources[%q].access.delete: public delete is not supported", source)
		}
		return fmt.Errorf("sources[%q].access.delete: unsupported access mode %q", source, access.Delete)
	}
	if !validMissingNamespaceMode(access.MissingNamespaceColumn) {
		return fmt.Errorf("sources[%q].access.missing_namespace_column: unsupported behavior %q", source, access.MissingNamespaceColumn)
	}
	return nil
}

func (c *Config) validateArtifactsConfig() error {
	if c == nil || !c.Artifacts.Enabled {
		return nil
	}
	for _, kind := range c.Artifacts.Locked {
		if strings.TrimSpace(kind) == "" {
			return fmt.Errorf("artifacts.locked: kind is required")
		}
	}
	if c.Artifacts.PollSeconds < 0 {
		return fmt.Errorf("artifacts.poll_seconds must be greater than or equal to 0")
	}
	if c.Artifacts.MaxPerOwner < 0 {
		return fmt.Errorf("artifacts.max_per_owner must be greater than or equal to 0")
	}
	sourceName := strings.TrimSpace(c.Artifacts.Source)
	if sourceName == "" {
		for _, source := range c.Sources {
			if source.CanonicalKind() == sourcecap.KindDatabase {
				sourceName = source.Name
				break
			}
		}
	}
	if sourceName == "" {
		return fmt.Errorf("artifacts.source is required when no database source is configured")
	}
	source, ok := c.SourceByName(sourceName)
	var sourceType string
	var readOnly bool
	if ok {
		if source.CanonicalKind() != sourcecap.KindDatabase {
			return fmt.Errorf("artifacts.source %q must be a writable SQL database source", sourceName)
		}
		sourceType = source.Type
		readOnly = source.ReadOnly
	} else {
		// Source normalization may be followed by service-owned runtime database
		// injection. Those private databases deliberately do not appear in the
		// public sources list, but remain valid artifact stores when present in
		// the normalized runtime database map.
		db, runtime := c.Databases[sourceName]
		if !c.sourcesNormalized || !runtime {
			return fmt.Errorf("artifacts.source %q is not configured", sourceName)
		}
		sourceType = db.Type
		readOnly = db.ReadOnly
	}
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "mongodb", "nanodb", "cassandra", "clickhouse":
		return fmt.Errorf("artifacts.source %q must be a writable SQL database source", sourceName)
	}
	if readOnly {
		return fmt.Errorf("artifacts.source %q is read-only", sourceName)
	}
	return nil
}

func (c *Config) validateWatchesConfig() error {
	if c == nil || !c.Watches.Enabled {
		return nil
	}
	if !c.Artifacts.Enabled {
		return fmt.Errorf("watches require artifacts.enabled")
	}
	if c.Watches.MaxPerOwner < 0 {
		return fmt.Errorf("watches.max_per_owner must be greater than or equal to 0")
	}
	if c.Watches.EventRetentionHours < 0 {
		return fmt.Errorf("watches.event_retention_hours must be greater than or equal to 0")
	}
	if c.Watches.MaxEventsPerWatch < 0 {
		return fmt.Errorf("watches.max_events_per_watch must be greater than or equal to 0")
	}
	if c.Watches.SnapshotMaxBytes < 0 {
		return fmt.Errorf("watches.snapshot_max_bytes must be greater than or equal to 0")
	}
	if c.Watches.EnrichmentDailyCap < 0 {
		return fmt.Errorf("watches.enrichment_daily_cap must be greater than or equal to 0")
	}
	if c.Watches.EnrichmentWorkers < 0 {
		return fmt.Errorf("watches.enrichment_workers must be greater than or equal to 0")
	}
	switch strings.ToLower(strings.TrimSpace(c.Watches.Runner)) {
	case "", "all", "off":
		return nil
	default:
		return fmt.Errorf("watches.runner must be all or off")
	}
}

func (c *Config) validateTasksConfig() error {
	if c == nil || !c.Tasks.Enabled {
		return nil
	}
	if !c.Artifacts.Enabled {
		return fmt.Errorf("tasks require artifacts.enabled")
	}
	if c.Tasks.MaxPerOwner < 0 {
		return fmt.Errorf("tasks.max_per_owner must be greater than or equal to 0")
	}
	if c.Tasks.MaxEntriesPerTask < 0 {
		return fmt.Errorf("tasks.max_entries_per_task must be greater than or equal to 0")
	}
	if c.Tasks.EntryRetentionHours < 0 {
		return fmt.Errorf("tasks.entry_retention_hours must be greater than or equal to 0")
	}
	if c.Tasks.SnapshotMaxBytes < 0 {
		return fmt.Errorf("tasks.snapshot_max_bytes must be greater than or equal to 0")
	}
	return nil
}

func (c *Config) normalizeIdentityDefaults() {
	if c == nil || !c.IsSourcesUsed() {
		return
	}
	if strings.TrimSpace(c.Identity.UserIDClaim) == "" {
		c.Identity.UserIDClaim = "sub"
	}
	if len(c.Identity.RoleClaims) == 0 {
		c.Identity.RoleClaims = []string{"role", "roles"}
	}
	if strings.TrimSpace(c.Identity.NamespaceClaim) == "" {
		c.Identity.NamespaceClaim = "account_id"
	}
	if len(c.Identity.AdminRoles) == 0 {
		c.Identity.AdminRoles = []string{"admin"}
	}
	if strings.TrimSpace(c.Identity.Query) == "" && strings.TrimSpace(c.RolesQuery) != "" {
		c.Identity.Query = c.RolesQuery
	}
	if strings.TrimSpace(c.RolesQuery) == "" && strings.TrimSpace(c.Identity.Query) != "" {
		c.RolesQuery = c.Identity.Query
	}
}

func (c *Config) normalizeArtifactsDefaults() {
	if c == nil || !c.Artifacts.Enabled {
		return
	}
	if strings.TrimSpace(c.Artifacts.Source) == "" {
		for _, source := range c.Sources {
			if source.CanonicalKind() == sourcecap.KindDatabase {
				c.Artifacts.Source = source.Name
				break
			}
		}
	}
	if strings.TrimSpace(c.Artifacts.Schema) == "" {
		c.Artifacts.Schema = "_graphjin"
	}
	if strings.TrimSpace(c.Artifacts.GlobalsPath) == "" {
		c.Artifacts.GlobalsPath = "./config"
	}
	if c.Artifacts.AutoInit == nil {
		c.Artifacts.AutoInit = boolPtr(true)
	}
	if c.Artifacts.PollSeconds == 0 {
		c.Artifacts.PollSeconds = 15
	}
	if c.Artifacts.ProjectionContentMaxBytes <= 0 {
		c.Artifacts.ProjectionContentMaxBytes = 32 * 1024
	}
	if c.Artifacts.MaxPerOwner == 0 {
		c.Artifacts.MaxPerOwner = 200
	}
}

func (c *Config) normalizeWatchesDefaults() {
	if c == nil || !c.Watches.Enabled {
		return
	}
	if c.Watches.MaxPerOwner == 0 {
		c.Watches.MaxPerOwner = 20
	}
	if c.Watches.EventRetentionHours == 0 {
		c.Watches.EventRetentionHours = 168
	}
	if c.Watches.MaxEventsPerWatch == 0 {
		c.Watches.MaxEventsPerWatch = 500
	}
	if c.Watches.SnapshotMaxBytes == 0 {
		c.Watches.SnapshotMaxBytes = 32768
	}
	if c.Watches.EnrichmentDailyCap == 0 {
		c.Watches.EnrichmentDailyCap = 10
	}
	if c.Watches.EnrichmentWorkers == 0 {
		c.Watches.EnrichmentWorkers = 1
	}
	if c.Watches.RetryMaxFailures == 0 {
		c.Watches.RetryMaxFailures = 5
	}
	if strings.TrimSpace(c.Watches.Runner) == "" {
		c.Watches.Runner = "off"
	} else {
		c.Watches.Runner = strings.ToLower(strings.TrimSpace(c.Watches.Runner))
	}
}

func (c *Config) normalizeTasksDefaults() {
	if c == nil || !c.Tasks.Enabled {
		return
	}
	if c.Tasks.MaxPerOwner == 0 {
		c.Tasks.MaxPerOwner = 20
	}
	if c.Tasks.MaxEntriesPerTask == 0 {
		c.Tasks.MaxEntriesPerTask = 500
	}
	if c.Tasks.EntryRetentionHours == 0 {
		c.Tasks.EntryRetentionHours = 168
	}
	if c.Tasks.SnapshotMaxBytes == 0 {
		c.Tasks.SnapshotMaxBytes = 32768
	}
}

func (c *Config) normalizeSourceAccessDefaults() {
	if c == nil || !c.IsSourcesUsed() {
		return
	}
	for i := range c.Sources {
		kind := c.Sources[i].CanonicalKind()
		switch kind {
		case sourcecap.KindDatabase:
			c.Sources[i].Access = effectiveDatabaseAccess(c.Sources[i].Access)
		}
	}
}

func effectiveDatabaseAccess(access SourceAccessConfig) SourceAccessConfig {
	if strings.TrimSpace(access.Read) == "" {
		access.Read = AccessModeAccount
	} else {
		access.Read = normalizeAccessMode(access.Read)
	}
	if strings.TrimSpace(access.Write) == "" {
		access.Write = AccessModeBlocked
	} else {
		access.Write = normalizeAccessMode(access.Write)
	}
	if strings.TrimSpace(access.Delete) == "" {
		access.Delete = AccessModeBlocked
	} else {
		access.Delete = normalizeAccessMode(access.Delete)
	}
	if strings.TrimSpace(access.NamespaceColumn) == "" {
		access.NamespaceColumn = "account_id"
	}
	if strings.TrimSpace(access.OwnerColumn) == "" {
		access.OwnerColumn = "user_id"
	}
	if strings.TrimSpace(access.MissingNamespaceColumn) == "" {
		access.MissingNamespaceColumn = MissingNamespaceBlock
	} else {
		access.MissingNamespaceColumn = strings.ToLower(strings.TrimSpace(access.MissingNamespaceColumn))
	}
	return access
}

func (c *Config) modeForSourceDefaults() string {
	if c == nil {
		return sourcecap.ModeDev
	}
	mode, err := CanonicalMode(c.Mode)
	if err == nil && mode != "" {
		return mode
	}
	if c.Production {
		return sourcecap.ModeProd
	}
	return sourcecap.ModeDev
}

// EffectiveSystemRootAccess sets the per-mode default access for the gj_* system
// roots. Modes set safe defaults; explicit system.root_access entries always win.
//
// Row-level owner scoping for the artifact-backed roots (gj_artifacts, gj_watch,
// gj_watch_event, gj_task, gj_task_entry, gj_workflow, gj_workflow_execution) is enforced by the artifact
// control-plane handler
// (owner_id = user_id, with the raw artifact tables blocked from generic GraphQL),
// so the "owner" mode here is the visibility gate that pairs with that handler
// scoping — it does not by itself add SQL row filters.
func (c *Config) EffectiveSystemRootAccess() map[string]string {
	access := make(map[string]string, len(c.System.RootAccess)+12)
	for root, mode := range c.System.RootAccess {
		access[strings.ToLower(strings.TrimSpace(root))] = normalizeAccessMode(mode)
	}
	var defaults map[string]string
	switch strings.ToLower(strings.TrimSpace(c.modeForSourceDefaults())) {
	case sourcecap.ModeDev:
		// Local development of the new stack: the whole surface is open.
		defaults = map[string]string{
			"gj_catalog":            AccessModePublic,
			"gj_artifacts":          AccessModePublic,
			"gj_watch":              AccessModePublic,
			"gj_watch_event":        AccessModePublic,
			"gj_task":               AccessModePublic,
			"gj_task_entry":         AccessModePublic,
			"gj_workflow":           AccessModePublic,
			"gj_workflow_execution": AccessModePublic,
			"gj_runtime":            AccessModePublic,
			"gj_security":           AccessModePublic,
			"gj_config":             AccessModePublic,
		}
	case sourcecap.ModeAgentic:
		// Trusted-agent matrix: discovery open (read-only), personal
		// artifacts/workflows owner-scoped, control plane admin-only.
		defaults = map[string]string{
			"gj_catalog":            AccessModePublic,
			"gj_artifacts":          AccessModeOwner,
			"gj_watch":              AccessModeOwner,
			"gj_watch_event":        AccessModeOwner,
			"gj_task":               AccessModeOwner,
			"gj_task_entry":         AccessModeOwner,
			"gj_workflow":           AccessModeOwner,
			"gj_workflow_execution": AccessModeOwner,
			"gj_runtime":            AccessModeAdmin,
			"gj_security":           AccessModeAdmin,
			"gj_config":             AccessModeAdmin,
		}
	default:
		// prod is the only pre-agentic compatibility mode: the agentic surface is
		// hard-gated off entirely (see serv agenticSurfaceEnabled). These admin
		// defaults are defense-in-depth for any root that would otherwise be
		// reachable.
		defaults = map[string]string{
			"gj_catalog":            AccessModeAdmin,
			"gj_artifacts":          AccessModeAdmin,
			"gj_watch":              AccessModeAdmin,
			"gj_watch_event":        AccessModeAdmin,
			"gj_task":               AccessModeAdmin,
			"gj_task_entry":         AccessModeAdmin,
			"gj_workflow":           AccessModeAdmin,
			"gj_workflow_execution": AccessModeAdmin,
			"gj_runtime":            AccessModeAdmin,
			"gj_security":           AccessModeAdmin,
			"gj_config":             AccessModeAdmin,
		}
	}
	for root, m := range defaults {
		if strings.TrimSpace(access[root]) == "" {
			access[root] = m
		}
	}
	return access
}

func (c IdentityConfig) clone() IdentityConfig {
	out := c
	if c.RoleClaims != nil {
		out.RoleClaims = append([]string(nil), c.RoleClaims...)
	}
	if c.AdminRoles != nil {
		out.AdminRoles = append([]string(nil), c.AdminRoles...)
	}
	return out
}

func (c ArtifactsConfig) clone() ArtifactsConfig {
	out := c
	if c.AutoInit != nil {
		v := *c.AutoInit
		out.AutoInit = &v
	}
	if c.Locked != nil {
		out.Locked = append([]string(nil), c.Locked...)
	}
	return out
}

func (c WatchesConfig) clone() WatchesConfig {
	out := c
	if c.WebhookAllow != nil {
		out.WebhookAllow = append([]string(nil), c.WebhookAllow...)
	}
	return out
}

func (c TasksConfig) clone() TasksConfig { return c }

func (c ArtifactsConfig) AutoInitEnabled() bool {
	return c.AutoInit == nil || *c.AutoInit
}

func boolPtr(v bool) *bool {
	return &v
}

func (c SourceAccessConfig) clone() SourceAccessConfig {
	out := c
	if c.PublicTables != nil {
		out.PublicTables = append([]string(nil), c.PublicTables...)
	}
	if c.AdminTables != nil {
		out.AdminTables = append([]string(nil), c.AdminTables...)
	}
	if c.BlockedTables != nil {
		out.BlockedTables = append([]string(nil), c.BlockedTables...)
	}
	return out
}

// FeatureCapability returns a system/workflow capability after applying the
// deployment-mode default. Explicit top-level feature configuration wins.
func (c *Config) FeatureCapability(kind, key string) bool {
	if c == nil {
		return false
	}
	if value, explicit := c.FeatureCapabilityConfigured(kind, key); explicit {
		return value
	}
	def, ok := featurecap.Lookup(kind, key)
	return ok && def.Default(c.modeForSourceDefaults())
}

// FeatureCapabilityConfigured reports an explicit top-level feature override.
func (c *Config) FeatureCapabilityConfigured(kind, key string) (bool, bool) {
	if c == nil {
		return false, false
	}
	var capabilities map[string]bool
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case featurecap.KindSystem:
		capabilities = c.System.Capabilities
	case featurecap.KindWorkflows:
		capabilities = c.Workflows.Capabilities
	default:
		return false, false
	}
	value, ok := capabilities[key]
	return value, ok
}

// EffectiveIdentityConfig returns identity config with source-mode defaults.
func (c *Config) EffectiveIdentityConfig() IdentityConfig {
	if c == nil {
		return IdentityConfig{UserIDClaim: "sub", RoleClaims: []string{"role", "roles"}, NamespaceClaim: "account_id", AdminRoles: []string{"admin"}}
	}
	out := c.Identity.clone()
	tmp := &Config{Identity: out, RolesQuery: c.RolesQuery, Sources: c.Sources}
	tmp.normalizeIdentityDefaults()
	return tmp.Identity
}

// EffectiveArtifactsConfig returns artifact config with source-mode defaults.
func (c *Config) EffectiveArtifactsConfig() ArtifactsConfig {
	if c == nil {
		return ArtifactsConfig{Schema: "_graphjin", GlobalsPath: "./config", PollSeconds: 15, ProjectionContentMaxBytes: 32 * 1024, MaxPerOwner: 200}
	}
	out := c.Artifacts.clone()
	tmp := &Config{Artifacts: out, Sources: c.Sources}
	tmp.normalizeArtifactsDefaults()
	return tmp.Artifacts
}

// EffectiveWatchesConfig returns watch config with source-mode defaults.
func (c *Config) EffectiveWatchesConfig() WatchesConfig {
	if c == nil {
		return WatchesConfig{MaxPerOwner: 20, EventRetentionHours: 168, MaxEventsPerWatch: 500, SnapshotMaxBytes: 32768, Runner: "off", EnrichmentDailyCap: 10, EnrichmentWorkers: 1, RetryMaxFailures: 5}
	}
	out := c.Watches.clone()
	tmp := &Config{Watches: out}
	tmp.normalizeWatchesDefaults()
	return tmp.Watches
}

// EffectiveTasksConfig returns task config with source-mode defaults.
func (c *Config) EffectiveTasksConfig() TasksConfig {
	if c == nil {
		return TasksConfig{MaxPerOwner: 20, MaxEntriesPerTask: 500, EntryRetentionHours: 168, SnapshotMaxBytes: 32768}
	}
	out := c.Tasks.clone()
	tmp := &Config{Tasks: out}
	tmp.normalizeTasksDefaults()
	return tmp.Tasks
}

// EffectiveSourceAccess returns a source's access config with source-kind defaults.
func (c *Config) EffectiveSourceAccess(source SourceConfig) SourceAccessConfig {
	switch source.CanonicalKind() {
	case sourcecap.KindDatabase:
		return effectiveDatabaseAccess(source.Access.clone())
	default:
		return source.Access.clone()
	}
}

// NormalizeSources translates public sources into the existing runtime config
// fields consumed by the engine and service. It is intentionally idempotent.
func (c *Config) NormalizeSources() error {
	if c == nil || !c.IsSourcesUsed() || c.sourcesNormalized {
		return nil
	}
	if err := c.ValidateIsSourcesUsed(); err != nil {
		return err
	}
	c.normalizeIdentityDefaults()
	c.normalizeArtifactsDefaults()
	c.normalizeWatchesDefaults()
	c.normalizeTasksDefaults()
	c.normalizeSourceAccessDefaults()

	sqlSources := make([]string, 0, len(c.Sources))
	c.Databases = make(map[string]DatabaseConfig)
	c.Filesystems = nil
	c.OpenAPISpecsDir = ""
	c.OpenAPI = nil

	for _, source := range c.Sources {
		name := strings.TrimSpace(source.Name)
		kind, err := CanonicalSourceKind(source.Kind)
		if err != nil {
			return fmt.Errorf("sources[%q]: %w", name, err)
		}
		switch kind {
		case "database":
			dbConf := source.databaseConfig()
			if dbConf.Type == "" {
				dbConf.Type = "postgres"
			}
			if strings.EqualFold(dbConf.Type, "codesql") {
				return fmt.Errorf("sources[%q]: use kind: code instead of kind: database with type: codesql", name)
			}
			c.Databases[name] = dbConf
			sqlSources = append(sqlSources, name)
		case "code":
			dbConf := source.databaseConfig()
			dbConf.Type = "codesql"
			c.Databases[name] = dbConf
			sqlSources = append(sqlSources, name)
		case "file":
			c.Filesystems = append(c.Filesystems, source.filesystemConfig())
		case "api":
			if source.SpecsDir != "" {
				if c.OpenAPISpecsDir != "" && c.OpenAPISpecsDir != source.SpecsDir {
					return fmt.Errorf("sources[%q]: multiple openapi specs_dir values are not supported", name)
				}
				c.OpenAPISpecsDir = source.SpecsDir
			}
			if len(source.Specs) != 0 {
				if c.OpenAPI == nil {
					c.OpenAPI = make(map[string]openapi.SpecConfig, len(source.Specs))
				}
				for key, spec := range source.Specs {
					if _, exists := c.OpenAPI[key]; exists {
						return fmt.Errorf("sources[%q]: duplicate openapi spec config %q", name, key)
					}
					c.OpenAPI[key] = spec
				}
			}
		}
	}

	defaultDB := c.defaultSQLSource(sqlSources)
	for i := range c.Tables {
		source, _ := c.SourceByName(c.Tables[i].Source)
		if source.ReadOnly {
			c.Tables[i].ReadOnly = true
		}
		switch source.CanonicalKind() {
		case "database", "code":
			c.Tables[i].Database = source.Name
		default:
			c.Tables[i].Database = defaultDB
		}
	}
	if err := c.applyRelationshipOverlays(); err != nil {
		return err
	}

	c.sourcesNormalized = true
	return nil
}

// RenormalizeSources re-runs source normalization after a runtime update has
// replaced the public sources list on a config that was already normalized.
func (c *Config) RenormalizeSources() error {
	if c == nil {
		return nil
	}
	c.sourcesNormalized = false
	if c.IsSourcesUsed() {
		c.Databases = nil
		c.Filesystems = nil
		c.OpenAPISpecsDir = ""
		c.OpenAPI = nil
		for i := range c.Tables {
			c.Tables[i].Database = ""
		}
	}
	return c.NormalizeSources()
}

func (c *Config) defaultSQLSource(sqlSources []string) string {
	for _, source := range c.Sources {
		if source.Default {
			kind, _ := CanonicalSourceKind(source.Kind)
			if kind == "database" || kind == "code" {
				return source.Name
			}
		}
	}
	if len(sqlSources) != 0 {
		sort.Strings(sqlSources)
		return sqlSources[0]
	}
	return DefaultDBName
}

// defaultDatabaseName chooses an application source before any runtime-only
// database injected by the service (for example the system NanoDB or managed
// artifact store). Runtime identifiers deliberately do not appear in Sources.
func (c *Config) defaultDatabaseName() string {
	if c == nil {
		return ""
	}
	var sourceNames []string
	for _, source := range c.Sources {
		kind := source.CanonicalKind()
		if kind != sourcecap.KindDatabase && kind != sourcecap.KindCode {
			continue
		}
		if _, ok := c.Databases[source.Name]; !ok {
			continue
		}
		if source.Default {
			return source.Name
		}
		sourceNames = append(sourceNames, source.Name)
	}
	if len(sourceNames) != 0 {
		sort.Strings(sourceNames)
		return sourceNames[0]
	}
	// Legacy database-only service configurations store the application
	// connection under DefaultDBName. Prefer it over runtime-only databases
	// injected by the service.
	if _, ok := c.Databases[DefaultDBName]; ok {
		return DefaultDBName
	}
	names := make([]string, 0, len(c.Databases))
	for name := range c.Databases {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) != 0 {
		return names[0]
	}
	return ""
}

func (s SourceConfig) databaseConfig() DatabaseConfig {
	return DatabaseConfig{
		Type:                   s.Type,
		ConnString:             s.ConnString,
		Host:                   s.Host,
		Port:                   s.Port,
		DBName:                 s.DBName,
		User:                   s.User,
		Password:               s.Password,
		Path:                   s.Path,
		MaxOpenConns:           s.MaxOpenConns,
		MaxIdleConns:           s.MaxIdleConns,
		Schema:                 s.Schema,
		PoolSize:               s.PoolSize,
		MaxConnections:         s.MaxConnections,
		MaxConnIdleTime:        s.MaxConnIdleTime,
		MaxConnLifeTime:        s.MaxConnLifeTime,
		PingTimeout:            s.PingTimeout,
		EnableTLS:              s.EnableTLS,
		ServerName:             s.ServerName,
		ServerCert:             s.ServerCert,
		ClientCert:             s.ClientCert,
		ClientKey:              s.ClientKey,
		Encrypt:                s.Encrypt,
		TrustServerCertificate: s.TrustServerCertificate,
		PrivateKeyPath:         s.PrivateKeyPath,
		PrivateKeyPEM:          s.PrivateKeyPEM,
		KeyPassphrase:          s.KeyPassphrase,
		ReadOnly:               s.ReadOnly,
		AnalyticsMode:          s.AnalyticsMode,
		InferDBRefs:            s.InferDBRefs,
	}
}

func (s SourceConfig) filesystemConfig() FilesystemConfig {
	return FilesystemConfig{
		Name:            s.Name,
		Backend:         s.Backend,
		Bucket:          s.Bucket,
		Region:          s.Region,
		Endpoint:        s.Endpoint,
		Prefix:          s.Prefix,
		Root:            s.Root,
		PresignTTL:      s.PresignTTL,
		PublicBaseURL:   s.PublicBaseURL,
		ReadOnly:        s.ReadOnly,
		MaxListPageSize: s.MaxListPageSize,
	}
}

func (c *Config) SourceByName(name string) (SourceConfig, bool) {
	if c == nil {
		return SourceConfig{}, false
	}
	for _, source := range c.Sources {
		if source.Name == name {
			return source, true
		}
	}
	return SourceConfig{}, false
}

func (s SourceConfig) CanonicalKind() string {
	kind, _ := CanonicalSourceKind(s.Kind)
	return kind
}

func (s SourceConfig) Capability(key string) (bool, bool) {
	if s.Capabilities == nil {
		return false, false
	}
	value, ok := s.Capabilities[key]
	return value, ok
}

func (c *Config) applyRelationshipOverlays() error {
	for i, rel := range c.Relationships {
		fromTable, fromColumn, err := splitRelationshipEndpoint(rel.From)
		if err != nil {
			return fmt.Errorf("relationships[%d].from: %w", i, err)
		}
		if _, _, err := splitRelationshipEndpoint(rel.To); err != nil {
			return fmt.Errorf("relationships[%d].to: %w", i, err)
		}
		idx := c.findTableIndex(fromTable)
		if idx == -1 {
			return fmt.Errorf("relationships[%d]: from table %q is not configured in tables", i, fromTable)
		}
		table := &c.Tables[idx]
		found := false
		for ci := range table.Columns {
			if table.Columns[ci].Name == fromColumn {
				table.Columns[ci].ForeignKey = rel.To
				found = true
				break
			}
		}
		if !found {
			table.Columns = append(table.Columns, Column{Name: fromColumn, ForeignKey: rel.To})
		}
	}
	return nil
}

func splitRelationshipEndpoint(endpoint string) (string, string, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return "", "", fmt.Errorf("must not be empty")
	}
	idx := strings.LastIndex(endpoint, ".")
	if idx <= 0 || idx == len(endpoint)-1 {
		return "", "", fmt.Errorf("must be table.column, schema.table.column, or source:schema.table.column")
	}
	return endpoint[:idx], endpoint[idx+1:], nil
}

func (c *Config) findTableIndex(path string) int {
	db, schema, table := splitTablePath(path)
	for i := range c.Tables {
		t := c.Tables[i]
		if db != "" && t.Database != db && t.Source != db {
			continue
		}
		if schema != "" && t.Schema != "" && t.Schema != schema {
			continue
		}
		if t.Name == table || t.Table == table {
			return i
		}
	}
	return -1
}

func splitTablePath(path string) (string, string, string) {
	db := ""
	if idx := strings.IndexByte(path, ':'); idx != -1 {
		db = path[:idx]
		path = path[idx+1:]
	}
	parts := strings.Split(path, ".")
	if len(parts) >= 2 {
		return db, strings.Join(parts[:len(parts)-1], "."), parts[len(parts)-1]
	}
	return db, "", path
}

// NormalizeDatabases ensures the primary database is represented as an entry
// in the Databases map, eliminating special-casing of empty targetDB strings.
// It is idempotent and should be called during core initialization.
func (c *Config) NormalizeDatabases() {
	// If no databases configured, create a default entry
	if len(c.Databases) == 0 {
		dbType := c.DBType
		if dbType == "" {
			dbType = "postgres"
		}
		c.Databases = map[string]DatabaseConfig{
			DefaultDBName: {
				Type: dbType,
			},
		}
	}

	// Prefer a configured application source. Service-owned runtime databases
	// are absent from Sources and must never become the primary database merely
	// because their collision-free identifier sorts first.
	defaultName := c.defaultDatabaseName()

	// Sync DBType with the default entry's Type
	defConf := c.Databases[defaultName]
	if defConf.Type == "" && c.DBType != "" {
		defConf.Type = c.DBType
		c.Databases[defaultName] = defConf
	} else if defConf.Type != "" && c.DBType == "" {
		c.DBType = defConf.Type
	}

	// Tag tables that have empty Database with the default name
	for i := range c.Tables {
		if c.Tables[i].Database == "" {
			c.Tables[i].Database = defaultName
		}
	}

	// Propagate database-level ReadOnly to all roles' table entries
	// belonging to that database. This leverages the existing table-level
	// read_only enforcement in the core engine.
	for dbName, dbConf := range c.Databases {
		if !dbConf.ReadOnly {
			continue
		}
		for ri := range c.Roles {
			for ti := range c.Roles[ri].Tables {
				rt := &c.Roles[ri].Tables[ti]
				if rt.Database != "" && rt.Database != dbName {
					continue
				}
				// Match tables that belong to this database
				for _, tbl := range c.Tables {
					if tbl.Database == dbName && strings.EqualFold(tbl.Name, rt.Name) {
						rt.ReadOnly = true
					}
				}
			}
		}
	}

	c.Databases[defaultName] = defConf
}

// Configuration for the GraphJin compiler core
type Config struct {
	// Is used to encrypt opaque values such as the cursor. Auto-generated when not set
	SecretKey string `mapstructure:"secret_key" json:"secret_key" yaml:"secret_key"  jsonschema:"title=Secret Key" jsonschema_extras:"x-graphjin-sensitive=secret"`

	// When set to true it disables the allow list workflow
	DisableAllowList bool `mapstructure:"disable_allow_list" json:"disable_allow_list" yaml:"disable_allow_list" jsonschema:"title=Disable Allow List,default=false"`

	// When set to true a database schema file will be generated in dev mode and
	// used in production mode. Auto database discovery will be disabled
	// in production mode.
	EnableSchema bool `mapstructure:"enable_schema" json:"enable_schema" yaml:"enable_schema" jsonschema:"title=Enable Schema,default=false"`

	// When set to true an introspection json file will be generated in dev mode.
	// This file can be used with other GraphQL tooling to generate clients, enable
	// autocomplete, etc
	EnableIntrospection bool `mapstructure:"enable_introspection" json:"enable_introspection" yaml:"enable_introspection" jsonschema:"title=Generate introspection JSON,default=false"`

	// Forces the database session variable 'user.id' to be set to the user id
	SetUserID bool `mapstructure:"set_user_id" json:"set_user_id" yaml:"set_user_id" jsonschema:"title=Set User ID,default=false"`

	// This ensures that for anonymous users (role 'anon') all tables are blocked
	// from queries and mutations. To open access to tables for anonymous users
	// they have to be added to the 'anon' role config
	DefaultBlock bool `mapstructure:"default_block" json:"default_block" yaml:"default_block" jsonschema:"title=Block tables for anonymous users,default=true"`

	// This is a list of variables that can be leveraged in your queries.
	// (eg. variable admin_id will be $admin_id in the query)
	Vars map[string]string `mapstructure:"variables" json:"variables" yaml:"variables" jsonschema:"title=Variables"`

	// This is a list of variables that map to http header values
	HeaderVars map[string]string `mapstructure:"header_variables" json:"header_variables" yaml:"header_variables" jsonschema:"title=Header Variables"`

	// A list of tables and columns that should disallowed in any and all queries
	Blocklist []string `jsonschema:"title=Block List"`

	// The configs for custom resolvers. For example the `remote_api`
	// resolver would join json from a remote API into your query response
	Resolvers []ResolverConfig `jsonschema:"-"`

	// Sources is the canonical external graph provider config. When present, all
	// SQL, CodeSQL, filesystem, and OpenAPI providers must be declared here.
	Sources []SourceConfig `mapstructure:"sources" json:"sources" yaml:"sources" jsonschema:"title=Sources"`

	// System configures GraphJin-owned roots. It is separate from Sources because
	// built-in functionality is not an external graph provider.
	System SystemConfig `mapstructure:"system" json:"system" yaml:"system" jsonschema:"title=System Features"`

	// Workflows configures the built-in Goja JavaScript workflow feature.
	Workflows WorkflowsConfig `mapstructure:"workflows" json:"workflows" yaml:"workflows" jsonschema:"title=Workflows"`

	// Identity declares the request-wide claims and optional enrichment query
	// used by source-mode access generation.
	Identity IdentityConfig `mapstructure:"identity" json:"identity" yaml:"identity" jsonschema:"title=Identity"`

	// Artifacts configures GraphJin-managed mutable agent artifacts exposed as
	// the gj_artifacts system root.
	Artifacts ArtifactsConfig `mapstructure:"artifacts" json:"artifacts" yaml:"artifacts" jsonschema:"title=Artifacts"`

	// Watches configures GraphJin-managed standing questions and durable inbox
	// events exposed as gj_watch and gj_watch_event system roots.
	Watches WatchesConfig `mapstructure:"watches" json:"watches" yaml:"watches" jsonschema:"title=Watches"`

	// Tasks configures owner-scoped declared goals and their immutable trail.
	Tasks TasksConfig `mapstructure:"tasks" json:"tasks" yaml:"tasks" jsonschema:"title=Tasks"`

	// OpenAPISpecsDir is the directory that GraphJin scans at startup for
	// OpenAPI 3 specification files (*.yaml / *.yml). Each spec dropped in
	// is parsed, classified, and exposed as remote-joinable fields and/or
	// top-level virtual tables. Defaults to "./config/specs" when empty.
	OpenAPISpecsDir string `mapstructure:"openapi_specs_dir" json:"openapi_specs_dir" yaml:"openapi_specs_dir" jsonschema:"title=OpenAPI Specs Directory,default=./config/specs"`

	// OpenAPI carries per-spec configuration keyed by the spec filename
	// without extension (e.g. "interaction_studio" matches
	// "config/specs/interaction_studio.yaml"). Operations classifiable from
	// the spec are auto-exposed; entries here add credentials, base URL
	// overrides, DB-to-API join wiring, and concurrency caps.
	OpenAPI map[string]openapi.SpecConfig `mapstructure:"openapi" json:"openapi" yaml:"openapi" jsonschema:"-"`

	// All table specific configuration such as aliased tables and relationships
	// between tables
	Tables []Table `jsonschema:"title=Tables"`

	// Relationships adds graph edges between typed table columns. Cardinality is
	// inferred from existing primary/unique metadata and reverse edges are still
	// generated by the schema graph.
	Relationships []RelationshipConfig `mapstructure:"relationships" json:"relationships" yaml:"relationships" jsonschema:"title=Relationships"`

	// All function specific configuration such as return types
	Functions []Function `jsonschema:"title=Functions"`

	// An SQL query if set enables attribute based access control. This query is
	// used to fetch the user attribute that then dynamically define the users role
	RolesQuery string `mapstructure:"roles_query" json:"roles_query" yaml:"roles_query" jsonschema:"title=Roles Query"`

	// Roles contains the configuration for all the roles you want to support 'user' and
	// 'anon' are two default roles. The 'user' role is used when a user ID is available
	// and 'anon' when it's not. Use the 'Roles Query' config to add more custom roles
	Roles []Role

	// Database type name Defaults to 'postgres' (options: postgres, mysql, mariadb, sqlite, oracle, mssql, nanodb)
	DBType string `mapstructure:"db_type" json:"db_type" yaml:"db_type" jsonschema:"title=Database Type,enum=postgres,enum=mysql,enum=mariadb,enum=sqlite,enum=oracle,enum=mssql,enum=snowflake,enum=nanodb"`

	// Log warnings and other debug information
	Debug bool `jsonschema:"title=Debug,default=false"`

	// Log SQL Query variable values
	LogVars bool `mapstructure:"log_vars" json:"log_vars" yaml:"log_vars" jsonschema:"title=Log Variables,default=false"`

	// Database polling duration (in seconds) used by subscriptions to
	// query for updates.
	SubsPollDuration time.Duration `mapstructure:"subs_poll_duration" json:"subs_poll_duration" yaml:"subs_poll_duration" jsonschema:"title=Subscription Polling Duration,default=5s"`

	// Upper bound on how many subscribers a single subscription poll cycle
	// will batch into one database query. The adaptive sizer never grows
	// past this value. When zero a sensible default is used.
	SubsMaxMembersPerWorker int `mapstructure:"subs_max_members_per_worker" json:"subs_max_members_per_worker" yaml:"subs_max_members_per_worker" jsonschema:"title=Subscription Max Members Per Worker,default=5000"`

	// Lower bound on the adaptive subscription chunk size; prevents the
	// sizer from shrinking down to per-row queries under sustained load.
	// When zero a sensible default is used.
	SubsMinMembersPerWorker int `mapstructure:"subs_min_members_per_worker" json:"subs_min_members_per_worker" yaml:"subs_min_members_per_worker" jsonschema:"title=Subscription Min Members Per Worker,default=50"`

	// Target query latency for the adaptive subscription chunk sizer.
	// When zero it is auto-derived as one quarter of subs_poll_duration.
	SubsTargetQueryLatency time.Duration `mapstructure:"subs_target_query_latency" json:"subs_target_query_latency" yaml:"subs_target_query_latency" jsonschema:"title=Subscription Target Query Latency,description=When zero this is auto-derived as 25% of subs_poll_duration"`

	// The default max limit (number of rows) when a limit is not defined in
	// the query or the table role config.
	DefaultLimit int `mapstructure:"default_limit" json:"default_limit" yaml:"default_limit" jsonschema:"title=Default Row Limit,default=20"`

	AnalyticsMode bool `mapstructure:"analytics_mode" json:"analytics_mode" yaml:"analytics_mode" jsonschema:"title=Analytics Mode,default=false,description=Disable implicit row-limit defaults for OLAP deployments"`

	// Disable all aggregation functions like count, sum, etc
	DisableAgg bool `mapstructure:"disable_agg_functions" json:"disable_agg_functions" yaml:"disable_agg_functions" jsonschema:"title=Disable Aggregations,default=false"`

	// Disable all functions like count, length,  etc
	DisableFuncs bool `mapstructure:"disable_functions" json:"disable_functions" yaml:"disable_functions" jsonschema:"title=Disable Functions,default=false"`

	// When set to true, GraphJin will not connect to a database and instead
	// return mock data based on the query structure.
	MockDB bool `mapstructure:"mock_db" json:"mock_db" yaml:"mock_db" jsonschema:"title=Mock DB,default=false"`

	// Enable automatic coversion of camel case in GraphQL to snake case in SQL
	EnableCamelcase bool `mapstructure:"enable_camelcase" json:"enable_camelcase" yaml:"enable_camelcase" jsonschema:"title=Enable Camel Case,default=false"`

	// Legacy production switch. Prefer Mode for new configs.
	Production bool `jsonschema:"title=Production Mode,default=false"`

	// Mode selects the deployment-mode defaults. Empty derives from Production.
	Mode string `mapstructure:"mode" json:"mode" yaml:"mode" jsonschema:"title=Mode,enum=dev,enum=prod,enum=agentic"`

	// Duration for polling the database to detect schema changes
	DBSchemaPollDuration time.Duration `mapstructure:"db_schema_poll_duration" json:"db_schema_poll_duration" yaml:"db_schema_poll_duration" jsonschema:"title=Schema Change Detection Polling Duration,default=10s"`

	// When set to true it disables production security features like enforcing the allow list
	DisableProdSecurity bool `mapstructure:"disable_production_security" json:"disable_production_security" yaml:"disable_production_security" jsonschema:"title=Disable Production Security"`

	// The filesystem to use for this instance of GraphJin
	FS interface{} `mapstructure:"-" jsonschema:"-" json:"-"`

	// Multiple database configurations for multi-database support.
	// When set, allows querying across multiple databases in a single GraphQL request.
	// Each database gets its own connection pool, schema, and SQL compiler.
	Databases map[string]DatabaseConfig `mapstructure:"databases" json:"databases" yaml:"databases" jsonschema:"title=Databases"`

	// CacheTrackingEnabled enables injection of __gj_id fields for cache row tracking.
	// This is set by the service layer when Redis caching is enabled.
	CacheTrackingEnabled bool `mapstructure:"-" json:"-" yaml:"-" jsonschema:"-"`

	// Federation opts the engine into Apollo Federation v2 subgraph mode.
	// When Federation.Enabled is true, `_service { sdl }` returns a
	// federation-flavoured SDL describing the schema's tables. See
	// FederationConfig for the full surface.
	Federation FederationConfig `mapstructure:"federation" json:"federation" yaml:"federation" jsonschema:"title=Apollo Federation v2"`

	// Filesystems exposes object stores (local directories, S3, GCS) as
	// virtual tables in the GraphQL schema. Each entry materialises one
	// table with a fixed shape — key/size/content_type/etag/modified_at/url/data —
	// and routes queries through a pluggable Backend.
	Filesystems []FilesystemConfig `mapstructure:"filesystems" json:"filesystems" yaml:"filesystems" jsonschema:"title=Filesystem Tables"`

	// Metadata exposes GraphJin-discovered database metadata for legacy
	// internal helpers. Public GraphQL discovery should use gj_catalog
	// catalog items instead of separate table/column roots.
	Metadata MetadataConfig `mapstructure:"metadata" json:"metadata" yaml:"metadata" jsonschema:"title=Metadata Graph"`

	// Catalog exposes GraphJin's AI-first self-model as a managed read-only
	// database. It supersedes the older metadata-only graph.
	Catalog CatalogConfig `mapstructure:"catalog" json:"catalog" yaml:"catalog" jsonschema:"title=Catalog Graph"`

	sourcesNormalized bool
}

// IdentityConfig declares how request-wide identity is extracted in source mode.
type IdentityConfig struct {
	UserIDClaim    string   `mapstructure:"user_id_claim" json:"user_id_claim" yaml:"user_id_claim" jsonschema:"title=User ID Claim,default=sub"`
	RoleClaims     []string `mapstructure:"role_claims" json:"role_claims" yaml:"role_claims" jsonschema:"title=Role Claims"`
	NamespaceClaim string   `mapstructure:"namespace_claim" json:"namespace_claim" yaml:"namespace_claim" jsonschema:"title=Namespace Claim,default=account_id"`
	AdminRoles     []string `mapstructure:"admin_roles" json:"admin_roles" yaml:"admin_roles" jsonschema:"title=Admin Roles"`
	Query          string   `mapstructure:"query" json:"query" yaml:"query" jsonschema:"title=Identity Enrichment Query"`
}

// ArtifactsConfig declares the GraphJin-managed SQL artifact store.
type ArtifactsConfig struct {
	Enabled                   bool     `mapstructure:"enabled" json:"enabled" yaml:"enabled" jsonschema:"title=Enable Artifacts,description=Enable the GraphJin-managed SQL artifact store for saved queries/fragments/workflows and catalog annotations; parsed dev and agentic configs default to enabled"`
	Source                    string   `mapstructure:"source" json:"source" yaml:"source" jsonschema:"title=Artifact Source,description=Database source name that hosts the artifact store; omitted dev and agentic configs use private managed SQLite while legacy explicit enablement uses the first writable SQL source"`
	Schema                    string   `mapstructure:"schema" json:"schema" yaml:"schema" jsonschema:"title=Artifact Schema,default=_graphjin,description=Schema for the artifact store tables"`
	AutoInit                  *bool    `mapstructure:"auto_init" json:"auto_init" yaml:"auto_init" jsonschema:"title=Auto Initialize Artifact Tables,default=true,description=Create artifact store tables automatically at startup"`
	GlobalsPath               string   `mapstructure:"globals_path" json:"globals_path" yaml:"globals_path" jsonschema:"title=Global Config Artifact Path,default=./config,description=Directory of read-only global artifacts loaded from config files"`
	Locked                    []string `mapstructure:"locked" json:"locked" yaml:"locked" jsonschema:"title=Locked Artifact Kinds,description=Artifact kinds that refuse writes through gj_artifacts"`
	PollSeconds               int      `mapstructure:"poll_seconds" json:"poll_seconds" yaml:"poll_seconds" jsonschema:"title=Artifact Poll Seconds,default=15,description=Fallback and reconnect interval for internal artifact and watch revision subscriptions"`
	ProjectionContentMaxBytes int      `mapstructure:"projection_content_max_bytes" json:"projection_content_max_bytes" yaml:"projection_content_max_bytes" jsonschema:"title=Projection Content Max Bytes,default=32768,description=Per-field byte cap for content and JSON fields in the in-memory gj_artifacts search projection; oversized values are dropped from the projection but stay fully readable through the artifact store"`
	MaxPerOwner               int      `mapstructure:"max_per_owner" json:"max_per_owner" yaml:"max_per_owner" jsonschema:"title=Max Artifacts Per Owner,default=200,description=Shared maximum catalog-visible saved-query fragment and workflow artifacts per owner; existing rows remain updatable at the cap"`
}

// WatchesConfig declares the GraphJin-managed watch store and runner settings.
type WatchesConfig struct {
	Enabled             bool     `mapstructure:"enabled" json:"enabled" yaml:"enabled" jsonschema:"title=Enable Watches,description=Enable durable owner-scoped watches; parsed dev and agentic configs default to enabled when artifacts are enabled"`
	MaxPerOwner         int      `mapstructure:"max_per_owner" json:"max_per_owner" yaml:"max_per_owner" jsonschema:"title=Max Watches Per Owner,default=20,description=Maximum active watches per owner"`
	EventRetentionHours int      `mapstructure:"event_retention_hours" json:"event_retention_hours" yaml:"event_retention_hours" jsonschema:"title=Watch Event Retention Hours,default=168,description=Hours to keep fired watch events before pruning"`
	MaxEventsPerWatch   int      `mapstructure:"max_events_per_watch" json:"max_events_per_watch" yaml:"max_events_per_watch" jsonschema:"title=Max Events Per Watch,default=500,description=Maximum stored events per watch; oldest are pruned first"`
	SnapshotMaxBytes    int      `mapstructure:"snapshot_max_bytes" json:"snapshot_max_bytes" yaml:"snapshot_max_bytes" jsonschema:"title=Watch Snapshot Max Bytes,default=32768,description=Byte cap for stored event snapshots and user-supplied watch-definition JSON fields"`
	Runner              string   `mapstructure:"runner" json:"runner" yaml:"runner" jsonschema:"title=Watch Runner,enum=all,enum=off,description=Which replicas evaluate watches; parsed dev and agentic configs default to all while literal and prod configs remain off"`
	WebhookAllow        []string `mapstructure:"webhook_allow" json:"webhook_allow" yaml:"webhook_allow" jsonschema:"title=Watch Webhook Allowlist,description=Allowlist of webhook URL prefixes watch deliveries may call; empty denies all webhooks"`
	EnrichmentDailyCap  int      `mapstructure:"enrichment_daily_cap" json:"enrichment_daily_cap" yaml:"enrichment_daily_cap" jsonschema:"title=Watch Enrichment Daily Cap,default=10,description=Maximum read-only agent enrichments per watch per day"`
	EnrichmentWorkers   int      `mapstructure:"enrichment_workers" json:"enrichment_workers" yaml:"enrichment_workers" jsonschema:"title=Watch Enrichment Workers,default=1,description=Concurrent watch enrichment workers"`
	RetryMaxFailures    int      `mapstructure:"retry_max_failures" json:"retry_max_failures" yaml:"retry_max_failures" jsonschema:"title=Watch Retry Max Failures,default=5,description=Consecutive transient runner failures before a watch enters terminal error status"`
}

// TasksConfig declares the GraphJin-managed durable declared-intent task store.
type TasksConfig struct {
	Enabled             bool `mapstructure:"enabled" json:"enabled" yaml:"enabled" jsonschema:"title=Enable Tasks,description=Enable durable owner-scoped declared-intent tasks; parsed dev and agentic configs default to enabled when artifacts are enabled"`
	MaxPerOwner         int  `mapstructure:"max_per_owner" json:"max_per_owner" yaml:"max_per_owner" jsonschema:"title=Max Tasks Per Owner,default=20,description=Maximum open tasks per owner"`
	MaxEntriesPerTask   int  `mapstructure:"max_entries_per_task" json:"max_entries_per_task" yaml:"max_entries_per_task" jsonschema:"title=Max Entries Per Task,default=500,description=Maximum stored trail entries per task; oldest are pruned first"`
	EntryRetentionHours int  `mapstructure:"entry_retention_hours" json:"entry_retention_hours" yaml:"entry_retention_hours" jsonschema:"title=Task Entry Retention Hours,default=168,description=Hours to keep task trail entries before prune-on-write and projection trimming"`
	SnapshotMaxBytes    int  `mapstructure:"snapshot_max_bytes" json:"snapshot_max_bytes" yaml:"snapshot_max_bytes" jsonschema:"title=Task Snapshot Max Bytes,default=32768,description=Byte cap for task snapshot_json and task-entry detail_json"`
}

// SourceAccessConfig declares source-level access defaults plus table/root
// classifications. It is intentionally small; it compiles down to legacy role
// table rules before qcode runs.
type SourceAccessConfig struct {
	Read                   string   `mapstructure:"read" json:"read" yaml:"read" jsonschema:"title=Read Access Mode"`
	Write                  string   `mapstructure:"write" json:"write" yaml:"write" jsonschema:"title=Write Access Mode"`
	Delete                 string   `mapstructure:"delete" json:"delete" yaml:"delete" jsonschema:"title=Delete Access Mode"`
	NamespaceColumn        string   `mapstructure:"namespace_column" json:"namespace_column" yaml:"namespace_column" jsonschema:"title=Namespace Column,default=account_id"`
	OwnerColumn            string   `mapstructure:"owner_column" json:"owner_column" yaml:"owner_column" jsonschema:"title=Owner Column,default=user_id"`
	MissingNamespaceColumn string   `mapstructure:"missing_namespace_column" json:"missing_namespace_column" yaml:"missing_namespace_column" jsonschema:"title=Missing Namespace Column Behavior,enum=block,enum=allow"`
	PublicTables           []string `mapstructure:"public_tables" json:"public_tables" yaml:"public_tables" jsonschema:"title=Public Tables"`
	AdminTables            []string `mapstructure:"admin_tables" json:"admin_tables" yaml:"admin_tables" jsonschema:"title=Admin Tables"`
	BlockedTables          []string `mapstructure:"blocked_tables" json:"blocked_tables" yaml:"blocked_tables" jsonschema:"title=Blocked Tables"`
}

// SystemConfig controls GraphJin-owned capabilities and caller access to
// built-in gj_* roots. Omitted values use mode defaults.
type SystemConfig struct {
	Capabilities map[string]bool   `mapstructure:"capabilities" json:"capabilities,omitempty" yaml:"capabilities,omitempty" jsonschema:"title=System Capabilities"`
	RootAccess   map[string]string `mapstructure:"root_access" json:"root_access,omitempty" yaml:"root_access,omitempty" jsonschema:"title=System Root Access"`
}

func (c SystemConfig) clone() SystemConfig {
	out := c
	if c.Capabilities != nil {
		out.Capabilities = make(map[string]bool, len(c.Capabilities))
		for key, value := range c.Capabilities {
			out.Capabilities[key] = value
		}
	}
	if c.RootAccess != nil {
		out.RootAccess = make(map[string]string, len(c.RootAccess))
		for root, mode := range c.RootAccess {
			out.RootAccess[root] = mode
		}
	}
	return out
}

// WorkflowsConfig controls workflow discovery and permissions. GraphJin ships
// one JavaScript runtime (Goja), so runtime selection is intentionally absent.
type WorkflowsConfig struct {
	Path         string          `mapstructure:"path" json:"path,omitempty" yaml:"path,omitempty" jsonschema:"title=Workflow Path,default=./workflows"`
	Capabilities map[string]bool `mapstructure:"capabilities" json:"capabilities,omitempty" yaml:"capabilities,omitempty" jsonschema:"title=Workflow Capabilities"`
}

func (c WorkflowsConfig) clone() WorkflowsConfig {
	out := c
	if c.Capabilities != nil {
		out.Capabilities = make(map[string]bool, len(c.Capabilities))
		for key, value := range c.Capabilities {
			out.Capabilities[key] = value
		}
	}
	return out
}

// SourceConfig declares one external provider in sources.
type SourceConfig struct {
	Name    string `mapstructure:"name" json:"name" yaml:"name" jsonschema:"title=Name"`
	Kind    string `mapstructure:"kind" json:"kind" yaml:"kind" jsonschema:"title=Kind,enum=database,enum=code,enum=file,enum=api"`
	Default bool   `mapstructure:"default" json:"default" yaml:"default" jsonschema:"title=Default Source"`

	Type                   string                        `mapstructure:"type" json:"type" yaml:"type" jsonschema:"title=Database Type"`
	ConnString             string                        `mapstructure:"connection_string" json:"connection_string" yaml:"connection_string" jsonschema:"title=Connection String" jsonschema_extras:"x-graphjin-sensitive=connection"`
	Host                   string                        `mapstructure:"host" json:"host" yaml:"host" jsonschema:"title=Host"`
	Port                   int                           `mapstructure:"port" json:"port" yaml:"port" jsonschema:"title=Port"`
	DBName                 string                        `mapstructure:"dbname" json:"dbname" yaml:"dbname" jsonschema:"title=Database Name"`
	User                   string                        `mapstructure:"user" json:"user" yaml:"user" jsonschema:"title=User"`
	Password               string                        `mapstructure:"password" json:"password" yaml:"password" jsonschema:"title=Password" jsonschema_extras:"x-graphjin-sensitive=secret"`
	Path                   string                        `mapstructure:"path" json:"path" yaml:"path" jsonschema:"title=Path"`
	MaxOpenConns           int                           `mapstructure:"max_open_conns" json:"max_open_conns" yaml:"max_open_conns" jsonschema:"title=Max Open Connections"`
	MaxIdleConns           int                           `mapstructure:"max_idle_conns" json:"max_idle_conns" yaml:"max_idle_conns" jsonschema:"title=Max Idle Connections"`
	Schema                 string                        `mapstructure:"schema" json:"schema" yaml:"schema" jsonschema:"title=Schema"`
	PoolSize               int                           `mapstructure:"pool_size" json:"pool_size" yaml:"pool_size" jsonschema:"title=Connection Pool Size"`
	MaxConnections         int                           `mapstructure:"max_connections" json:"max_connections" yaml:"max_connections" jsonschema:"title=Maximum Connections"`
	MaxConnIdleTime        time.Duration                 `mapstructure:"max_connection_idle_time" json:"max_connection_idle_time" yaml:"max_connection_idle_time" jsonschema:"title=Connection Idle Time"`
	MaxConnLifeTime        time.Duration                 `mapstructure:"max_connection_life_time" json:"max_connection_life_time" yaml:"max_connection_life_time" jsonschema:"title=Connection Life Time"`
	PingTimeout            time.Duration                 `mapstructure:"ping_timeout" json:"ping_timeout" yaml:"ping_timeout" jsonschema:"title=Healthcheck Ping Timeout"`
	EnableTLS              bool                          `mapstructure:"enable_tls" json:"enable_tls" yaml:"enable_tls" jsonschema:"title=Enable TLS"`
	ServerName             string                        `mapstructure:"server_name" json:"server_name" yaml:"server_name" jsonschema:"title=TLS Server Name"`
	ServerCert             string                        `mapstructure:"server_cert" json:"server_cert" yaml:"server_cert" jsonschema:"title=Server Certificate" jsonschema_extras:"x-graphjin-sensitive=certificate"`
	ClientCert             string                        `mapstructure:"client_cert" json:"client_cert" yaml:"client_cert" jsonschema:"title=Client Certificate" jsonschema_extras:"x-graphjin-sensitive=certificate"`
	ClientKey              string                        `mapstructure:"client_key" json:"client_key" yaml:"client_key" jsonschema:"title=Client Key" jsonschema_extras:"x-graphjin-sensitive=key_material"`
	Encrypt                *bool                         `mapstructure:"encrypt" json:"encrypt,omitempty" yaml:"encrypt,omitempty" jsonschema:"title=MSSQL Encrypt"`
	TrustServerCertificate *bool                         `mapstructure:"trust_server_certificate" json:"trust_server_certificate,omitempty" yaml:"trust_server_certificate,omitempty" jsonschema:"title=MSSQL Trust Server Certificate"`
	PrivateKeyPath         string                        `mapstructure:"private_key_path" json:"private_key_path" yaml:"private_key_path" jsonschema:"title=Private Key File Path (Snowflake)" jsonschema_extras:"x-graphjin-sensitive=secret_ref"`
	PrivateKeyPEM          string                        `mapstructure:"private_key_pem" json:"private_key_pem" yaml:"private_key_pem" jsonschema:"title=Private Key PEM (Snowflake)" jsonschema_extras:"x-graphjin-sensitive=key_material"`
	KeyPassphrase          string                        `mapstructure:"key_passphrase" json:"key_passphrase" yaml:"key_passphrase" jsonschema:"title=Key Passphrase (Snowflake)" jsonschema_extras:"x-graphjin-sensitive=secret"`
	ReadOnly               bool                          `mapstructure:"read_only" json:"read_only" yaml:"read_only" jsonschema:"title=Read Only"`
	AnalyticsMode          *bool                         `mapstructure:"analytics_mode" json:"analytics_mode,omitempty" yaml:"analytics_mode,omitempty" jsonschema:"title=Analytics Mode"`
	InferDBRefs            *bool                         `mapstructure:"infer_db_refs" json:"infer_db_refs,omitempty" yaml:"infer_db_refs,omitempty" jsonschema:"title=Infer Database References"`
	Backend                string                        `mapstructure:"backend" json:"backend" yaml:"backend" jsonschema:"title=Filesystem Backend"`
	Bucket                 string                        `mapstructure:"bucket" json:"bucket" yaml:"bucket" jsonschema:"title=Bucket"`
	Region                 string                        `mapstructure:"region" json:"region" yaml:"region" jsonschema:"title=Region"`
	Endpoint               string                        `mapstructure:"endpoint" json:"endpoint" yaml:"endpoint" jsonschema:"title=Endpoint"`
	Prefix                 string                        `mapstructure:"prefix" json:"prefix" yaml:"prefix" jsonschema:"title=Prefix"`
	Root                   string                        `mapstructure:"root" json:"root" yaml:"root" jsonschema:"title=Root"`
	PresignTTL             time.Duration                 `mapstructure:"presign_ttl" json:"presign_ttl" yaml:"presign_ttl" jsonschema:"title=Presigned URL TTL"`
	PublicBaseURL          string                        `mapstructure:"public_base_url" json:"public_base_url" yaml:"public_base_url" jsonschema:"title=Public Base URL"`
	MaxListPageSize        int                           `mapstructure:"max_list_page_size" json:"max_list_page_size" yaml:"max_list_page_size" jsonschema:"title=Max List Page Size"`
	SpecsDir               string                        `mapstructure:"specs_dir" json:"specs_dir" yaml:"specs_dir" jsonschema:"title=OpenAPI Specs Directory"`
	Specs                  map[string]openapi.SpecConfig `mapstructure:"specs" json:"specs" yaml:"specs" jsonschema:"-"`
	Capabilities           map[string]bool               `mapstructure:"capabilities" json:"capabilities,omitempty" yaml:"capabilities,omitempty" jsonschema:"title=Capabilities"`
	Access                 SourceAccessConfig            `mapstructure:"access" json:"access" yaml:"access" jsonschema:"title=Access"`
}

type RelationshipConfig struct {
	From string `mapstructure:"from" json:"from" yaml:"from" jsonschema:"title=From"`
	To   string `mapstructure:"to" json:"to" yaml:"to" jsonschema:"title=To"`
	As   string `mapstructure:"as" json:"as,omitempty" yaml:"as,omitempty" jsonschema:"title=Alias"`
}

// MetadataConfig controls the managed metadata graph database.
type MetadataConfig struct {
	// Enabled controls whether GraphJin exposes discovered metadata tables.
	// When omitted, it defaults to enabled in dev and disabled in production.
	Enabled *bool `mapstructure:"enabled" json:"enabled,omitempty" yaml:"enabled,omitempty" jsonschema:"title=Enable Metadata Graph"`

	// Database is the virtual database name. Defaults to "graphjin".
	Database string `mapstructure:"database" json:"database" yaml:"database" jsonschema:"title=Metadata Database,default=graphjin"`

	// AutoCodeRelations controls automatic relationships from catalog items
	// to CodeSQL's public gj_code projection. When omitted, it follows Enabled.
	AutoCodeRelations *bool `mapstructure:"auto_code_relations" json:"auto_code_relations,omitempty" yaml:"auto_code_relations,omitempty" jsonschema:"title=Auto Code Relations"`

	// CodeDatabases optionally pins which CodeSQL database should be linked.
	// Empty means auto-detect, but only when exactly one CodeSQL DB exists.
	CodeDatabases []string `mapstructure:"code_databases" json:"code_databases,omitempty" yaml:"code_databases,omitempty" jsonschema:"title=CodeSQL Databases"`
}

// CatalogConfig controls the managed AI-first catalog graph database.
type CatalogConfig struct {
	// Enabled controls whether GraphJin exposes catalog tables.
	// When omitted, it defaults to enabled in dev and disabled in production.
	Enabled any `mapstructure:"enabled" json:"enabled,omitempty" yaml:"enabled,omitempty" jsonschema:"title=Enable Catalog Graph,enum=auto,enum=true,enum=false,default=auto"`

	// Database is the virtual database name. Defaults to "graphjin".
	Database string `mapstructure:"database" json:"database" yaml:"database" jsonschema:"title=Catalog Database,default=graphjin"`

	// Samples controls whether live sample/profile data is inlined or fetched on
	// demand. Default: on_demand.
	Samples string `mapstructure:"samples" json:"samples" yaml:"samples" jsonschema:"title=Catalog Sample Mode,enum=on_demand,enum=inline,enum=off,default=on_demand"`

	// AutoCodeRelations controls automatic relationships from catalog/schema
	// tables to CodeSQL's code_db_refs. When omitted, it follows Enabled.
	AutoCodeRelations any `mapstructure:"auto_code_relations" json:"auto_code_relations,omitempty" yaml:"auto_code_relations,omitempty" jsonschema:"title=Auto Code Relations,enum=auto,enum=true,enum=false,default=auto"`

	// CodeDatabases optionally pins which CodeSQL database should be linked.
	// Empty means auto-detect, but only when exactly one CodeSQL DB exists.
	CodeDatabases []string `mapstructure:"code_databases" json:"code_databases,omitempty" yaml:"code_databases,omitempty" jsonschema:"title=CodeSQL Databases"`
}

func (c *Config) CatalogEnabled() bool {
	if c == nil {
		return false
	}
	if c.modeForSourceDefaults() == sourcecap.ModeProd {
		return false
	}
	return c.FeatureCapability(featurecap.KindSystem, featurecap.KeyCatalogRead)
}

func (c *Config) CatalogDatabaseName() string {
	if c == nil {
		return "graphjin"
	}
	if strings.TrimSpace(c.Catalog.Database) != "" {
		return strings.TrimSpace(c.Catalog.Database)
	}
	if strings.TrimSpace(c.Metadata.Database) != "" {
		return strings.TrimSpace(c.Metadata.Database)
	}
	return "graphjin"
}

func (c *Config) CatalogAutoCodeRelationsEnabled() bool {
	if c == nil {
		return false
	}
	return c.CatalogEnabled()
}

func (c *Config) CatalogCodeDatabases() []string {
	return nil
}

func (c *Config) MetadataEnabled() bool {
	if c == nil {
		return false
	}
	return c.CatalogEnabled()
}

func (c *Config) MetadataDatabaseName() string {
	return c.CatalogDatabaseName()
}

func (c *Config) MetadataAutoCodeRelationsEnabled() bool {
	return c.CatalogAutoCodeRelationsEnabled()
}

// FilesystemConfig declares one filesystem-backed virtual table.
//
//	filesystems:
//	  - name: avatars
//	    backend: s3
//	    bucket: my-bucket
//	    prefix: avatars/
//	    region: us-east-1
//	    presign_ttl: 15m
//
//	  - name: invoices
//	    backend: gcs
//	    bucket: invoices
//	    prefix: 2026/
//
//	  - name: uploads_local
//	    backend: local
//	    root: /var/lib/graphjin/uploads
//
// Backend-specific options live alongside the common ones; unrecognised
// fields per backend produce a clear error at startup rather than being
// silently ignored.
type FilesystemConfig struct {
	// Name is the table name surfaced in GraphQL (e.g. "avatars").
	Name string `mapstructure:"name" json:"name" yaml:"name" jsonschema:"title=Table Name"`

	// Backend selects the implementation: "local", "s3", or "gcs".
	// S3 and GCS are gated by build tags; on a slim build, configuring
	// them produces a clear error.
	Backend string `mapstructure:"backend" json:"backend" yaml:"backend" jsonschema:"title=Backend,enum=local,enum=s3,enum=gcs"`

	// Bucket is the S3 bucket / GCS bucket name. Ignored for local.
	Bucket string `mapstructure:"bucket" json:"bucket" yaml:"bucket" jsonschema:"title=Bucket"`

	// Region is the S3 region (e.g. "us-east-1"). Ignored for local/gcs.
	Region string `mapstructure:"region" json:"region" yaml:"region" jsonschema:"title=Region"`

	// Endpoint, when non-empty, overrides the S3 endpoint URL — useful
	// for MinIO, localstack, or non-AWS S3-compatible services.
	Endpoint string `mapstructure:"endpoint" json:"endpoint" yaml:"endpoint" jsonschema:"title=Endpoint Override"`

	// Prefix is prepended to every key. Effective root for the table.
	Prefix string `mapstructure:"prefix" json:"prefix" yaml:"prefix" jsonschema:"title=Key Prefix"`

	// Root is the local filesystem root directory. Required for the
	// local backend; ignored otherwise.
	Root string `mapstructure:"root" json:"root" yaml:"root" jsonschema:"title=Local Root Directory"`

	// PresignTTL bounds how long generated presigned URLs are valid.
	// Defaults to 15 minutes when zero. Local backend ignores this.
	PresignTTL time.Duration `mapstructure:"presign_ttl" json:"presign_ttl" yaml:"presign_ttl" jsonschema:"title=Presigned URL TTL,default=15m"`

	// PublicBaseURL, when set, replaces the presigned-URL host. Useful
	// when objects are fronted by a CDN. The path is appended unchanged.
	PublicBaseURL string `mapstructure:"public_base_url" json:"public_base_url" yaml:"public_base_url" jsonschema:"title=Public Base URL"`

	// ReadOnly disables GraphJin-managed writes and local filesystem watch
	// invalidation for this filesystem table.
	ReadOnly bool `mapstructure:"read_only" json:"read_only" yaml:"read_only" jsonschema:"title=Read Only"`

	// MaxListPageSize caps the number of entries returned per list call.
	// Defaults to 1000 when zero.
	MaxListPageSize int `mapstructure:"max_list_page_size" json:"max_list_page_size" yaml:"max_list_page_size" jsonschema:"title=Max List Page Size,default=1000"`
}

// DatabaseConfig defines configuration for a single database in multi-database mode
type DatabaseConfig struct {
	// Database type (postgres, mysql, mariadb, sqlite, oracle, mongodb, snowflake, nanodb)
	Type string `mapstructure:"type" json:"type" yaml:"type" jsonschema:"title=Database Type,enum=postgres,enum=mysql,enum=mariadb,enum=sqlite,enum=oracle,enum=mongodb,enum=snowflake,enum=nanodb,enum=codesql"`

	// ManagedType preserves the service-level logical database type after a
	// managed database has been translated to its runtime driver.
	ManagedType string `mapstructure:"-" json:"-" yaml:"-" jsonschema:"-"`

	// Connection string for the database (alternative to individual params)
	ConnString string `mapstructure:"connection_string" json:"connection_string" yaml:"connection_string" jsonschema:"title=Connection String" jsonschema_extras:"x-graphjin-sensitive=connection"`

	// Database host
	Host string `mapstructure:"host" json:"host" yaml:"host" jsonschema:"title=Host"`

	// Database port
	Port int `mapstructure:"port" json:"port" yaml:"port" jsonschema:"title=Port"`

	// Database name
	DBName string `mapstructure:"dbname" json:"dbname" yaml:"dbname" jsonschema:"title=Database Name"`

	// Database user
	User string `mapstructure:"user" json:"user" yaml:"user" jsonschema:"title=User"`

	// Database password
	Password string `mapstructure:"password" json:"password" yaml:"password" jsonschema:"title=Password" jsonschema_extras:"x-graphjin-sensitive=secret"`

	// File path for SQLite databases
	Path string `mapstructure:"path" json:"path" yaml:"path" jsonschema:"title=File Path (SQLite)"`

	// Maximum number of open connections
	MaxOpenConns int `mapstructure:"max_open_conns" json:"max_open_conns" yaml:"max_open_conns" jsonschema:"title=Max Open Connections"`

	// Maximum number of idle connections
	MaxIdleConns int `mapstructure:"max_idle_conns" json:"max_idle_conns" yaml:"max_idle_conns" jsonschema:"title=Max Idle Connections"`

	// Schema name to use (for databases that support schemas)
	Schema string `mapstructure:"schema" json:"schema" yaml:"schema" jsonschema:"title=Schema"`

	// Connection pool settings
	PoolSize        int           `mapstructure:"pool_size" json:"pool_size" yaml:"pool_size" jsonschema:"title=Connection Pool Size"`
	MaxConnections  int           `mapstructure:"max_connections" json:"max_connections" yaml:"max_connections" jsonschema:"title=Maximum Connections"`
	MaxConnIdleTime time.Duration `mapstructure:"max_connection_idle_time" json:"max_connection_idle_time" yaml:"max_connection_idle_time" jsonschema:"title=Connection Idle Time"`
	MaxConnLifeTime time.Duration `mapstructure:"max_connection_life_time" json:"max_connection_life_time" yaml:"max_connection_life_time" jsonschema:"title=Connection Life Time"`

	// Health check
	PingTimeout time.Duration `mapstructure:"ping_timeout" json:"ping_timeout" yaml:"ping_timeout" jsonschema:"title=Healthcheck Ping Timeout"`

	// TLS settings
	EnableTLS  bool   `mapstructure:"enable_tls" json:"enable_tls" yaml:"enable_tls" jsonschema:"title=Enable TLS"`
	ServerName string `mapstructure:"server_name" json:"server_name" yaml:"server_name" jsonschema:"title=TLS Server Name"`
	ServerCert string `mapstructure:"server_cert" json:"server_cert" yaml:"server_cert" jsonschema:"title=Server Certificate" jsonschema_extras:"x-graphjin-sensitive=certificate"`
	ClientCert string `mapstructure:"client_cert" json:"client_cert" yaml:"client_cert" jsonschema:"title=Client Certificate" jsonschema_extras:"x-graphjin-sensitive=certificate"`
	ClientKey  string `mapstructure:"client_key" json:"client_key" yaml:"client_key" jsonschema:"title=Client Key" jsonschema_extras:"x-graphjin-sensitive=key_material"`

	// MSSQL-specific: disable TLS encryption (go-mssqldb defaults to encrypt=true)
	Encrypt *bool `mapstructure:"encrypt" json:"encrypt,omitempty" yaml:"encrypt,omitempty" jsonschema:"title=MSSQL Encrypt"`

	// MSSQL-specific: trust server certificate without validation
	TrustServerCertificate *bool `mapstructure:"trust_server_certificate" json:"trust_server_certificate,omitempty" yaml:"trust_server_certificate,omitempty" jsonschema:"title=MSSQL Trust Server Certificate"`

	// Snowflake key pair authentication (PKCS#8 PEM format).
	// Generate key: openssl genrsa 2048 | openssl pkcs8 -topk8 -inform PEM -out rsa_key.p8
	PrivateKeyPath string `mapstructure:"private_key_path" json:"private_key_path" yaml:"private_key_path" jsonschema:"title=Private Key File Path (Snowflake)" jsonschema_extras:"x-graphjin-sensitive=secret_ref"`
	PrivateKeyPEM  string `mapstructure:"private_key_pem" json:"private_key_pem" yaml:"private_key_pem" jsonschema:"title=Private Key PEM (Snowflake)" jsonschema_extras:"x-graphjin-sensitive=key_material"`
	KeyPassphrase  string `mapstructure:"key_passphrase" json:"key_passphrase" yaml:"key_passphrase" jsonschema:"title=Key Passphrase (Snowflake)" jsonschema_extras:"x-graphjin-sensitive=secret"`

	// Read-only mode — blocks all mutations and DDL against this database.
	// Once set in config, cannot be changed at runtime via MCP tools.
	ReadOnly bool `mapstructure:"read_only" json:"read_only" yaml:"read_only" jsonschema:"title=Read Only"`

	AnalyticsMode *bool `mapstructure:"analytics_mode" json:"analytics_mode,omitempty" yaml:"analytics_mode,omitempty" jsonschema:"title=Analytics Mode (per-DB override)"`

	// InferDBRefs controls CodeSQL's best-effort code-to-database reference
	// inference. It is only used for databases with type: codesql.
	InferDBRefs *bool `mapstructure:"infer_db_refs" json:"infer_db_refs,omitempty" yaml:"infer_db_refs,omitempty" jsonschema:"title=Infer Database References"`
}

func (c *Config) EffectiveAnalyticsMode(database string) bool {
	if c == nil {
		return false
	}
	if db, ok := c.Databases[database]; ok && db.AnalyticsMode != nil {
		return *db.AnalyticsMode
	}
	return c.AnalyticsMode
}

// SnowflakeKeyPairConfig allows external services to inject Snowflake key pair
// credentials programmatically (e.g., separate pipelines for Marketo, Salesforce, etc.).
type SnowflakeKeyPairConfig interface {
	GetAccount() string
	GetUser() string
	GetPrivateKeyPEM() []byte
	GetKeyPassphrase() string
	GetWarehouse() string
	GetDatabase() string
	GetSchema() string
}

// Configuration for a database table
type Table struct {
	Name   string
	Schema string
	Table  string // Inherits Table
	Type   string
	// Generated marks runtime-only table configuration injected by GraphJin.
	// It cannot be set through public configuration.
	Generated bool `mapstructure:"-" json:"-" yaml:"-" jsonschema:"-"`
	// Source is required in sources used and points at one sources[].name.
	Source string `mapstructure:"source" json:"source" yaml:"source" jsonschema:"title=Source"`
	// Database name for multi-database support. References a key in Config.Databases.
	// If empty, uses the default database.
	Database  string `mapstructure:"database" json:"database" yaml:"database" jsonschema:"title=Database"`
	ReadOnly  bool   `mapstructure:"read_only" json:"read_only" yaml:"read_only" jsonschema:"title=Read Only"`
	Blocklist []string
	Columns   []Column
	// Permitted order by options
	OrderBy map[string][]string `mapstructure:"order_by" json:"order_by" yaml:"order_by" jsonschema:"title=Order By Options,example=created_at desc"`
	// Partition configuration for warehouse-optimized queries (Snowflake, BigQuery).
	// When set, queries without a filter on the partition column will either get a
	// default time-range filter injected or produce a warning.
	Partition *PartitionConfig `mapstructure:"partition" json:"partition,omitempty" yaml:"partition,omitempty" jsonschema:"title=Partition Configuration"`
	// Cassandra per-table options (allow_filtering escape hatch + key overrides).
	Cassandra *CassandraConfig `mapstructure:"cassandra" json:"cassandra,omitempty" yaml:"cassandra,omitempty" jsonschema:"title=Cassandra Table Configuration"`
}

// CassandraConfig declares per-table Cassandra options. Keys are normally
// introspected from system_schema; the override lists are a fail-fast/assertion
// mechanism, not required config.
type CassandraConfig struct {
	// AllowFiltering is the only ALLOW FILTERING escape hatch — enable only for
	// known-small tables. It never relaxes OR/NOT/LIKE/IS NULL rejections.
	AllowFiltering bool `mapstructure:"allow_filtering" json:"allow_filtering,omitempty" yaml:"allow_filtering,omitempty" jsonschema:"title=Allow Filtering"`
	// PartitionKeys / ClusteringKeys optionally override the introspected key roles.
	PartitionKeys  []string `mapstructure:"partition_keys" json:"partition_keys,omitempty" yaml:"partition_keys,omitempty" jsonschema:"title=Partition Keys"`
	ClusteringKeys []string `mapstructure:"clustering_keys" json:"clustering_keys,omitempty" yaml:"clustering_keys,omitempty" jsonschema:"title=Clustering Keys"`
}

// PartitionConfig declares the partition key for a warehouse table.
// When a query does not filter on the partition column:
//   - If DefaultRangeDays > 0, a filter is auto-injected (e.g., created_at >= now - 30 days)
//   - Otherwise, a warning is logged
type PartitionConfig struct {
	// Column is the partition key column name (e.g., "created_at").
	Column string `mapstructure:"column" json:"column" yaml:"column" jsonschema:"title=Partition Column,example=created_at"`
	// DefaultRangeDays is the number of days to auto-filter when no partition filter
	// is present in the query. Set to 0 to only warn without injecting a filter.
	DefaultRangeDays int  `mapstructure:"default_range_days" json:"default_range_days,omitempty" yaml:"default_range_days,omitempty" jsonschema:"title=Default Range Days,example=30"`
	None             bool `mapstructure:"none" json:"none,omitempty" yaml:"none,omitempty" jsonschema:"title=Disable Partition Check"`
}

// Configuration for a database table column
type Column struct {
	Name       string
	Type       string `jsonschema:"example=integer,example=text"`
	Primary    bool
	Array      bool
	FullText   bool   `mapstructure:"full_text" json:"full_text" yaml:"full_text" jsonschema:"title=Full Text Search"`
	ForeignKey string `mapstructure:"related_to" json:"related_to" yaml:"related_to" jsonschema:"title=Related To,example=other_table.id_column,example=users.id"`
}

// Configuration for a database function
type Function struct {
	Name       string
	Schema     string
	ReturnType string `mapstructure:"return_type" json:"return_type" yaml:"return_type" jsonschema:"title=Return Type,example=boolean,example=record"`
}

// Configuration for user role
type Role struct {
	Name    string
	Comment string
	Match   string      `jsonschema:"title=Related To,example=other_table.id_column,example=users.id"`
	Tables  []RoleTable `jsonschema:"title=Table Configuration for Role"`
	tm      map[string]*RoleTable
}

// Table configuration for a specific role (user role)
type RoleTable struct {
	Name      string
	Schema    string
	Database  string `mapstructure:"database" json:"database" yaml:"database" jsonschema:"title=Database"`
	ReadOnly  bool   `mapstructure:"read_only" json:"read_only" yaml:"read_only" jsonschema:"title=Read Only"`
	Generated bool   `mapstructure:"-" json:"-" yaml:"-" jsonschema:"-"`

	Query  *Query
	Insert *Insert
	Update *Update
	Upsert *Upsert
	Delete *Delete
}

// Table configuration for querying a table with a role
type Query struct {
	Limit int
	// Use filters to enforce table wide things like { disabled: false } where you never want disabled users to be shown.
	Filters          []string
	Columns          []string
	DisableFunctions bool `mapstructure:"disable_functions" json:"disable_functions" yaml:"disable_functions"`
	Block            bool
}

// Table configuration for inserting into a table with a role
type Insert struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

// Table configuration for updating a table with a role
type Update struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

// Table configuration for creating/updating (upsert) a table with a role
type Upsert struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

// Table configuration for deleting from a table with a role
type Delete struct {
	Filters []string
	Columns []string
	Block   bool
}

// Resolver interface is used to create custom resolvers
// Custom resolvers must return a JSON value to be merged into
// the response JSON.
//
// Example Redis Resolver:
/*
	type Redis struct {
		Addr string
		client redis.Client
	}

	func newRedis(v map[string]interface{}) (*Redis, error) {
		re := &Redis{}
		if err := mapstructure.Decode(v, re); err != nil {
			return nil, err
		}
		re.client := redis.NewClient(&redis.Options{
			Addr:     re.Addr,
			Password: "", // no password set
			DB:       0,  // use default DB
		})
		return re, nil
	}

	func (r *remoteAPI) Resolve(req ResolverReq) ([]byte, error) {
		val, err := rdb.Get(ctx, req.ID).Result()
		if err != nil {
				return err
		}

		return val, nil
	}

	func main() {
		conf := core.Config{
			Resolvers: []Resolver{
				Name: "cached_profile",
				Type: "redis",
				Table: "users",
				Column: "id",
				Props: []ResolverProps{
					"addr": "localhost:6379",
				},
			},
		}

		redisRe := func(v ResolverProps) (Resolver, error) {
			return newRedis(v)
		}

		gj, err := core.NewGraphJin(conf, db,
			core.OptionSetResolver("redis" redisRe))
		if err != nil {
			log.Fatal(err)
		}
	}
*/
type Resolver interface {
	Resolve(context.Context, ResolverReq) ([]byte, error)
}

// ResolverProps is a map of properties from the resolver config to be passed
// to the customer resolver's builder (new) function
type ResolverProps map[string]interface{}

// ResolverConfig struct defines a custom resolver
type ResolverConfig struct {
	Name      string
	Type      string
	Schema    string
	Table     string
	Column    string
	StripPath string        `mapstructure:"strip_path" json:"strip_path" yaml:"strip_path"`
	Props     ResolverProps `mapstructure:",remain"`

	// remoteColumns carries the response shape a synthesised resolver already
	// knows, so the remote table it registers is not empty. It is unexported
	// because it is derived, never configured: a user writing a resolver by hand
	// has no response schema to describe.
	remoteColumns []sdata.DBColumn
}

type ResolverReq struct {
	ID   string
	Sel  *qcode.Select
	Log  *log.Logger
	Vars map[string]json.RawMessage
	*RequestConfig
}

// AddRoleTable function is a helper function to make it easy to add per-table
// row-level config
func (c *Config) AddRoleTable(role, table string, conf interface{}) error {
	var r *Role

	for i := range c.Roles {
		if strings.EqualFold(c.Roles[i].Name, role) {
			r = &c.Roles[i]
			break
		}
	}
	if r == nil {
		nr := Role{Name: role}
		c.Roles = append(c.Roles, nr)
		r = &c.Roles[len(c.Roles)-1]
	}

	var schema string

	if v := strings.SplitN(table, ".", 2); len(v) == 2 {
		schema = v[0]
		table = v[1]
	}

	var t *RoleTable
	for i := range r.Tables {
		if strings.EqualFold(r.Tables[i].Name, table) &&
			strings.EqualFold(r.Tables[i].Schema, schema) {
			t = &r.Tables[i]
			break
		}
	}
	if t == nil {
		nt := RoleTable{Name: table, Schema: schema}
		r.Tables = append(r.Tables, nt)
		t = &r.Tables[len(r.Tables)-1]
	}

	switch v := conf.(type) {
	case Query:
		t.Query = &v
	case Insert:
		t.Insert = &v
	case Update:
		t.Update = &v
	case Upsert:
		t.Upsert = &v
	case Delete:
		t.Delete = &v
	default:
		return fmt.Errorf("unsupported object type: %t", v)
	}
	return nil
}

func (c *Config) RemoveRoleTable(role, table string) error {
	ri := -1

	for i := range c.Roles {
		if strings.EqualFold(c.Roles[i].Name, role) {
			ri = i
			break
		}
	}
	if ri == -1 {
		return fmt.Errorf("role not found: %s", role)
	}

	tables := c.Roles[ri].Tables
	ti := -1

	var schema string

	if v := strings.SplitN(table, ".", 2); len(v) == 2 {
		schema = v[0]
		table = v[1]
	}

	for i, t := range tables {
		if strings.EqualFold(t.Name, table) &&
			strings.EqualFold(t.Schema, schema) {
			ti = i
			break
		}
	}
	if ti == -1 {
		return fmt.Errorf("table not found: %s", table)
	}

	c.Roles[ri].Tables = append(tables[:ti], tables[ti+1:]...)
	if len(c.Roles[ri].Tables) == 0 {
		c.Roles = append(c.Roles[:ri], c.Roles[ri+1:]...)
	}
	return nil
}
