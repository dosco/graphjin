package core

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/dosco/graphjin/core/v3/internal/allow"
	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// Initializes the graphjin instance with the config
func (gj *graphjinEngine) initConfig() error {
	c := gj.conf

	// Validate configuration before proceeding
	if err := c.Validate(); err != nil {
		return err
	}

	// Normalize databases so the primary DB is always an entry in Databases
	c.NormalizeDatabases()

	tableMap := make(map[string]struct{})

	for _, table := range c.Tables {
		k := table.Schema + table.Name
		if _, ok := tableMap[k]; ok {
			return fmt.Errorf("duplicate table found: %s", table.Name)
		}
		tableMap[k] = struct{}{}
	}

	for k, v := range c.Vars {
		if v == "" || !strings.HasPrefix(v, "sql:") {
			continue
		}
		if n, ok := isASCII(v); !ok {
			return fmt.Errorf("variables: %s: invalid character '%s' at %d",
				k, c.RolesQuery[:n+1], n+1)
		}
	}

	gj.roles = make(map[string]*Role)

	for i, role := range c.Roles {
		k := role.Name
		if _, ok := gj.roles[(role.Name)]; ok {
			return fmt.Errorf("duplicate role found: %s", role.Name)
		}

		role.Match = sanitize(role.Match)
		role.tm = make(map[string]*RoleTable)

		for n, t := range role.Tables {
			role.tm[t.Schema+t.Name] = &role.Tables[n]
		}

		gj.roles[k] = &c.Roles[i]
	}

	// If user role not defined then create it
	if _, ok := gj.roles["user"]; !ok {
		ur := Role{
			Name: "user",
			tm:   make(map[string]*RoleTable),
		}
		gj.roles["user"] = &ur
	}

	// If anon role is not defined then create it
	if _, ok := gj.roles["anon"]; !ok {
		ur := Role{
			Name: "anon",
			tm:   make(map[string]*RoleTable),
		}
		gj.roles["anon"] = &ur
	}

	if c.RolesQuery != "" {
		if n, ok := isASCII(c.RolesQuery); !ok {
			return fmt.Errorf("roles_query: invalid character (%s) at %d",
				c.RolesQuery[:n+1], n+1)
		}

		// More than 2 roles tell us that custom roles have been added
		// hence ABAC is handled
		gj.abacEnabled = (len(gj.roles) > 2)
	}

	return nil
}

// addTableInfo adds table info to the compiler
func (gj *graphjinEngine) addTableInfo(t Table) error {
	obm := map[string][][2]string{}

	for k, ob := range t.OrderBy {
		for _, v := range ob {
			vals := strings.Fields(strings.TrimSpace(v))
			if len(vals) != 2 {
				return fmt.Errorf("invalid format for order by (column sort_order): %s", v)
			}
			obm[k] = append(obm[k], [2]string{vals[0], vals[1]})
		}
	}
	if gj.tmap == nil {
		gj.tmap = make(map[string]qcode.TConfig)
	}
	gj.tmap[(t.Schema + t.Name)] = qcode.TConfig{OrderBy: obm}
	return nil
}

// getDBTableAliases returns a map of table aliases
func getDBTableAliases(c *Config) map[string][]string {
	m := make(map[string][]string, len(c.Tables))

	for i := range c.Tables {
		t := c.Tables[i]

		if t.Table != "" && t.Type == "" {
			m[t.Table] = append(m[t.Table], t.Name)
		}
	}
	return m
}

