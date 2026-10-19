package core

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func (c *Config) validateSourceAccessGrants(source, kind string, access SourceAccessConfig) error {
	if len(access.Grants) == 0 {
		return nil
	}
	if kind != sourcecap.KindDatabase {
		return fmt.Errorf("sources[%q].access.grants: grants are supported on database sources only", source)
	}
	adminRoles := stringSet(c.EffectiveIdentityConfig().AdminRoles)
	seen := make(map[string]struct{})
	for i, grant := range access.Grants {
		role := strings.TrimSpace(grant.Role)
		path := fmt.Sprintf("sources[%q].access.grants[%d]", source, i)
		switch {
		case role == "":
			return fmt.Errorf("%s: role is required", path)
		case isReservedRoleName(role):
			return fmt.Errorf("%s: role %q is reserved", path, role)
		case roleInSet(role, adminRoles):
			return fmt.Errorf("%s: role %q is an admin role, and admin roles do not use grants", path, role)
		case !c.grantRoleDefined(role):
			return fmt.Errorf("%s: role %q is not defined in roles", path, role)
		case len(grant.Tables) == 0:
			return fmt.Errorf("%s: tables is required", path)
		}
		for j, table := range grant.Tables {
			name := strings.TrimSpace(table.Name)
			tpath := fmt.Sprintf("%s.tables[%d]", path, j)
			if name == "" {
				return fmt.Errorf("%s: name is required", tpath)
			}
			if len(table.Columns) == 0 {
				return fmt.Errorf("%s: columns is required", tpath)
			}
			for _, col := range table.Columns {
				if strings.TrimSpace(col) == "" {
					return fmt.Errorf("%s: column names must not be empty", tpath)
				}
			}
			if stringInFold(access.BlockedTables, name) {
				return fmt.Errorf("%s: table %q is in blocked_tables", tpath, name)
			}
			key := strings.ToLower(role) + "\x00" + strings.ToLower(name)
			if _, ok := seen[key]; ok {
				return fmt.Errorf("%s: role %q has more than one grant for table %q", tpath, role, name)
			}
			seen[key] = struct{}{}
		}
	}
	return nil
}

func (c *Config) grantRoleDefined(role string) bool {
	if strings.EqualFold(role, "anon") || strings.EqualFold(role, "user") {
		return true
	}
	for _, r := range c.Roles {
		if strings.EqualFold(strings.TrimSpace(r.Name), role) {
			return true
		}
	}
	return false
}

func stringInFold(list []string, value string) bool {
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return true
		}
	}
	return false
}

type sourceGrantEntry struct {
	role    string
	table   SourceAccessGrantTable
	matched bool
}

type sourceGrantIndex struct {
	source  string
	entries []*sourceGrantEntry
}

func newSourceGrantIndex(source string, access SourceAccessConfig) *sourceGrantIndex {
	idx := &sourceGrantIndex{source: source}
	for _, grant := range access.Grants {
		for _, table := range grant.Tables {
			idx.entries = append(idx.entries, &sourceGrantEntry{
				role:  strings.TrimSpace(grant.Role),
				table: table,
			})
		}
	}
	return idx
}

// find marks and returns the grant of a role for a table.
func (idx *sourceGrantIndex) find(role string, table *sdata.DBTable) (*sourceGrantEntry, error) {
	var found *sourceGrantEntry
	for _, e := range idx.entries {
		if !strings.EqualFold(e.role, role) || !sourceAccessTableListed([]string{e.table.Name}, table) {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("sources[%q].access.grants: role %q has more than one grant for table %q", idx.source, role, table.Name)
		}
		e.matched = true
		found = e
	}
	return found, nil
}

// rejectClosedTable fails when a grant names a table that GraphJin blocks
// for every role.
func (idx *sourceGrantIndex) rejectClosedTable(table *sdata.DBTable, reason string) error {
	for _, e := range idx.entries {
		if sourceAccessTableListed([]string{e.table.Name}, table) {
			return fmt.Errorf("sources[%q].access.grants: role %q cannot read table %q: %s", idx.source, e.role, table.Name, reason)
		}
	}
	return nil
}

func (idx *sourceGrantIndex) unmatched() error {
	for _, e := range idx.entries {
		if !e.matched {
			return fmt.Errorf("sources[%q].access.grants: role %q: table %q not found", idx.source, e.role, e.table.Name)
		}
	}
	return nil
}

func applySourceGrant(rt *RoleTable, grant *sourceGrantEntry, table *sdata.DBTable, access SourceAccessConfig, readMode, source string) error {
	if readMode == AccessModeBlocked {
		return fmt.Errorf("sources[%q].access.grants: role %q cannot read table %q: read access is blocked for this table", source, grant.role, table.Name)
	}
	cols := make([]string, 0, len(grant.table.Columns))
	for _, col := range grant.table.Columns {
		col = strings.TrimSpace(col)
		if !tableHasColumn(table, col) {
			return fmt.Errorf("sources[%q].access.grants: role %q: table %q has no column %q", source, grant.role, table.Name, col)
		}
		cols = append(cols, col)
	}
	filters := modeFilters(readMode, access, false)
	if f := strings.TrimSpace(grant.table.Filter); f != "" {
		filters = append(filters, f)
	}
	rt.Query = &Query{Columns: cols, Filters: filters}
	return nil
}
