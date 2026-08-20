package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

type MetadataSnapshot struct {
	Databases     []MetadataDatabase
	Tables        []MetadataTable
	Columns       []MetadataColumn
	Relationships []MetadataRelationship
	Functions     []MetadataFunction
	Indexes       []MetadataIndex
	APIOperations []MetadataAPIOperation
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

// MetadataAPIOperation is the catalog-safe projection of one classified
// OpenAPI operation. It contains no authentication configuration or request
// values and is safe to expose after caller-specific authorization filtering.
type MetadataAPIOperation struct {
	ID                 string
	SourceName         string
	SpecKey            string
	OperationID        string
	RootName           string
	Method             string
	Path               string
	Mode               string
	Active             bool
	SkipReason         string
	Capability         string
	AllowedRoles       []string
	RequestMediaType   string
	RequestSchemaJSON  string
	ResponseSchemaJSON string
	SuccessStatuses    []int
	RetryEnabled       bool
	RiskLevel          string
}

// MetadataSnapshot returns a stable, queryable projection of the schemas that
// GraphJin discovered. Excluded database names are skipped, which the service
// uses to keep managed metadata and CodeSQL cache databases out of gj_* rows.
func (g *GraphJin) MetadataSnapshot(exclude ...string) (*MetadataSnapshot, error) {
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
	return gj.metadataSnapshot(skip), nil
}

func (gj *graphjinEngine) metadataSnapshot(skip map[string]struct{}) *MetadataSnapshot {
	out := &MetadataSnapshot{}
	for _, dbName := range gj.sortedDatabaseNames() {
		if _, ok := skip[dbName]; ok {
			continue
		}
		ctx := gj.databases[dbName]
		if ctx == nil || ctx.schema == nil {
			continue
		}
		readOnly := false
		if dbConf, ok := gj.conf.Databases[dbName]; ok {
			readOnly = dbConf.ReadOnly
		}
		out.Databases = append(out.Databases, MetadataDatabase{
			ID:        dbName,
			Name:      dbName,
			Type:      ctx.dbtype,
			IsDefault: dbName == gj.defaultDB,
			ReadOnly:  readOnly,
		})

		for _, t := range ctx.schema.GetTables() {
			if t.Blocked || t.Type == "managed" || strings.HasPrefix(strings.ToLower(t.Name), "gj_") {
				continue
			}
			columns := append([]sdata.DBColumn(nil), t.Columns...)
			sort.SliceStable(columns, func(i, j int) bool {
				if columns[i].ID != columns[j].ID {
					return columns[i].ID < columns[j].ID
				}
				return columns[i].Name < columns[j].Name
			})
			columnNames := make(map[string]struct{}, len(columns))
			for _, column := range columns {
				columnNames[column.Name] = struct{}{}
			}
			primaryKeys := make([]string, 0, len(t.PrimaryCols))
			for _, column := range columns {
				if column.PrimaryKey {
					primaryKeys = append(primaryKeys, column.Name)
				}
			}
			tableID := metadataTableID(dbName, t.Schema, t.Name)
			tableKey := tableID
			out.Tables = append(out.Tables, MetadataTable{
				ID:           tableID,
				DatabaseName: dbName,
				SchemaName:   t.Schema,
				TableName:    t.Name,
				Type:         t.Type,
				Comment:      t.Comment,
				PrimaryKey:   strings.Join(primaryKeys, ","),
				ColumnCount:  len(columns),
				TableKey:     tableKey,
			})
			for i, c := range columns {
				colID := metadataColumnID(dbName, c.Schema, c.Table, c.Name)
				colKey := colID
				out.Columns = append(out.Columns, MetadataColumn{
					ID:           colID,
					TableID:      tableID,
					DatabaseName: dbName,
					SchemaName:   c.Schema,
					TableName:    c.Table,
					ColumnName:   c.Name,
					Type:         c.Type,
					Array:        c.Array,
					NotNull:      c.NotNull,
					PrimaryKey:   c.PrimaryKey,
					UniqueKey:    c.UniqueKey,
					Indexed:      c.Index,
					IndexName:    c.IndexName,
					DefaultValue: c.Default,
					Comment:      c.Comment,
					Ordinal:      i,
					TableKey:     tableKey,
					ColumnKey:    colKey,
				})
				if c.Index || c.IndexName != "" || c.UniqueKey {
					name := c.IndexName
					if name == "" {
						name = fmt.Sprintf("%s_%s_idx", c.Table, c.Name)
					}
					out.Indexes = append(out.Indexes, MetadataIndex{
						ID:           metadataIndexID(dbName, c.Schema, c.Table, c.Name, name),
						DatabaseName: dbName,
						SchemaName:   c.Schema,
						TableName:    c.Table,
						ColumnName:   c.Name,
						Name:         name,
						Unique:       c.UniqueKey,
					})
				}
				if c.FKeyTable != "" && c.FKeyCol != "" {
					targetDB := dbName
					if c.FKeyDatabase != "" {
						targetDB = c.FKeyDatabase
					}
					if _, ok := skip[targetDB]; ok {
						continue
					}
					targetSchema := c.FKeySchema
					if targetSchema == "" {
						targetSchema = t.Schema
					}
					targetID := metadataColumnID(targetDB, targetSchema, c.FKeyTable, c.FKeyCol)
					relID := colID + "->" + targetID
					relType := "one_to_many"
					if c.FKeyIsUnique || c.UniqueKey {
						relType = "one_to_one"
					}
					out.Relationships = append(out.Relationships, MetadataRelationship{
						ID:               relID,
						FromDatabaseName: dbName,
						FromSchemaName:   c.Schema,
						FromTableName:    c.Table,
						FromColumnName:   c.Name,
						FromColumnID:     colID,
						ToDatabaseName:   targetDB,
						ToSchemaName:     targetSchema,
						ToTableName:      c.FKeyTable,
						ToColumnName:     c.FKeyCol,
						ToColumnID:       targetID,
						RelType:          relType,
						IsCrossDatabase:  targetDB != dbName,
						Source:           "foreign_key",
					})
				}
			}
			// An API-join table's link to its parent lives on a synthetic key column
			// that is deliberately absent from the selectable column list. Relationships
			// were derived only from that list, so the join was never published: the
			// catalog described account_health as a standalone table and every
			// cross-source episode queried it top-level with an invented filter, when
			// the only way to reach it is nested under accounts. The edge is emitted
			// here without adding the internal column to the published surface.
			for _, c := range t.PrimaryCols {
				if c.FKeyTable == "" || c.FKeyCol == "" {
					continue
				}
				if _, listed := columnNames[c.Name]; listed {
					continue
				}
				targetDB := dbName
				if c.FKeyDatabase != "" {
					targetDB = c.FKeyDatabase
				}
				if _, ok := skip[targetDB]; ok {
					continue
				}
				targetSchema := c.FKeySchema
				if targetSchema == "" {
					targetSchema = t.Schema
				}
				colID := metadataColumnID(dbName, c.Schema, c.Table, c.Name)
				targetID := metadataColumnID(targetDB, targetSchema, c.FKeyTable, c.FKeyCol)
				out.Relationships = append(out.Relationships, MetadataRelationship{
					ID:               colID + "->" + targetID,
					FromDatabaseName: dbName,
					FromSchemaName:   c.Schema,
					FromTableName:    c.Table,
					FromColumnName:   c.Name,
					FromColumnID:     colID,
					ToDatabaseName:   targetDB,
					ToSchemaName:     targetSchema,
					ToTableName:      c.FKeyTable,
					ToColumnName:     c.FKeyCol,
					ToColumnID:       targetID,
					RelType:          "one_to_one",
					IsCrossDatabase:  targetDB != dbName,
					Source:           "remote_join",
				})
			}
		}
		for _, fn := range ctx.schema.GetFunctions() {
			out.Functions = append(out.Functions, MetadataFunction{
				ID:           metadataFunctionID(dbName, fn.Schema, fn.Name),
				DatabaseName: dbName,
				SchemaName:   fn.Schema,
				Name:         fn.Name,
				ReturnType:   fn.Type,
				Aggregate:    fn.Agg,
				Comment:      fn.Comment,
			})
		}
	}
	if gj.openapiRuntime != nil {
		for _, specRuntime := range gj.openapiRuntime.AllSpecs() {
			if specRuntime == nil || specRuntime.Spec() == nil {
				continue
			}
			for i := range specRuntime.Spec().Operations {
				op := &specRuntime.Spec().Operations[i]
				capability := "api.read"
				risk := "low"
				switch strings.ToUpper(op.Method) {
				case "DELETE":
					capability, risk = "api.delete", "critical"
				case "POST", "PUT", "PATCH":
					capability, risk = "api.write", "high"
				}
				out.APIOperations = append(out.APIOperations, MetadataAPIOperation{
					ID:                 op.SourceName + ":" + op.SpecKey + ":" + op.OperationID,
					SourceName:         op.SourceName,
					SpecKey:            op.SpecKey,
					OperationID:        op.OperationID,
					RootName:           op.ExposeAs,
					Method:             op.Method,
					Path:               op.PathTemplate,
					Mode:               op.Mode.String(),
					Active:             op.Mode != 0,
					SkipReason:         op.SkipReason,
					Capability:         capability,
					AllowedRoles:       append([]string(nil), op.AllowedRoles...),
					RequestMediaType:   op.RequestMediaType,
					RequestSchemaJSON:  openAPIRequestSchemaJSON(op.PathParams, op.QueryParams, op.HeaderParams, op.RequestBodyRequired, op.RequestBodySchema),
					ResponseSchemaJSON: openAPISchemaJSON(op.ResponseSchema),
					SuccessStatuses:    append([]int(nil), op.SuccessStatuses...),
					RetryEnabled:       op.RetryOnAuthFailure,
					RiskLevel:          risk,
				})
			}
		}
	}
	dedupeMetadataSnapshot(out)
	return out
}

func openAPIRequestSchemaJSON(path, query, header interface{}, bodyRequired bool, body interface{}) string {
	b, err := json.Marshal(map[string]interface{}{
		"path":          path,
		"query":         query,
		"headers":       header,
		"body_required": bodyRequired,
		"body":          body,
	})
	if err != nil {
		return "{}"
	}
	return string(b)
}

func openAPISchemaJSON(schema interface{}) string {
	if schema == nil {
		return ""
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return ""
	}
	return string(b)
}

func dedupeMetadataSnapshot(out *MetadataSnapshot) {
	out.Tables = uniqueMetadataByID(out.Tables, func(v MetadataTable) string { return v.ID })
	out.Columns = uniqueMetadataByID(out.Columns, func(v MetadataColumn) string { return v.ID })
	out.Relationships = uniqueMetadataByID(out.Relationships, func(v MetadataRelationship) string { return v.ID })
	out.Functions = uniqueMetadataByID(out.Functions, func(v MetadataFunction) string { return v.ID })
	out.Indexes = uniqueMetadataByID(out.Indexes, func(v MetadataIndex) string { return v.ID })
	out.APIOperations = uniqueMetadataByID(out.APIOperations, func(v MetadataAPIOperation) string { return v.ID })

	sort.SliceStable(out.Tables, func(i, j int) bool { return out.Tables[i].ID < out.Tables[j].ID })
	sort.SliceStable(out.Columns, func(i, j int) bool { return out.Columns[i].ID < out.Columns[j].ID })
	sort.SliceStable(out.Relationships, func(i, j int) bool { return out.Relationships[i].ID < out.Relationships[j].ID })
	sort.SliceStable(out.Functions, func(i, j int) bool { return out.Functions[i].ID < out.Functions[j].ID })
	sort.SliceStable(out.Indexes, func(i, j int) bool { return out.Indexes[i].ID < out.Indexes[j].ID })
	sort.SliceStable(out.APIOperations, func(i, j int) bool { return out.APIOperations[i].ID < out.APIOperations[j].ID })
}

func uniqueMetadataByID[T any](items []T, id func(T) string) []T {
	if len(items) < 2 {
		return items
	}
	seen := make(map[string]struct{}, len(items))
	out := items[:0]
	for _, item := range items {
		key := id(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func metadataTableID(database, schema, table string) string {
	return database + ":" + schema + "." + table
}

func metadataColumnID(database, schema, table, column string) string {
	return metadataTableID(database, schema, table) + "." + column
}

func metadataFunctionID(database, schema, name string) string {
	return database + ":" + schema + "." + name
}

func metadataIndexID(database, schema, table, column, name string) string {
	return metadataColumnID(database, schema, table, column) + ":" + name
}
