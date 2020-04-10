package core

import (
	"fmt"
	"strings"

	"github.com/dosco/super-graph/config"
	"github.com/dosco/super-graph/core/internal/psql"
	"github.com/dosco/super-graph/core/internal/qcode"
)

func addTables(c *config.Config, di *psql.DBInfo) error {
	for _, t := range c.Tables {
		if len(t.Table) == 0 || len(t.Columns) == 0 {
			continue
		}
		if err := addTable(di, t.Columns, t); err != nil {
			return err
		}
	}
	return nil
}

func addTable(di *psql.DBInfo, cols []config.Column, t config.Table) error {
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

func addForeignKeys(c *config.Config, di *psql.DBInfo) error {
	for _, t := range c.Tables {
		for _, c := range t.Columns {
			if len(c.ForeignKey) == 0 {
				continue
			}
			if err := addForeignKey(di, c, t); err != nil {
				return err
			}
		}
	}
	return nil
}

func addForeignKey(di *psql.DBInfo, c config.Column, t config.Table) error {
	c1, ok := di.GetColumn(t.Name, c.Name)
	if !ok {
		return fmt.Errorf(
			"Invalid table '%s' or column '%s' in config.Config",
			t.Name, c.Name)
	}

	v := strings.SplitN(c.ForeignKey, ".", 2)
	if len(v) != 2 {
		return fmt.Errorf(
			"Invalid foreign_key in config.Config for table '%s' and column '%s",
			t.Name, c.Name)
	}

	fkt, fkc := v[0], v[1]
	c2, ok := di.GetColumn(fkt, fkc)
	if !ok {
		return fmt.Errorf(
			"Invalid foreign_key in config.Config for table '%s' and column '%s",
			t.Name, c.Name)
	}

	c1.FKeyTable = fkt
	c1.FKeyColID = []int16{c2.ID}

	return nil
}

func addRoles(c *config.Config, qc *qcode.Compiler) error {
	for _, r := range c.Roles {
		for _, t := range r.Tables {
			if err := addRole(qc, r, t); err != nil {
				return err
			}
		}
	}

	return nil
}

func addRole(qc *qcode.Compiler, r config.Role, t config.RoleTable) error {
	blockFilter := []string{"false"}

	query := qcode.QueryConfig{
		Limit:            t.Query.Limit,
		Filters:          t.Query.Filters,
		Columns:          t.Query.Columns,
		DisableFunctions: t.Query.DisableFunctions,
	}

	if t.Query.Block {
		query.Filters = blockFilter
	}

	insert := qcode.InsertConfig{
		Filters: t.Insert.Filters,
		Columns: t.Insert.Columns,
		Presets: t.Insert.Presets,
	}

	if t.Insert.Block {
		insert.Filters = blockFilter
	}

	update := qcode.UpdateConfig{
		Filters: t.Update.Filters,
		Columns: t.Update.Columns,
		Presets: t.Update.Presets,
	}

	if t.Update.Block {
		update.Filters = blockFilter
	}

	delete := qcode.DeleteConfig{
		Filters: t.Delete.Filters,
		Columns: t.Delete.Columns,
	}

	if t.Delete.Block {
		delete.Filters = blockFilter
	}

	return qc.AddRole(r.Name, t.Name, qcode.TRConfig{
		Query:  query,
		Insert: insert,
		Update: update,
		Delete: delete,
	})
}
