package core

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/dosco/graphjin/core/internal/qcode"
	"github.com/dosco/graphjin/core/internal/sdata"
	"github.com/gobuffalo/flect"
)

func (gj *GraphJin) initConfig() error {
	c := gj.conf

	for _, v := range c.Inflections {
		kv := strings.SplitN(v, ":", 2)
		if kv[0] == "" || kv[1] == "" {
			return fmt.Errorf("inflections: should be an array of singular:plural values: %s", v)
		}
		flect.AddPlural(kv[0], kv[1])
		flect.AddSingular(kv[1], kv[0])
	}

	// Tables: Validate and sanitize
	tm := make(map[string]struct{})

	for i := 0; i < len(c.Tables); i++ {
		t := &c.Tables[i]

		if _, ok := tm[t.Name]; ok {
			gj.conf.Tables = append(c.Tables[:i], c.Tables[i+1:]...)
			gj.log.Printf("WRN duplicate table found: %s", t.Name)
		}
		tm[t.Name] = struct{}{}
	}

	for k, v := range c.Vars {
		if v == "" || !strings.HasPrefix(v, "sql:") {
			continue
		}
		if n, ok := isASCII(v); !ok {
			return fmt.Errorf("variables: %s: invalid character (%s) at %d",
				k, c.RolesQuery[:n+1], n+1)
		}
	}

	gj.roles = make(map[string]*Role)

	for i := 0; i < len(c.Roles); i++ {
		role := &c.Roles[i]
		role.Name = sanitize(role.Name)

		if _, ok := gj.roles[role.Name]; ok {
			c.Roles = append(c.Roles[:i], c.Roles[i+1:]...)
			gj.log.Printf("WRN duplicate role found: %s", role.Name)
		}

		role.Match = sanitize(role.Match)
		role.tm = make(map[string]*RoleTable)

		for n, table := range role.Tables {
			role.tm[table.Name] = &role.Tables[n]
		}

		gj.roles[role.Name] = role
	}

	// If user role not defined then create it
	if _, ok := gj.roles["user"]; !ok {
		ur := Role{
			Name: "user",
			tm:   make(map[string]*RoleTable),
		}
		c.Roles = append(c.Roles, ur)
		gj.roles["user"] = &ur
	}

	// If anon role is not defined then create it
	if _, ok := gj.roles["anon"]; !ok {
		ur := Role{
			Name: "anon",
			tm:   make(map[string]*RoleTable),
		}
		c.Roles = append(c.Roles, ur)
		gj.roles["anon"] = &ur
	}

	if c.RolesQuery != "" {
		if n, ok := isASCII(c.RolesQuery); !ok {
			return fmt.Errorf("roles_query: invalid character (%s) at %d",
				c.RolesQuery[:n+1], n+1)
		}
		n := 0
		for k, v := range gj.roles {
			if k == "user" || k == "anon" {
				n++
			} else if v.Match != "" {
				n++
			}
		}

		// More than 2 roles tell us that custom roles have been added
		// hence ABAC is handled
		gj.abacEnabled = (n > 2)

		if !gj.abacEnabled {
			gj.log.Printf("WRN attribute based access control disabled: no custom roles (with 'match' defined)")
		}
	} else if len(gj.roles) > 2 {
		gj.log.Printf("INF attribute based access control disabled: roles_query not set")
	}

	return nil
}

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

func addTables(conf *Config, di *sdata.DBInfo) error {
	var err error

	for _, t := range conf.Tables {
		switch t.Type {
		case "json", "jsonb":
			err = addJsonTable(conf, di, t)

		case "polymorphic":
			err = addVirtualTable(conf, di, t)

		default:
			err = updateTable(conf, di, t)
		}

		if err != nil {
			return err
		}

	}
	return nil
}

func updateTable(conf *Config, di *sdata.DBInfo, t Table) error {
	for _, c := range t.Columns {
		if c1, err := di.GetColumn(t.Schema, t.Name, c.Name); err == nil {
			if c.Primary {
				c1.PrimaryKey = true
			}
			if c.Array {
				c1.Array = true
			}
		} else {
			return fmt.Errorf("table: %s.%s: %w", t.Schema, t.Name, err)
		}
	}
	return nil
}

