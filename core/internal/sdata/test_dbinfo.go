package sdata

func GetTestDBInfo() *DBInfo {
	columns := [][]DBColumn{
		[]DBColumn{
			DBColumn{Schema: "public", Table: "customers", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Schema: "public", Table: "customers", Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "customers", Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "customers", Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "customers", Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "customers", Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "customers", Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "customers", Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "customers", Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "customers", Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Schema: "public", Table: "users", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Schema: "public", Table: "users", Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "users", Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "users", Name: "avatar", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "users", Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "users", Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "users", Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "users", Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "users", Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "users", Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "users", Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Schema: "public", Table: "products", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Schema: "public", Table: "products", Name: "name", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "products", Name: "description", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "products", Name: "price", Type: "numeric(7,2)", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "products", Name: "user_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "users", FKeyCol: "id"},
			DBColumn{Schema: "public", Table: "products", Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "products", Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "products", Name: "tsv", Type: "tsvector", NotNull: false, PrimaryKey: false, UniqueKey: false, FullText: true},
			DBColumn{Schema: "public", Table: "products", Name: "tags", Type: "text[]", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "tags", FKeyCol: "slug", Array: true},
			DBColumn{Schema: "public", Table: "products", Name: "tag_count", Type: "json", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "tag_count", FKeyCol: ""}},
		[]DBColumn{
			DBColumn{Schema: "public", Table: "purchases", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Schema: "public", Table: "purchases", Name: "customer_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "customers", FKeyCol: "id"},
			DBColumn{Schema: "public", Table: "purchases", Name: "product_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "products", FKeyCol: "id"},
			DBColumn{Schema: "public", Table: "purchases", Name: "sale_type", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "purchases", Name: "quantity", Type: "integer", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "purchases", Name: "due_date", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "purchases", Name: "returned", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Schema: "public", Table: "tags", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Schema: "public", Table: "tags", Name: "name", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "tags", Name: "slug", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Schema: "public", Table: "tag_count", Name: "tag_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "tags", FKeyCol: "id"},
			DBColumn{Schema: "public", Table: "tag_count", Name: "count", Type: "int", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Schema: "public", Table: "notifications", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Schema: "public", Table: "notifications", Name: "verb", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "notifications", Name: "subject_type", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			DBColumn{Schema: "public", Table: "notifications", Name: "subject_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		[]DBColumn{
			DBColumn{Schema: "public", Table: "comments", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			DBColumn{Schema: "public", Table: "comments", Name: "product_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "products", FKeyCol: "id"},
			DBColumn{Schema: "public", Table: "comments", Name: "commenter_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "users", FKeyCol: "id"},
			DBColumn{Schema: "public", Table: "comments", Name: "reply_to_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "comments", FKeyCol: "id"},
			DBColumn{Schema: "public", Table: "comments", Name: "body", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false}},
	}

	var cols []DBColumn

	for _, colset := range columns {
		cols = append(cols, colset...)
	}

	vt := []VirtualTable{{
		Name:       "subject",
		IDColumn:   "subject_id",
		TypeColumn: "subject_type",
		FKeyColumn: "id"},
	}

	di := NewDBInfo("", 110000, "public", "db", cols, nil, nil)
	di.VTables = vt
	return di
}

func GetTestSchema() (*DBSchema, error) {
	aliases := map[string][]string{
		"users": {"me"},
	}

	return NewDBSchema(GetTestDBInfo(), aliases)
}
