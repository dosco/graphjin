package core

import (
	"strings"
)

func (s *gstate) readOnlyMutationRoot() (string, bool) {
	if s == nil || s.cs == nil || s.cs.st.qc == nil || s.gj == nil {
		return "", false
	}
	dbName := s.database
	if dbName == "" {
		dbName = s.gj.defaultDB
	}
	qc := s.cs.st.qc
	for _, rootID := range qc.Roots {
		sel := qc.Selects[rootID]
		table := sel.Ti.Name
		schema := sel.Ti.Schema
		if s.gj.tableReadOnly(dbName, schema, table) {
			if dbName != "" {
				return dbName + "." + table, true
			}
			return table, true
		}
	}
	return "", false
}

func (gj *graphjinEngine) tableReadOnly(dbName, schema, table string) bool {
	if gj == nil || gj.conf == nil {
		return false
	}
	for _, confTable := range gj.conf.Tables {
		if !confTable.ReadOnly {
			continue
		}
		if confTable.Database != "" && dbName != "" && confTable.Database != dbName {
			continue
		}
		if confTable.Schema != "" && schema != "" && !strings.EqualFold(confTable.Schema, schema) {
			continue
		}
		if strings.EqualFold(confTable.Name, table) || strings.EqualFold(confTable.Table, table) {
			return true
		}
	}
	return false
}