func addJsonTable(conf *Config, di *sdata.DBInfo, t Table) error {
	// This is for jsonb column that want to be a table.
	if t.Table == "" {
		return fmt.Errorf("json table: set the 'table' for column '%s'", t.Name)
	}

	bc, err := di.GetColumn(t.Schema, t.Table, t.Name)
	if err != nil {
		return fmt.Errorf("json table: %w", err)
	}

	bt, err := di.GetTable(bc.Schema, bc.Table)
	if err != nil {
		return fmt.Errorf("json table: %w", err)
	}

	if bc.Type != "json" && bc.Type != "jsonb" {
		return fmt.Errorf(
			"json table: column '%s' in table '%s' is of type '%s'. Only JSON or JSONB is valid",
			t.Name, t.Table, bc.Type)
	}

	columns := make([]sdata.DBColumn, 0, len(t.Columns))

	for i := range t.Columns {
		c := t.Columns[i]
		columns = append(columns, sdata.DBColumn{
			Schema: bc.Schema,
			Table:  t.Name,
			Name:   c.Name,
			Key:    strings.ToLower(c.Name),
			Type:   c.Type,
		})
		if c.Type == "" {
			return fmt.Errorf("json table: type parameter missing for column: %s.%s'",
				t.Name, c.Name)
		}
	}

	col1 := sdata.DBColumn{
		PrimaryKey: true,
		Schema:     bc.Schema,
		Table:      bc.Table,
		Name:       bc.Name,
		Key:        bc.Key,
		Type:       bc.Type,
	}

	nt := sdata.NewDBTable(bc.Schema, t.Name, bc.Type, columns)
	nt.PrimaryCol = col1
	nt.SecondaryCol = bt.PrimaryCol
	di.AddTable(nt)

	return nil
}

func addVirtualTable(conf *Config, di *sdata.DBInfo, t Table) error {
	if len(t.Columns) == 0 {
		return fmt.Errorf("polymorphic table: no id column specified")
	}
	c := t.Columns[0]

	if c.ForeignKey == "" {
		return fmt.Errorf("polymorphic table: no 'related_to' specified on id column")
	}
	s := strings.SplitN(c.ForeignKey, ".", 2)

	if len(s) != 2 {
		return fmt.Errorf("polymorphic table: foreign key must be <type column>.<foreign key column>")
	}

	di.VTables = append(di.VTables, sdata.VirtualTable{
		Name:       t.Name,
		IDColumn:   c.Name,
		TypeColumn: s[0],
		FKeyColumn: s[1],
	})

	return nil
}

func addForeignKeys(conf *Config, di *sdata.DBInfo) error {
	for _, t := range conf.Tables {
		if t.Type == "polymorphic" {
			continue
		}
		for _, c := range t.Columns {
			if c.ForeignKey == "" {
				continue
			}
			if err := addForeignKey(conf, di, c, t); err != nil {
				return err
			}
		}
	}
	return nil
}

func addForeignKey(conf *Config, di *sdata.DBInfo, c Column, t Table) error {
	c1, err := di.GetColumn(t.Schema, t.Name, c.Name)
	if err != nil {
		return fmt.Errorf("config: add foreign key: %w", err)
	}

	v := strings.SplitN(c.ForeignKey, ".", 2)
	if len(v) != 2 {
		return fmt.Errorf(
			"config: invalid foreign key defined for table '%s' and column '%s': %s",
			t.Name, c.Name, c.ForeignKey)
	}

	// check if it's a polymorphic foreign key
	if _, err := di.GetColumn(t.Schema, t.Name, v[0]); err == nil {
		c2, err := di.GetColumn(t.Schema, t.Name, v[1])
		if err != nil {
			return fmt.Errorf(
				"config: invalid column '%s' for polymorphic relationship on table '%s' and column '%s'",
				v[1], t.Name, c.Name)
		}

		c1.FKeySchema = t.Schema
		c1.FKeyTable = v[0]
		c1.FKeyCol = c2.Name
		return nil
	}

	fkt, fkc := v[0], v[1]
	c3, err := di.GetColumn(t.Schema, fkt, fkc)
	if err != nil {
		return fmt.Errorf(
			"config: foreign key for table '%s' and column '%s' points to unknown table '%s' and column '%s'",
			t.Name, c.Name, v[0], v[1])
	}

	c1.FKeySchema = t.Schema
	c1.FKeyTable = fkt
	c1.FKeyCol = c3.Name

	return nil
}

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

func addRole(qc *qcode.Compiler, r Role, t RoleTable, defaultBlock bool) error {
	ro := false // read-only

	if defaultBlock && r.Name == "anon" {
		ro = true
	}

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
			Filters: t.Update.Filters,
			Columns: t.Update.Columns,
			Presets: t.Update.Presets,
			Block:   t.Update.Block,
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

func (r *Role) GetTable(name string) *RoleTable {
	return r.tm[name]
}

func sanitize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isASCII(s string) (int, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] > unicode.MaxASCII {
			return i, false
		}
	}
	return -1, true
}
