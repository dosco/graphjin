package psql

import (
	"log"
	"strings"
)

func getTestSchema() *DBSchema {
	tables := []DBTable{
		DBTable{Name: "customers", Type: "table"},
		DBTable{Name: "users", Type: "table"},
		DBTable{Name: "products", Type: "table"},
		DBTable{Name: "purchases", Type: "table"},
		DBTable{Name: "tags", Type: "table"},
		DBTable{Name: "tag_count", Type: "json"},
	}

	columns := [][]DBColumn{
		[]DBColumn{
			DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{ID: 2, Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 3, Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 4, Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 5, Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 6, Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 7, Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 8, Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 9, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 10, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{ID: 2, Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 3, Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 4, Name: "avatar", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 5, Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 6, Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 7, Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 8, Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 9, Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 10, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 11, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{ID: 2, Name: "name", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 3, Name: "description", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 4, Name: "price", Type: "numeric(7,2)", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 5, Name: "user_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "users", FKeyColID: []int16{1}},
			DBColumn{ID: 6, Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 7, Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 8, Name: "tsv", Type: "tsvector", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 9, Name: "tags", Type: "text[]", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "tags", FKeyColID: []int16{3}, Array: true},
			DBColumn{ID: 9, Name: "tag_count", Type: "json", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "tag_count", FKeyColID: []int16{}}},
		[]DBColumn{
			DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{ID: 2, Name: "customer_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "customers", FKeyColID: []int16{1}},
			DBColumn{ID: 3, Name: "product_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "products", FKeyColID: []int16{1}},
			DBColumn{ID: 4, Name: "sale_type", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 5, Name: "quantity", Type: "integer", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 6, Name: "due_date", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 7, Name: "returned", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{ID: 1, Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{ID: 2, Name: "name", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{ID: 3, Name: "slug", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{ID: 1, Name: "tag_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeyTable: "tags", FKeyColID: []int16{1}},
			DBColumn{ID: 2, Name: "count", Type: "int", NotNull: false, PrimaryKey: false, UniqueKey: false}},
	}

	for i := range tables {
		tables[i].Key = strings.ToLower(tables[i].Name)
		for n := range columns[i] {
			columns[i][n].Key = strings.ToLower(columns[i][n].Name)
		}
	}

	schema := &DBSchema{
		ver: 110000,
		t:   make(map[string]*DBTableInfo),
		rm:  make(map[string]map[string]*DBRel),
	}

	aliases := map[string][]string{
		"users": []string{"mes"},
	}

	for i, t := range tables {
		err := schema.addTable(t, columns[i], aliases)
		if err != nil {
			log.Fatal(err)
		}
	}

	for i, t := range tables {
		err := schema.firstDegreeRels(t, columns[i])
		if err != nil {
			log.Fatal(err)
		}
	}

	for i, t := range tables {
		err := schema.secondDegreeRels(t, columns[i])
		if err != nil {
			log.Fatal(err)
		}
	}

	return schema
}
