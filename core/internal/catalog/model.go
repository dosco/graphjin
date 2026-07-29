package catalog

import "time"

// Snapshot is the AI-facing catalog projection of GraphJin's own world:
// database schema, language features, config, and callable capabilities.
type Snapshot struct {
	GeneratedAt     time.Time         `json:"generated_at"`
	Revision        string            `json:"revision,omitempty"`
	SourceRevisions map[string]string `json:"source_revisions,omitempty"`
	Cards           []Card            `json:"cards"`
	Details         []CardDetail      `json:"details,omitempty"`
	Nodes           []Node            `json:"nodes,omitempty"`
	Edges           []Edge            `json:"edges,omitempty"`
	EntryPoints     []EntryPoint      `json:"entrypoints,omitempty"`
	Capabilities    []Capability      `json:"capabilities,omitempty"`
	search          *searchIndex      `json:"-"`
}

type BuildOptions struct {
	Sources                []Source     `json:"sources,omitempty"`
	Workflows              []Workflow   `json:"workflows,omitempty"`
	Fragments              []Fragment   `json:"fragments,omitempty"`
	SavedQueries           []SavedQuery `json:"saved_queries,omitempty"`
	EnabledTools           []string     `json:"enabled_tools,omitempty"`
	EnabledToolsKnown      bool         `json:"enabled_tools_known,omitempty"`
	WorkflowRuntime        string       `json:"workflow_runtime,omitempty"`
	WorkflowTimeoutSeconds int          `json:"workflow_timeout_seconds,omitempty"`
}

type Source struct {
	Name         string          `json:"name"`
	Kind         string          `json:"kind"`
	Type         string          `json:"type,omitempty"`
	Default      bool            `json:"default,omitempty"`
	ReadOnly     bool            `json:"read_only,omitempty"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
}

type Workflow struct {
	Name           string             `json:"name"`
	Description    string             `json:"description,omitempty"`
	Tags           []string           `json:"tags,omitempty"`
	Variables      []WorkflowVariable `json:"variables,omitempty"`
	Path           string             `json:"path,omitempty"`
	SourceHash     string             `json:"source_hash,omitempty"`
	Runtime        string             `json:"runtime,omitempty"`
	TimeoutSeconds int                `json:"timeout_seconds,omitempty"`
	CreatedAt      string             `json:"created_at,omitempty"`
	UpdatedAt      string             `json:"updated_at,omitempty"`
}

type WorkflowVariable struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type Fragment struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	Definition string `json:"definition,omitempty"`
	On         string `json:"on,omitempty"`
	SourceHash string `json:"source_hash,omitempty"`
}

type SavedQuery struct {
	Name       string         `json:"name"`
	Namespace  string         `json:"namespace,omitempty"`
	Operation  string         `json:"operation,omitempty"`
	Query      string         `json:"query,omitempty"`
	Variables  map[string]any `json:"variables,omitempty"`
	SourceHash string         `json:"source_hash,omitempty"`
}

type Query struct {
	Search  string            `json:"search,omitempty"`
	Where   map[string]any    `json:"where,omitempty"`
	OrderBy map[string]string `json:"order_by,omitempty"`
	Limit   int               `json:"limit,omitempty"`
	Offset  int               `json:"offset,omitempty"`
	Explain bool              `json:"explain,omitempty"`

	// Shorthand fields are kept for compatibility with the first catalog MCP
	// surface. Query combines these with Where using an implicit AND.
	Kind     string `json:"kind,omitempty"`
	Database string `json:"database,omitempty"`
	Schema   string `json:"schema,omitempty"`
	Table    string `json:"table,omitempty"`
	Column   string `json:"column,omitempty"`
}

type QueryResult struct {
	Cards   []Card           `json:"cards"`
	Matches map[string]Match `json:"matches,omitempty"`
	Facets  map[string]int   `json:"facets,omitempty"`
}

type Match struct {
	Score         float64  `json:"score"`
	MatchedFields []string `json:"matched_fields,omitempty"`
	MatchedTerms  []string `json:"matched_terms,omitempty"`
	Why           string   `json:"why,omitempty"`
}

// CandidateHint lets an outer service contribute ranked recall candidates
// without coupling the catalog package to an embedding implementation.
type CandidateHint struct {
	CardID string  `json:"card_id"`
	Rank   int     `json:"rank"`
	Weight float64 `json:"weight,omitempty"`
	Score  float64 `json:"score,omitempty"`
	Source string  `json:"source,omitempty"`
	Why    string  `json:"why,omitempty"`
}

type Card struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Title            string `json:"title"`
	Summary          string `json:"summary"`
	DatabaseName     string `json:"database_name,omitempty"`
	SchemaName       string `json:"schema_name,omitempty"`
	TableName        string `json:"table_name,omitempty"`
	ColumnName       string `json:"column_name,omitempty"`
	Source           string `json:"source,omitempty"`
	SourceKind       string `json:"source_kind,omitempty"`
	OwnerSource      string `json:"owner_source,omitempty"`
	OwnerSourcesJSON string `json:"owner_sources_json,omitempty"`
	RiskLevel        string `json:"risk_level,omitempty"`
	Confidence       string `json:"confidence,omitempty"`
	Sensitive        bool   `json:"sensitive,omitempty"`
	Sensitivity      string `json:"sensitivity,omitempty"`
	EvidenceJSON     string `json:"evidence_json,omitempty"`
	ExamplesJSON     string `json:"examples_json,omitempty"`
	SuggestedNext    string `json:"suggested_next_json,omitempty"`
	DetailRef        string `json:"detail_ref,omitempty"`
	QueryJSON        string `json:"query_json,omitempty"`
	InputSchemaJSON  string `json:"input_schema_json,omitempty"`
	OutputSchemaJSON string `json:"output_schema_json,omitempty"`
	SafetyJSON       string `json:"safety_json,omitempty"`
	GraphQLQuery     string `json:"graphql_query,omitempty"`
	GraphQLMutation  string `json:"graphql_mutation,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

type CardDetail struct {
	ID       string `json:"id"`
	CardID   string `json:"card_id"`
	Section  string `json:"section"`
	Content  string `json:"content,omitempty"`
	DataJSON string `json:"data_json,omitempty"`
}

type Node struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Summary string `json:"summary,omitempty"`
	CardID  string `json:"card_id,omitempty"`
}

