package core

import (
	"fmt"
	"sort"
	"strings"
)

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
			if t.Blocked {
				continue
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
				PrimaryKey:   strings.Join(t.PKColNames(), ","),
				ColumnCount:  len(t.Columns),
				TableKey:     tableKey,
			})
			for i, c := range t.Columns {
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
	dedupeMetadataSnapshot(out)
	return out
}

func dedupeMetadataSnapshot(out *MetadataSnapshot) {
	out.Tables = uniqueMetadataByID(out.Tables, func(v MetadataTable) string { return v.ID })
	out.Columns = uniqueMetadataByID(out.Columns, func(v MetadataColumn) string { return v.ID })
	out.Relationships = uniqueMetadataByID(out.Relationships, func(v MetadataRelationship) string { return v.ID })
	out.Functions = uniqueMetadataByID(out.Functions, func(v MetadataFunction) string { return v.ID })
	out.Indexes = uniqueMetadataByID(out.Indexes, func(v MetadataIndex) string { return v.ID })

	sort.SliceStable(out.Tables, func(i, j int) bool { return out.Tables[i].ID < out.Tables[j].ID })
	sort.SliceStable(out.Columns, func(i, j int) bool { return out.Columns[i].ID < out.Columns[j].ID })
	sort.SliceStable(out.Relationships, func(i, j int) bool { return out.Relationships[i].ID < out.Relationships[j].ID })
	sort.SliceStable(out.Functions, func(i, j int) bool { return out.Functions[i].ID < out.Functions[j].ID })
	sort.SliceStable(out.Indexes, func(i, j int) bool { return out.Indexes[i].ID < out.Indexes[j].ID })
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
