package core

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/dosco/super-graph/core/internal/sdata"
	"github.com/gobuffalo/flect"
)

func (sg *SuperGraph) initConfig() error {
	c := sg.conf

	for k, v := range c.Inflections {
		flect.AddPlural(k, v)
	}

	// Tables: Validate and sanitize
	tm := make(map[string]struct{})

	for i := 0; i < len(c.Tables); i++ {
		t := &c.Tables[i]
		// t.Name = flect.Pluralize(strings.ToLower(t.Name))

		if _, ok := tm[t.Name]; ok {
			sg.conf.Tables = append(c.Tables[:i], c.Tables[i+1:]...)
			sg.log.Printf("WRN duplicate table found: %s", t.Name)
		}
		tm[t.Name] = struct{}{}

		t.Table = flect.Pluralize(strings.ToLower(t.Table))
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

	sg.roles = make(map[string]*Role)

	for i := 0; i < len(c.Roles); i++ {
		role := &c.Roles[i]
		role.Name = sanitize(role.Name)

		if _, ok := sg.roles[role.Name]; ok {
			c.Roles = append(c.Roles[:i], c.Roles[i+1:]...)
			sg.log.Printf("WRN duplicate role found: %s", role.Name)
		}

		role.Match = sanitize(role.Match)
		role.tm = make(map[string]*RoleTable)

		for n, table := range role.Tables {
			role.tm[table.Name] = &role.Tables[n]
		}

		sg.roles[role.Name] = role
	}

	// If user role not defined then create it
	if _, ok := sg.roles["user"]; !ok {
		ur := Role{
			Name: "user",
			tm:   make(map[string]*RoleTable),
		}
		c.Roles = append(c.Roles, ur)
		sg.roles["user"] = &ur
	}

	// If anon role is not defined then create it
	if _, ok := sg.roles["anon"]; !ok {
		ur := Role{
			Name: "anon",
			tm:   make(map[string]*RoleTable),
		}
		c.Roles = append(c.Roles, ur)
		sg.roles["anon"] = &ur
	}

	if c.RolesQuery != "" {
		if n, ok := isASCII(c.RolesQuery); !ok {
			return fmt.Errorf("roles_query: invalid character (%s) at %d",
				c.RolesQuery[:n+1], n+1)
		}
		n := 0
		for k, v := range sg.roles {
			if k == "user" || k == "anon" {
				n++
			} else if v.Match != "" {
				n++
			}
		}
		sg.abacEnabled = (n > 2)

		if !sg.abacEnabled {
			sg.log.Printf("WRN attribute based access control disabled: no custom roles (with 'match' defined)")
		}
	} else {
		sg.log.Printf("INF attribute based access control disabled: roles_query not set")
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

func addTables(c *Config, di *sdata.DBInfo) error {
	var err error

	for _, t := range c.Tables {
		for _, c := range t.Columns {
			if !c.Primary {
				continue
			}
			if c1, err := di.GetColumn(t.Name, c.Name); err == nil {
				c1.PrimaryKey = true
			} else {
				return fmt.Errorf("config: set primary key: (%s) %w", t.Name, err)
			}
			break
		}

		switch t.Type {
		case "json", "jsonb":
			err = addJsonTable(di, t.Columns, t)

		case "polymorphic":
			err = addVirtualTable(di, t.Columns, t)
		}

		if err != nil {
			return err
		}

	}
	return nil
}

func addJsonTable(di *sdata.DBInfo, cols []Column, t Table) error {
	// This is for jsonb columns that want to be tables.
	bc, err := di.GetColumn(t.Table, t.Name)
	if err != nil {
		return fmt.Errorf("json table: %w", err)
	}

	if bc.Type != "json" && bc.Type != "jsonb" {
		return fmt.Errorf(
			"json table: column '%s' in table '%s' is of type '%s'. Only JSON or JSONB is valid",
			t.Name, t.Table, bc.Type)
	}

	table := sdata.DBTable{
		Name: t.Name,
		Key:  strings.ToLower(t.Name),
		Type: bc.Type,
	}

	columns := make([]sdata.DBColumn, 0, len(cols))

	for i := range cols {
		c := cols[i]
		columns = append(columns, sdata.DBColumn{
			Name: c.Name,
			Key:  strings.ToLower(c.Name),
			Type: c.Type,
		})
	}

	bc.FKeyTable = t.Name
	di.AddTable(table, columns)

	return nil
}

func addVirtualTable(di *sdata.DBInfo, cols []Column, t Table) error {
	if len(cols) == 0 {
		return fmt.Errorf("polymorphic table: no id column specified")
	}

	c := cols[0]

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

func addForeignKeys(c *Config, di *sdata.DBInfo) error {
	for _, t := range c.Tables {
		if t.Type == "polymorphic" {
			continue
		}
		for _, c := range t.Columns {
			if c.ForeignKey == "" {
				continue
			}
			if err := addForeignKey(di, c, t); err != nil {
				return err
			}
		}
	}
	return nil
}

func addForeignKey(di *sdata.DBInfo, c Column, t Table) error {
	tn := t.Name
	c1, err := di.GetColumn(tn, c.Name)
	if err != nil {
		return fmt.Errorf("config: foreign keys: %w", err)
	}

	v := strings.SplitN(c.ForeignKey, ".", 2)
	if len(v) != 2 {
		return fmt.Errorf(
			"config: invalid foreign key defined for table '%s' and column '%s': %s",
			tn, c.Name, c.ForeignKey)
	}

	// check if it's a polymorphic foreign key
	if _, err := di.GetColumn(tn, v[0]); err == nil {
		c2, err := di.GetColumn(tn, v[1])
		if err != nil {
			return fmt.Errorf(
				"config: invalid column '%s' for polymorphic relationship on table '%s' and column '%s'",
				v[1], tn, c.Name)
		}

		c1.FKeyTable = v[0]
		c1.FKeyColID = []int16{c2.ID}
		return nil
	}

	fkt, fkc := v[0], v[1]
	c3, err := di.GetColumn(fkt, fkc)
	if err != nil {
		return fmt.Errorf(
			"config: foreign key for table '%s' and column '%s' points to unknown table '%s' and column '%s'",
			t.Name, c.Name, v[0], v[1])
	}

	c1.FKeyTable = fkt
	c1.FKeyColID = []int16{c3.ID}

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

	return qc.AddRole(r.Name, t.Name, qcode.TRConfig{
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
