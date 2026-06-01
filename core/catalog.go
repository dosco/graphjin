package core

import (
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/catalog"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

type CatalogSnapshot catalog.Snapshot
type CatalogCard = catalog.Card
type CatalogCardDetail = catalog.CardDetail
type CatalogNode = catalog.Node
type CatalogEdge = catalog.Edge
type CatalogEntryPoint = catalog.EntryPoint
type CatalogCapability = catalog.Capability
type CatalogFeature = catalog.Feature
type CatalogFeatureArg = catalog.FeatureArg
type CatalogQuery = catalog.Query
type CatalogQueryOutput = catalog.QueryResult
type CatalogMatch = catalog.Match
type CatalogBuildOptions = catalog.BuildOptions
type CatalogWorkflow = catalog.Workflow
type CatalogWorkflowVariable = catalog.WorkflowVariable
type CatalogFragment = catalog.Fragment
type CatalogSavedQuery = catalog.SavedQuery
type CatalogSource = catalog.Source

// LanguageFeatures returns the structured GraphJin language registry used by
// the AI catalog, MCP guidance, and drift tests.
func LanguageFeatures() []CatalogFeature {
	return catalog.LanguageFeatures()
}

// CatalogSnapshot returns the AI-first self-catalog for this GraphJin engine.
// It is intentionally read-only and is safe to expose through MCP/admin
// adapters after those adapters apply their own auth and availability rules.
func (g *GraphJin) CatalogSnapshot(exclude ...string) (*CatalogSnapshot, error) {
	gj, err := g.getEngine()
	if err != nil {
		return nil, err
	}
	skip := make(map[string]struct{}, len(exclude))
	for _, name := range exclude {
		if name != "" {
			skip[name] = struct{}{}
		}
	}
	md := gj.metadataSnapshot(skip)
	opts := CatalogBuildOptions{
		Fragments:    g.catalogFragments(),
		SavedQueries: g.catalogSavedQueries(),
	}
	opts = catalogBuildOptionsFromConfig(gj.conf, opts)
	snap := catalog.BuildWithOptions(catalogMetadataSnapshot(md), gj.conf, opts)
	out := CatalogSnapshot(*snap)
	return &out, nil
}

func (g *GraphJin) catalogFragments() []CatalogFragment {
	fragments, err := g.ListFragments()
	if err != nil {
		return nil
	}
	out := make([]CatalogFragment, 0, len(fragments))
	for _, fragment := range fragments {
		details, err := g.GetFragment(qualifiedFragmentName(fragment.Namespace, fragment.Name))
		if err != nil {
			continue
		}
		out = append(out, CatalogFragment{
			Name:       details.Name,
			Namespace:  details.Namespace,
			Definition: details.Definition,
			On:         details.On,
		})
	}
	return out
}

func (g *GraphJin) catalogSavedQueries() []CatalogSavedQuery {
	queries, err := g.ListSavedQueries()
	if err != nil {
		return nil
	}
	out := make([]CatalogSavedQuery, 0, len(queries))
	for _, query := range queries {
		details, err := g.GetSavedQuery(qualifiedFragmentName(query.Namespace, query.Name))
		if err != nil {
			continue
		}
		out = append(out, CatalogSavedQuery{
			Name:      details.Name,
			Namespace: details.Namespace,
			Operation: details.Operation,
			Query:     details.Query,
			Variables: details.Variables,
		})
	}
	return out
}

func qualifiedFragmentName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "." + name
}

// BuildCatalogSnapshot builds a catalog from an existing metadata snapshot.
// Service code uses this while refreshing the managed catalog database so schema
// metadata and catalog rows are produced from the same point-in-time snapshot.
func BuildCatalogSnapshot(md *MetadataSnapshot, conf *Config) *CatalogSnapshot {
	return BuildCatalogSnapshotWithOptions(md, conf, CatalogBuildOptions{})
}

func BuildCatalogSnapshotWithOptions(md *MetadataSnapshot, conf *Config, opts CatalogBuildOptions) *CatalogSnapshot {
	opts = catalogBuildOptionsFromConfig(conf, opts)
	snap := catalog.BuildWithOptions(catalogMetadataSnapshot(catalogVisibleMetadataSnapshot(md, conf)), conf, opts)
	out := CatalogSnapshot(*snap)
	return &out
}