// addTables adds tables to the database info
// targetDB is the database name to process (after normalization, all tables have Database set)
func addTables(conf *Config, dbInfo *sdata.DBInfo, targetDB string) error {
	var err error

	for _, t := range conf.Tables {
		// After normalization, every table has a Database set.
		// Only process tables matching the target database.
		if t.Database != targetDB {
			continue
		}

		// skip aliases
		if t.Table != "" && t.Type == "" {
			continue
		}
		switch t.Type {
		case "json", "jsonb":
			err = addJsonTable(conf, dbInfo, t)

		case "polymorphic":
			err = addVirtualTable(conf, dbInfo, t)

		default:
			err = updateTable(conf, dbInfo, t)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

// updateTable updates the table info in the database info
func updateTable(conf *Config, dbInfo *sdata.DBInfo, table Table) error {
	// Use dbinfo's default schema if not specified in table config
	schema := table.Schema
	if schema == "" {
		schema = dbInfo.Schema
	}

	t1, err := dbInfo.GetTable(schema, table.Name)
	if err != nil {
		return fmt.Errorf("table: %w", err)
	}

	for _, c := range table.Columns {
		c1, err := dbInfo.GetColumn(schema, table.Name, c.Name)
		if err != nil {
			return err
		}

		if c.Primary {
			c1.PrimaryKey = true
			t1.PrimaryCol = *c1
		}

		if c.Array {
			c1.Array = true
		}
	}

	return nil
}

// addJsonTable adds a json table to the database info
func addJsonTable(conf *Config, dbInfo *sdata.DBInfo, table Table) error {
	// This is for jsonb column that want to be a table.
	if table.Table == "" {
		return fmt.Errorf("json table: set the 'table' for column '%s'", table.Name)
	}

	bc, err := dbInfo.GetColumn(table.Schema, table.Table, table.Name)
	if err != nil {
		return fmt.Errorf("json table: %w", err)
	}

	bt, err := dbInfo.GetTable(bc.Schema, bc.Table)
	if err != nil {
		return fmt.Errorf("json table: %w", err)
	}

	// Allow json, jsonb, clob, and text types
	// MariaDB stores JSON as longtext, so we need to accept text types as well
	validJSONTypes := map[string]bool{
		"json": true, "jsonb": true, "clob": true,
		"longtext": true, "text": true, "mediumtext": true,
	}
	if !validJSONTypes[bc.Type] {
		return fmt.Errorf(
			"json table: column '%s' in table '%s' is of type '%s'. Only JSON, JSONB, CLOB, or TEXT types are valid",
			table.Name, table.Table, bc.Type)
	}

	columns := make([]sdata.DBColumn, 0, len(table.Columns))

	for i := range table.Columns {
		c := table.Columns[i]
		columns = append(columns, sdata.DBColumn{
			ID:     -1,
			Schema: bc.Schema,
			Table:  table.Name,
			Name:   c.Name,
			Type:   c.Type,
		})
		if c.Type == "" {
			return fmt.Errorf("json table: type parameter missing for column: %s.%s'",
				table.Name, c.Name)
		}
	}

	col1 := sdata.DBColumn{
		ID:         bc.ID,
		PrimaryKey: true,
		Schema:     bc.Schema,
		Table:      bc.Table,
		Name:       bc.Name,
		Type:       bc.Type,
	}

	nt := sdata.NewDBTable(bc.Schema, table.Name, bc.Type, columns)
	nt.PrimaryCol = col1
	nt.SecondaryCol = bt.PrimaryCol

	dbInfo.AddTable(nt)
	return nil
}

// addVirtualTable adds a virtual table to the database info
func addVirtualTable(conf *Config, di *sdata.DBInfo, t Table) error {
	if len(t.Columns) == 0 {
		return fmt.Errorf("polymorphic table: no id column specified")
	}
	c := t.Columns[0]

	if c.ForeignKey == "" {
		return fmt.Errorf("polymorphic table: no 'related_to' specified on id column")
	}

	fk, ok := c.getFK(di.Schema)
	if !ok {
		return fmt.Errorf("polymorphic table: foreign key must be <type column>.<foreign key column>")
	}

	di.VTables = append(di.VTables, sdata.VirtualTable{
		Name:       t.Name,
		IDColumn:   c.Name,
		TypeColumn: fk.Table,
		FKeyColumn: fk.Column,
	})

	return nil
}

// addForeignKeys adds foreign keys to the database info.
// targetDB is the database name to process (after normalization, all tables have Database set).
// allDBInfos provides access to other databases' metadata for cross-database FK resolution.
func addForeignKeys(conf *Config, di *sdata.DBInfo, targetDB string, allDBInfos map[string]*sdata.DBInfo) error {
	for _, t := range conf.Tables {
		// After normalization, every table has a Database set.
		if t.Database != targetDB {
			continue
		}

		if t.Type == "polymorphic" {
			continue
		}
		for _, c := range t.Columns {
			if c.ForeignKey == "" {
				continue
			}
			if err := addForeignKey(conf, di, c, t, allDBInfos); err != nil {
				return err
			}
		}
	}
	return nil
}

// addForeignKey adds a foreign key to the database info.
// allDBInfos is used to resolve cross-database FK references.
func addForeignKey(conf *Config, di *sdata.DBInfo, c Column, t Table, allDBInfos map[string]*sdata.DBInfo) error {
	// Use di.Schema as default if table schema is not specified
	schema := t.Schema
	if schema == "" {
		schema = di.Schema
	}
	c1, err := di.GetColumn(schema, t.Name, c.Name)
	if err != nil {
		return fmt.Errorf("config: add foreign key: %w", err)
	}

	fk, ok := c.getFK(di.Schema)
	if !ok {
		return fmt.Errorf(
			"config: invalid foreign key defined for table '%s' and column '%s': %s",
			t.Name, c.Name, c.ForeignKey)
	}

	// Cross-database FK: resolve against the target database's DBInfo
	if fk.Database != "" {
		targetDI, ok := allDBInfos[fk.Database]
		if !ok {
			return fmt.Errorf(
				"config: foreign key for table '%s' and column '%s' references unknown database '%s'",
				t.Name, c.Name, fk.Database)
		}

		c3, err := targetDI.GetColumn(fk.Schema, fk.Table, fk.Column)
		if err != nil {
			return fmt.Errorf(
				"config: foreign key for table '%s' and column '%s' points to unknown table '%s:%s.%s' column '%s'",
				t.Name, c.Name, fk.Database, fk.Schema, fk.Table, fk.Column)
		}

		c1.FKeyDatabase = fk.Database
		c1.FKeySchema = fk.Schema
		c1.FKeyTable = fk.Table
		c1.FKeyCol = c3.Name
		return nil
	}

	// check if it's a polymorphic foreign key
	if _, err := di.GetColumn(schema, t.Name, fk.Table); err == nil {
		c2, err := di.GetColumn(schema, t.Name, fk.Column)
		if err != nil {
			return fmt.Errorf(
				"config: invalid column '%s' for polymorphic relationship on table '%s' and column '%s'",
				fk.Column, t.Name, c.Name)
		}

		c1.FKeySchema = schema
		c1.FKeyTable = fk.Table
		c1.FKeyCol = c2.Name
		return nil
	}

	c3, err := di.GetColumn(fk.Schema, fk.Table, fk.Column)
	if err != nil {
		return fmt.Errorf(
			"config: foreign key for table '%s' and column '%s' points to unknown table '%s.%s' and column '%s'",
			t.Name, c.Name, fk.Schema, fk.Table, fk.Column)
	}

	c1.FKeySchema = fk.Schema
	c1.FKeyTable = fk.Table
	c1.FKeyCol = c3.Name

	// Check if this is a recursive FK (same table pointing to itself)
	if fk.Schema == schema && fk.Table == t.Name {
		c1.FKRecursive = true
	}

	return nil
}

// addFullTextColumns applies full-text search configuration to database columns
// targetDB is the database name to process (after normalization, all tables have Database set)
func addFullTextColumns(conf *Config, di *sdata.DBInfo, targetDB string) error {
	for _, t := range conf.Tables {
		// After normalization, every table has a Database set.
		if t.Database != targetDB {
			continue
		}
		schema := t.Schema
		if schema == "" {
			schema = di.Schema
		}
		for _, c := range t.Columns {
			if !c.FullText {
				continue
			}
			col, err := di.GetColumn(schema, t.Name, c.Name)
			if err != nil {
				return fmt.Errorf("config: add fulltext column: %w", err)
			}
			col.FullText = true

			// Also add to table's FullText slice
			table, err := di.GetTable(schema, t.Name)
			if err != nil {
				return fmt.Errorf("config: add fulltext column: %w", err)
			}
			table.FullText = append(table.FullText, *col)
		}
	}
	return nil
}

// addFunctions updates function configurations in the database info
func addFunctions(conf *Config, di *sdata.DBInfo) error {
	for _, f := range conf.Functions {
		for i := range di.Functions {
			if di.Functions[i].Name == f.Name && (f.Schema == "" || di.Functions[i].Schema == f.Schema) {
				if f.ReturnType != "" {
					di.Functions[i].Type = f.ReturnType
				}
			}
		}
	}
	return nil
}

// addRoles adds roles to the compiler
func addRoles(c *Config, qc *qcode.Compiler) error {
	for _, r := range c.Roles {
		for _, t := range r.Tables {
			if err := addRole(qc, r, t, c.DefaultBlock); err != nil {
				return err
			}
		}
	}

	return nil
}

// addRole adds a role to the compiler
func addRole(qc *qcode.Compiler, r Role, t RoleTable, defaultBlock bool) error {
	ro := defaultBlock && r.Name == "anon"

	if t.ReadOnly {
		ro = true
	}

	query := qcode.QueryConfig{Block: false}
	insert := qcode.InsertConfig{Block: ro}
	update := qcode.UpdateConfig{Block: ro}
	upsert := qcode.UpsertConfig{Block: ro}
	del := qcode.DeleteConfig{Block: ro}

	if t.Query != nil {
		query = qcode.QueryConfig{
			Limit:            t.Query.Limit,
			Filters:          t.Query.Filters,
			Columns:          t.Query.Columns,
			DisableFunctions: t.Query.DisableFunctions,
			Block:            t.Query.Block,
		}
	}

	if t.Insert != nil {
		insert = qcode.InsertConfig{
			Columns: t.Insert.Columns,
			Presets: t.Insert.Presets,
			Block:   t.Insert.Block,
		}
	}

	if t.Update != nil {
		update = qcode.UpdateConfig{
			Filters: t.Update.Filters,
			Columns: t.Update.Columns,
			Presets: t.Update.Presets,
			Block:   t.Update.Block,
		}
	}

	if t.Upsert != nil {
		upsert = qcode.UpsertConfig{
			Filters: t.Upsert.Filters,
			Columns: t.Upsert.Columns,
			Presets: t.Upsert.Presets,
			Block:   t.Upsert.Block,
		}
	}

	if t.Delete != nil {
		del = qcode.DeleteConfig{
			Filters: t.Delete.Filters,
			Columns: t.Delete.Columns,
			Block:   t.Delete.Block,
		}
	}

	return qc.AddRole(r.Name, t.Schema, t.Name, qcode.TRConfig{
		Query:  query,
		Insert: insert,
		Update: update,
		Upsert: upsert,
		Delete: del,
	})
}

// GetTable returns a table from the role
func (r *Role) GetTable(schema, name string) *RoleTable {
	return r.tm[name]
}

// fkTarget holds the parsed components of a foreign key reference.
type fkTarget struct {
	Database string // Target database (empty = same database)
	Schema   string
	Table    string
	Column   string
}

// getFK parses the foreign key reference string.
// Supported formats:
//   - "table.column"                  -> same db, default schema
//   - "schema.table.column"           -> same db, explicit schema
//   - "database:table.column"         -> cross-db, default schema
//   - "database:schema.table.column"  -> cross-db, explicit schema
func (c *Column) getFK(defaultSchema string) (fkTarget, bool) {
	fk := c.ForeignKey

	var database string
	if idx := strings.IndexByte(fk, ':'); idx != -1 {
		database = fk[:idx]
		fk = fk[idx+1:]
	}

	v := strings.SplitN(fk, ".", 3)
	switch len(v) {
	case 2:
		return fkTarget{Database: database, Schema: defaultSchema, Table: v[0], Column: v[1]}, true
	case 3:
		return fkTarget{Database: database, Schema: v[0], Table: v[1], Column: v[2]}, true
	default:
		return fkTarget{}, false
	}
}

// sanitize trims the value
func sanitize(value string) string {
	return strings.TrimSpace(value)
}

// isASCII checks if the string is ASCII
func isASCII(s string) (int, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return i, false
		}
	}
	return -1, true
}

// initAllowList initializes the allow list
func (gj *graphjinEngine) initAllowList() (err error) {
	gj.allowList, err = allow.New(
		gj.log,
		gj.fs,
		gj.conf.DisableAllowList) // if true then read only
	if err != nil {
		return fmt.Errorf("failed to initialize allow list: %w", err)
	}

	return nil
}
