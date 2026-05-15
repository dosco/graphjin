package core

import "github.com/dosco/graphjin/core/v3/internal/catalog"

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
	snap := catalog.Build(catalogMetadataSnapshot(md), gj.conf)
	out := CatalogSnapshot(*snap)
	return &out, nil
}

// BuildCatalogSnapshot builds a catalog from an existing metadata snapshot.
// Service code uses this while refreshing the managed catalog database so schema
// metadata and catalog rows are produced from the same point-in-time snapshot.
func BuildCatalogSnapshot(md *MetadataSnapshot, conf *Config) *CatalogSnapshot {
	return BuildCatalogSnapshotWithOptions(md, conf, CatalogBuildOptions{})
}

func BuildCatalogSnapshotWithOptions(md *MetadataSnapshot, conf *Config, opts CatalogBuildOptions) *CatalogSnapshot {
	snap := catalog.BuildWithOptions(catalogMetadataSnapshot(md), conf, opts)
	out := CatalogSnapshot(*snap)
	return &out
}

func CatalogSourceRevisions(md *MetadataSnapshot, conf *Config, opts CatalogBuildOptions) map[string]string {
	return catalog.SourceRevisions(catalogMetadataSnapshot(md), conf, opts)
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
