package serv

import (
	"fmt"
	"strings"

	"github.com/dosco/super-graph/psql"
	"github.com/dosco/super-graph/qcode"
)

func addForeignKeys(c *config, di *psql.DBInfo) error {
	for _, t := range c.Tables {
		for _, c := range t.Columns {
			if err := addForeignKey(di, c, t); err != nil {
				return err
			}
		}
	}
	return nil
}

func addForeignKey(di *psql.DBInfo, c configColumn, t configTable) error {
	c1, ok := di.GetColumn(t.Name, c.Name)
	if !ok {
		return fmt.Errorf(
			"Invalid table '%s' or column '%s in config",
			t.Name, c.Name)
	}

	v := strings.SplitN(c.ForeignKey, ".", 2)
	if len(v) != 2 {
		return fmt.Errorf(
			"Invalid foreign_key in config for table '%s' and column '%s",
			t.Name, c.Name)
	}

	fkt, fkc := v[0], v[1]
	c2, ok := di.GetColumn(fkt, fkc)
	if !ok {
		return fmt.Errorf(
			"Invalid foreign_key in config for table '%s' and column '%s",
			t.Name, c.Name)
	}

	c1.FKeyTable = fkt
	c1.FKeyColID = []int16{c2.ID}

	return nil
}

func addRoles(c *config, qc *qcode.Compiler) error {
	for _, r := range c.Roles {
		for _, t := range r.Tables {
			if err := addRole(qc, r, t); err != nil {
				return err
			}
		}
	}

	return nil
}

func addRole(qc *qcode.Compiler, r configRole, t configRoleTable) error {
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

	if t.Query.Block {
		insert.Filters = blockFilter
	}

	update := qcode.UpdateConfig{
		Filters: t.Insert.Filters,
		Columns: t.Insert.Columns,
		Presets: t.Insert.Presets,
	}

	if t.Query.Block {
		update.Filters = blockFilter
	}

	delete := qcode.DeleteConfig{
		Filters: t.Insert.Filters,
		Columns: t.Insert.Columns,
	}

	if t.Query.Block {
		delete.Filters = blockFilter
	}

	return qc.AddRole(r.Name, t.Name, qcode.TRConfig{
		Query:  query,
		Insert: insert,
		Update: update,
		Delete: delete,
	})
}
