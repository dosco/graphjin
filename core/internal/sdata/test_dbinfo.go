package sdata

func GetTestDBInfo() *DBInfo {
	columns := [][]DBColumn{
		{
			{Schema: "public", Table: "customers", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			{Schema: "public", Table: "customers", Name: "user_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "users", FKeyCol: "id"},
			{Schema: "public", Table: "customers", Name: "product_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "products", FKeyCol: "id"},
			{Schema: "public", Table: "customers", Name: "vip", Type: "boolean", NotNull: true, PrimaryKey: false, UniqueKey: false}},
		{
			{Schema: "public", Table: "users", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			{Schema: "public", Table: "users", Name: "full_name", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "users", Name: "phone", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "users", Name: "avatar", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "users", Name: "email", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "users", Name: "encrypted_password", Type: "character varying", NotNull: true, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "users", Name: "reset_password_token", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "users", Name: "reset_password_sent_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "users", Name: "remember_created_at", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "users", Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "users", Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false}},
		{
			{Schema: "public", Table: "products", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			{Schema: "public", Table: "products", Name: "name", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "products", Name: "description", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "products", Name: "price", Type: "numeric(7,2)", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "products", Name: "user_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "users", FKeyCol: "id"},
			{Schema: "public", Table: "products", Name: "created_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "products", Name: "updated_at", Type: "timestamp without time zone", NotNull: true, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "products", Name: "tsv", Type: "tsvector", NotNull: false, PrimaryKey: false, UniqueKey: false, FullText: true},
			{Schema: "public", Table: "products", Name: "tags", Type: "text[]", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "tags", FKeyCol: "slug", Array: true},
			{Schema: "public", Table: "products", Name: "tag_count", Type: "json", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "tag_count", FKeyCol: ""}},
		{
			{Schema: "public", Table: "purchases", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			{Schema: "public", Table: "purchases", Name: "customer_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "customers", FKeyCol: "id"},
			{Schema: "public", Table: "purchases", Name: "product_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "products", FKeyCol: "id"},
			{Schema: "public", Table: "purchases", Name: "sale_type", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "purchases", Name: "quantity", Type: "integer", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "purchases", Name: "due_date", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "purchases", Name: "returned", Type: "timestamp without time zone", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		{
			{Schema: "public", Table: "tags", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			{Schema: "public", Table: "tags", Name: "name", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "tags", Name: "slug", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		{
			{Schema: "public", Table: "tag_count", Name: "tag_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "tags", FKeyCol: "id"},
			{Schema: "public", Table: "tag_count", Name: "count", Type: "int", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		{
			{Schema: "public", Table: "notifications", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			{Schema: "public", Table: "notifications", Name: "verb", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "notifications", Name: "subject_type", Type: "text", NotNull: false, PrimaryKey: false, UniqueKey: false},
			{Schema: "public", Table: "notifications", Name: "subject_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false}},
		{
			{Schema: "public", Table: "comments", Name: "id", Type: "bigint", NotNull: true, PrimaryKey: true, UniqueKey: true},
			{Schema: "public", Table: "comments", Name: "product_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "products", FKeyCol: "id"},
			{Schema: "public", Table: "comments", Name: "commenter_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "users", FKeyCol: "id"},
			{Schema: "public", Table: "comments", Name: "reply_to_id", Type: "bigint", NotNull: false, PrimaryKey: false, UniqueKey: false, FKeySchema: "public", FKeyTable: "comments", FKeyCol: "id", FKRecursive: true},
			{Schema: "public", Table: "comments", Name: "body", Type: "character varying", NotNull: false, PrimaryKey: false, UniqueKey: false}},
	}

	fn := []DBFunction{
		{
			Schema: "public",
			Name:   "get_top_products",
			Type:   "record",
			Agg:    false,
			Inputs: []DBFuncParam{
				{ID: 1, Name: "n", Type: "integer", Array: false}},
			Outputs: []DBFuncParam{
				{ID: 2, Name: "id", Type: "bigint", Array: false},
				{ID: 3, Name: "name", Type: "bigint", Array: false}},
		},
		{
			Schema: "public",
			Name:   "text2score",
			Type:   "numeric",
			Agg:    false,
			Inputs: []DBFuncParam{
				{ID: 1, Name: "text", Type: "text", Array: false}},
			Outputs: []DBFuncParam{
				{ID: 2, Name: "score", Type: "bigint", Array: false}},
		},
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
	di.Functions = fn
	return di
}

func GetTestSchema() (*DBSchema, error) {
	aliases := map[string][]string{
		"users": {"me"},
	}

	return NewDBSchema(GetTestDBInfo(), aliases)
}
