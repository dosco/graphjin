package sdata

import (
	"strings"
)

func GetTestDBInfo() *DBInfo {
	tables := []DBTable{
		DBTable{Name: "customers", Type: "table"},
		DBTable{Name: "users", Type: "table"},
		DBTable{Name: "products", Type: "table"},
		DBTable{Name: "purchases", Type: "table"},
		DBTable{Name: "tags", Type: "table"},
		DBTable{Name: "tag_count", Type: "json"},
		DBTable{Name: "notifications", Type: "table"},
		DBTable{Name: "comments", Type: "table"},
	}

	columns := [][]DBColumn{
		[]DBColumn{
			DBColumn{Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "avatar", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Name: "name", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "description", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "price", Type: "numeric(7,2)", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "user_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "users", FKeyCol: "id"},
			DBColumn{Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "tsv", Type: "tsvector", NotNull: false, PrimaryKey: false, UniqueKey: false, FullText: true},
			DBColumn{Name: "tags", Type: "text[]", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "tags", FKeyCol: "slug", Array: true},
			DBColumn{Name: "tag_count", Type: "json", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "tag_count", FKeyCol: ""}},
		[]DBColumn{
			DBColumn{Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Name: "customer_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "customers", FKeyCol: "id"},
			DBColumn{Name: "product_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "products", FKeyCol: "id"},
			DBColumn{Name: "sale_type", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "quantity", Type: "integer", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "due_date", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "returned", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Name: "name", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "slug", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Name: "tag_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "tags", FKeyCol: "id"},
			DBColumn{Name: "count", Type: "int", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Name: "verb", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "subject_type", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Name: "subject_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Name: "product_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "products", FKeyCol: "id"},
			DBColumn{Name: "commenter_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "users", FKeyCol: "id"},
			DBColumn{Name: "reply_to_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "comments", FKeyCol: "id"},
			DBColumn{Name: "body", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false}},
	}

	vTables := []VirtualTable{{
		Name:       "subject",
		IDColumn:   "subject_id",
		TypeColumn: "subject_type",
		FKeyColumn: "id"},
	}

	for i := range tables {
		tables[i].Key = strings.ToLower(tables[i].Name)
		for n := range columns[i] {
			columns[i][n].Key = strings.ToLower(columns[i][n].Name)
		}
	}

	return &DBInfo{
		Version:   110000,
		Tables:    tables,
		Columns:   columns,
		Functions: []DBFunction{},
		VTables:   vTables,
		colMap:    newColMap(tables, columns),
	}
}

func GetTestSchema() (*DBSchema, error) {
	aliases := map[string][]string{
		"users": {"mes"},
	}

	return NewDBSchema(GetTestDBInfo(), aliases)
}

func newColMap(tables []DBTable, columns [][]DBColumn) map[string]*DBColumn {
	cm := make(map[string]*DBColumn, len(tables))

	for i, t := range tables {
		for n, c := range columns[i] {
			cm[(t.Name + c.Name)] = &columns[i][n]
		}
	}

	return cm
}