type Edge struct {
	ID      string `json:"id"`
	FromID  string `json:"from_id"`
	ToID    string `json:"to_id"`
	Kind    string `json:"kind"`
	Summary string `json:"summary,omitempty"`
}

type EntryPoint struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Summary       string `json:"summary"`
	QueryJSON     string `json:"query_json,omitempty"`
	SuggestedNext string `json:"suggested_next_json,omitempty"`
}

type Capability struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Kind             string `json:"kind"`
	Summary          string `json:"summary"`
	InputSchemaJSON  string `json:"input_schema_json,omitempty"`
	OutputSchemaJSON string `json:"output_schema_json,omitempty"`
	SafetyJSON       string `json:"safety_json,omitempty"`
}

type MetadataSnapshot struct {
	Databases     []MetadataDatabase
	Tables        []MetadataTable
	Columns       []MetadataColumn
	Relationships []MetadataRelationship
	Functions     []MetadataFunction
	Indexes       []MetadataIndex
}

type MetadataDatabase struct {
	ID        string
	Name      string
	Type      string
	IsDefault bool
	ReadOnly  bool
}

type MetadataTable struct {
	ID           string
	DatabaseName string
	SchemaName   string
	TableName    string
	Type         string
	Comment      string
	PrimaryKey   string
	ColumnCount  int
	TableKey     string
}

type MetadataColumn struct {
	ID           string
	TableID      string
	DatabaseName string
	SchemaName   string
	TableName    string
	ColumnName   string
	Type         string
	Array        bool
	NotNull      bool
	PrimaryKey   bool
	UniqueKey    bool
	Indexed      bool
	IndexName    string
	DefaultValue string
	Comment      string
	Ordinal      int
	TableKey     string
	ColumnKey    string
}

type MetadataRelationship struct {
	ID               string
	FromDatabaseName string
	FromSchemaName   string
	FromTableName    string
	FromColumnName   string
	FromColumnID     string
	ToDatabaseName   string
	ToSchemaName     string
	ToTableName      string
	ToColumnName     string
	ToColumnID       string
	RelType          string
	IsCrossDatabase  bool
	Source           string
}

type MetadataFunction struct {
	ID           string
	DatabaseName string
	SchemaName   string
	Name         string
	ReturnType   string
	Aggregate    bool
	Comment      string
}

type MetadataIndex struct {
	ID           string
	DatabaseName string
	SchemaName   string
	TableName    string
	ColumnName   string
	Name         string
	Unique       bool
}