func CatalogSourceRevisions(md *MetadataSnapshot, conf *Config, opts CatalogBuildOptions) map[string]string {
	opts = catalogBuildOptionsFromConfig(conf, opts)
	return catalog.SourceRevisions(catalogMetadataSnapshot(catalogVisibleMetadataSnapshot(md, conf)), conf, opts)
}

func catalogBuildOptionsFromConfig(conf *Config, opts CatalogBuildOptions) CatalogBuildOptions {
	if conf != nil && len(opts.Sources) == 0 {
		opts.Sources = catalogSourcesFromConfig(conf)
	}
	return opts
}

func catalogSourcesFromConfig(conf *Config) []CatalogSource {
	if conf == nil {
		return nil
	}
	out := make([]CatalogSource, 0, len(conf.Sources))
	for _, source := range conf.Sources {
		kind := source.CanonicalKind()
		if kind == "" {
			continue
		}
		name := strings.TrimSpace(source.Name)
		if name == "" {
			name = kind
		}
		capabilities := make(map[string]bool, len(source.Capabilities))
		for key, value := range source.Capabilities {
			capabilities[key] = value
		}
		if len(capabilities) == 0 {
			capabilities = nil
		}
		out = append(out, CatalogSource{
			Name:         name,
			Kind:         kind,
			Type:         strings.TrimSpace(source.Type),
			Default:      source.Default,
			ReadOnly:     source.ReadOnly,
			Capabilities: capabilities,
		})
	}
	return out
}

func catalogVisibleMetadataSnapshot(md *MetadataSnapshot, conf *Config) *MetadataSnapshot {
	if md == nil || conf == nil || !conf.IsSourcesUsed() {
		return md
	}
	hiddenTables := make(map[string]struct{})
	for _, source := range conf.Sources {
		if source.CanonicalKind() != sourcecap.KindDatabase {
			continue
		}
		access := conf.EffectiveSourceAccess(source)
		for _, table := range md.Tables {
			if !strings.EqualFold(table.DatabaseName, source.Name) {
				continue
			}
			if catalogSourceAccessTableListed(access.AdminTables, table) ||
				catalogSourceAccessTableListed(access.BlockedTables, table) {
				hiddenTables[catalogMetadataTableKey(table.DatabaseName, table.SchemaName, table.TableName)] = struct{}{}
			}
		}
	}
	if len(hiddenTables) == 0 {
		return md
	}

	out := &MetadataSnapshot{}
	out.Databases = append(out.Databases, md.Databases...)
	for _, table := range md.Tables {
		if _, hidden := hiddenTables[catalogMetadataTableKey(table.DatabaseName, table.SchemaName, table.TableName)]; !hidden {
			out.Tables = append(out.Tables, table)
		}
	}
	for _, col := range md.Columns {
		if _, hidden := hiddenTables[catalogMetadataTableKey(col.DatabaseName, col.SchemaName, col.TableName)]; !hidden {
			out.Columns = append(out.Columns, col)
		}
	}
	for _, rel := range md.Relationships {
		fromHidden := catalogMetadataTableHidden(hiddenTables, rel.FromDatabaseName, rel.FromSchemaName, rel.FromTableName)
		toHidden := catalogMetadataTableHidden(hiddenTables, rel.ToDatabaseName, rel.ToSchemaName, rel.ToTableName)
		if !fromHidden && !toHidden {
			out.Relationships = append(out.Relationships, rel)
		}
	}
	for _, fn := range md.Functions {
		if _, hidden := hiddenTables[catalogMetadataTableKey(fn.DatabaseName, fn.SchemaName, fn.Name)]; !hidden {
			out.Functions = append(out.Functions, fn)
		}
	}
	for _, idx := range md.Indexes {
		if _, hidden := hiddenTables[catalogMetadataTableKey(idx.DatabaseName, idx.SchemaName, idx.TableName)]; !hidden {
			out.Indexes = append(out.Indexes, idx)
		}
	}
	return out
}

