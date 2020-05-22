package core

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/core/internal/qcode"
	"github.com/gobuffalo/flect"
)

func (sg *SuperGraph) initConfig() error {
	c := sg.conf

	for k, v := range c.Inflections {
		flect.AddPlural(k, v)
	}

	// Variables: Validate and sanitize
	for k, v := range c.Vars {
		c.Vars[k] = sanitizeVars(v)
	}

	// Tables: Validate and sanitize
	tm := make(map[string]struct{})

	for i := 0; i < len(c.Tables); i++ {
		t := &c.Tables[i]
		t.Name = flect.Pluralize(strings.ToLower(t.Name))

		if _, ok := tm[t.Name]; ok {
			sg.conf.Tables = append(c.Tables[:i], c.Tables[i+1:]...)
			sg.log.Printf("WRN duplicate table found: %s", t.Name)
		}
		tm[t.Name] = struct{}{}

		t.Table = flect.Pluralize(strings.ToLower(t.Table))
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

	// If anon role is not defined and DefaultBlock is not then then create it
	if _, ok := sg.roles["anon"]; !ok && !c.DefaultBlock {
		ur := Role{
			Name: "anon",
			tm:   make(map[string]*RoleTable),
		}
		c.Roles = append(c.Roles, ur)
		sg.roles["anon"] = &ur
	}

	// Roles: validate and sanitize
	c.RolesQuery = sanitizeVars(c.RolesQuery)

	if c.RolesQuery == "" {
		sg.log.Printf("WRN roles_query not defined: attribute based access control disabled")
	}

	_, userExists := sg.roles["user"]
	_, sg.anonExists = sg.roles["anon"]

	sg.abacEnabled = userExists && c.RolesQuery != ""

	return nil
}

func getDBTableAliases(c *Config) map[string][]string {
	m := make(map[string][]string, len(c.Tables))

	for i := range c.Tables {
		t := c.Tables[i]

		if len(t.Table) == 0 || len(t.Columns) != 0 {
			continue
		}

		m[t.Table] = append(m[t.Table], t.Name)
	}
	return m
}

func addTables(c *Config, di *psql.DBInfo) error {
	for _, t := range c.Tables {
		if t.Table == "" || len(t.Columns) == 0 {
			continue
		}
		if err := addTable(di, t.Columns, t); err != nil {
			return err
		}

	}
	return nil
}

func addTable(di *psql.DBInfo, cols []Column, t Table) error {
	bc, ok := di.GetColumn(t.Table, t.Name)
	if !ok {
		return fmt.Errorf(
			"Column '%s' not found on table '%s'",
			t.Name, t.Table)
	}

	if bc.Type != "json" && bc.Type != "jsonb" {
		return fmt.Errorf(
			"Column '%s' in table '%s' is of type '%s'. Only JSON or JSONB is valid",
			t.Name, t.Table, bc.Type)
	}

	table := psql.DBTable{
		Name: t.Name,
		Key:  strings.ToLower(t.Name),
		Type: bc.Type,
	}

	columns := make([]psql.DBColumn, 0, len(cols))

	for i := range cols {
		c := cols[i]
		columns = append(columns, psql.DBColumn{
			Name: c.Name,
			Key:  strings.ToLower(c.Name),
			Type: c.Type,
		})
	}

	di.AddTable(table, columns)
	bc.FKeyTable = t.Name

	return nil
}

func addForeignKeys(c *Config, di *psql.DBInfo) error {
	for _, t := range c.Tables {
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

func addForeignKey(di *psql.DBInfo, c Column, t Table) error {
	c1, ok := di.GetColumn(t.Name, c.Name)
	if !ok {
		return fmt.Errorf(
			"Invalid table '%s' or column '%s' in Config",
			t.Name, c.Name)
	}

	v := strings.SplitN(c.ForeignKey, ".", 2)
	if len(v) != 2 {
		return fmt.Errorf(
			"Invalid foreign_key in Config for table '%s' and column '%s",
			t.Name, c.Name)
	}

	fkt, fkc := v[0], v[1]
	c2, ok := di.GetColumn(fkt, fkc)
	if !ok {
		return fmt.Errorf(
			"Invalid foreign_key in Config for table '%s' and column '%s",
			t.Name, c.Name)
	}

	c1.FKeyTable = fkt
	c1.FKeyColID = []int16{c2.ID}

	return nil
}

func addRoles(c *Config, qc *qcode.Compiler) error {
	for _, r := range c.Roles {
		for _, t := range r.Tables {
			if err := addRole(qc, r, t); err != nil {
				return err
			}
		}
	}

	return nil
}

func addRole(qc *qcode.Compiler, r Role, t RoleTable) error {
	blocked := struct {
		readOnly bool
		query    bool
		insert   bool
		update   bool
		delete   bool
	}{true, true, true, true, true}

	if r.Name == "anon" {
		blocked.query = false
	} else {
		blocked.readOnly = false
		blocked.query = false
		blocked.insert = false
		blocked.update = false
		blocked.delete = false
	}

	if t.ReadOnly != nil {
		blocked.readOnly = *t.ReadOnly
	}
	if t.Query.Block != nil {
		blocked.query = *t.Query.Block
	}
	if t.Insert.Block != nil {
		blocked.insert = *t.Insert.Block
	}
	if t.Update.Block != nil {
		blocked.update = *t.Update.Block
	}
	if t.Delete.Block != nil {
		blocked.delete = *t.Delete.Block
	}

	query := qcode.QueryConfig{
		Limit:            t.Query.Limit,
		Filters:          t.Query.Filters,
		Columns:          t.Query.Columns,
		DisableFunctions: t.Query.DisableFunctions,
		Block:            blocked.query,
	}

	insert := qcode.InsertConfig{
		Filters: t.Insert.Filters,
		Columns: t.Insert.Columns,
		Presets: t.Insert.Presets,
		Block:   blocked.insert,
	}

	update := qcode.UpdateConfig{
		Filters: t.Update.Filters,
		Columns: t.Update.Columns,
		Presets: t.Update.Presets,
		Block:   blocked.update,
	}

	del := qcode.DeleteConfig{
		Filters: t.Delete.Filters,
		Columns: t.Delete.Columns,
		Block:   blocked.delete,
	}

	return qc.AddRole(r.Name, t.Name, qcode.TRConfig{
		ReadOnly: blocked.readOnly,
		Query:    query,
		Insert:   insert,
		Update:   update,
		Delete:   del,
	})
}

func (r *Role) GetTable(name string) *RoleTable {
	return r.tm[name]
}

func sanitize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

var (
	varRe1 = regexp.MustCompile(`(?mi)\$([a-zA-Z0-9_.]+)`)
	varRe2 = regexp.MustCompile(`\{\{([a-zA-Z0-9_.]+)\}\}`)
)

func sanitizeVars(s string) string {
	s0 := varRe1.ReplaceAllString(s, `{{$1}}`)

	s1 := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s0)

	return varRe2.ReplaceAllStringFunc(s1, func(m string) string {
		return strings.ToLower(m)
	})
}