func catalogMetadataTableHidden(hidden map[string]struct{}, database, schema, table string) bool {
	_, ok := hidden[catalogMetadataTableKey(database, schema, table)]
	return ok
}

func catalogMetadataTableKey(database, schema, table string) string {
	return strings.ToLower(strings.TrimSpace(database)) + ":" +
		strings.ToLower(strings.TrimSpace(schema)) + ":" +
		strings.ToLower(strings.TrimSpace(table))
}

func catalogSourceAccessTableListed(list []string, table MetadataTable) bool {
	for _, item := range list {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(table.TableName))
		schemaName := strings.ToLower(strings.TrimSpace(table.SchemaName))
		databaseName := strings.ToLower(strings.TrimSpace(table.DatabaseName))
		if item == name ||
			item == schemaName+"."+name ||
			item == databaseName+":"+name ||
			item == databaseName+":"+schemaName+"."+name {
			return true
		}
	}
	return false
}

func CatalogRevisionFromSourceRevisions(source map[string]string) string {
	return catalog.RevisionFromSourceRevisions(source)
}

func (s *CatalogSnapshot) Query(q CatalogQuery) []CatalogCard {
	result, err := s.QueryResult(q)
	if err != nil {
		return nil
	}
	return result.Cards
}

func (s *CatalogSnapshot) QueryResult(q CatalogQuery) (CatalogQueryOutput, error) {
	if s == nil {
		return CatalogQueryOutput{}, nil
	}
	return (*catalog.Snapshot)(s).Query(q)
}

func (s *CatalogSnapshot) Card(id string) (CatalogCard, bool) {
	if s == nil {
		return CatalogCard{}, false
	}
	for _, card := range s.Cards {
		if card.ID == id {
			return card, true
		}
	}
	return CatalogCard{}, false
}

func (s *CatalogSnapshot) CardDetails(cardID string) []CatalogCardDetail {
	if s == nil {
		return nil
	}
	var out []CatalogCardDetail
	for _, detail := range s.Details {
		if detail.CardID == cardID {
			out = append(out, detail)
		}
	}
	return out
}

func (s *CatalogSnapshot) CardEdges(cardID string) []CatalogEdge {
	if s == nil {
		return nil
	}
	nodeIDs := map[string]struct{}{}
	for _, node := range s.Nodes {
		if node.CardID == cardID {
			nodeIDs[node.ID] = struct{}{}
		}
	}
	var out []CatalogEdge
	for _, edge := range s.Edges {
		_, from := nodeIDs[edge.FromID]
		_, to := nodeIDs[edge.ToID]
		if from || to {
			out = append(out, edge)
		}
	}
	return out
}

func catalogMetadataSnapshot(md *MetadataSnapshot) *catalog.MetadataSnapshot {
	if md == nil {
		return &catalog.MetadataSnapshot{}
	}
	out := &catalog.MetadataSnapshot{
		Databases:     make([]catalog.MetadataDatabase, len(md.Databases)),
		Tables:        make([]catalog.MetadataTable, len(md.Tables)),
		Columns:       make([]catalog.MetadataColumn, len(md.Columns)),
		Relationships: make([]catalog.MetadataRelationship, len(md.Relationships)),
		Functions:     make([]catalog.MetadataFunction, len(md.Functions)),
		Indexes:       make([]catalog.MetadataIndex, len(md.Indexes)),
	}
	for i, v := range md.Databases {
		out.Databases[i] = catalog.MetadataDatabase(v)
	}
	for i, v := range md.Tables {
		out.Tables[i] = catalog.MetadataTable(v)
	}
	for i, v := range md.Columns {
		out.Columns[i] = catalog.MetadataColumn(v)
	}
	for i, v := range md.Relationships {
		out.Relationships[i] = catalog.MetadataRelationship(v)
	}
	for i, v := range md.Functions {
		out.Functions[i] = catalog.MetadataFunction(v)
	}
	for i, v := range md.Indexes {
		out.Indexes[i] = catalog.MetadataIndex(v)
	}
	return out
}
