package bigquerydriver

import (
	"regexp"
	"strings"
)

// GraphJin's BigQuery catalog SQL is written relative to "the current
// dataset": it reads bare INFORMATION_SCHEMA views and asks for the dataset
// and project through @@dataset_id and @@project_id. BigQuery needs the views
// qualified, and TABLE_STORAGE exists only per region. qualify fills those in
// from the connector's own project, dataset and location.
var (
	datasetViewPattern = regexp.MustCompile(`(?i)\binformation_schema\.(columns|key_column_usage|table_constraints|constraint_column_usage|tables)\b`)
	regionViewPattern  = regexp.MustCompile(`(?i)\binformation_schema\.table_storage\b`)
	datasetVarPattern  = regexp.MustCompile(`(?i)@@dataset_id\b`)
	projectVarPattern  = regexp.MustCompile(`(?i)@@project_id\b`)
)

// qualify rewrites the catalog references in query. Text inside string
// literals and quoted identifiers is left alone, so a value that happens to
// read "information_schema.tables" is never changed.
func (s *state) qualify(query, location string) string {
	if !strings.Contains(query, "@@") && !containsFold(query, "information_schema") {
		return query // every query GraphJin compiles from GraphQL
	}

	// The forms BigQuery documents: `project`.`dataset`.INFORMATION_SCHEMA.VIEW
	// and `project`.`region-location`.INFORMATION_SCHEMA.TABLE_STORAGE.
	dataset := "`" + s.conf.ProjectID + "`.`" + s.conf.DatasetID + "`.INFORMATION_SCHEMA."
	region := "`" + s.conf.ProjectID + "`.`region-" + strings.ToLower(location) + "`.INFORMATION_SCHEMA.TABLE_STORAGE"

	var out strings.Builder
	out.Grow(len(query) + 128)
	for len(query) > 0 {
		code, quoted, rest := splitQuoted(query)
		if code != "" {
			code = regionViewPattern.ReplaceAllLiteralString(code, region)
			code = datasetViewPattern.ReplaceAllStringFunc(code, func(m string) string {
				return dataset + strings.ToUpper(m[len("information_schema."):])
			})
			code = datasetVarPattern.ReplaceAllLiteralString(code, "'"+s.conf.DatasetID+"'")
			code = projectVarPattern.ReplaceAllLiteralString(code, "'"+s.conf.ProjectID+"'")
			out.WriteString(code)
		}
		out.WriteString(quoted)
		query = rest
	}
	return out.String()
}

// splitQuoted cuts sql into the code before the next quoted span, the span
// itself (a '...' or "..." string, or a `...` identifier, with its quotes),
// and what follows. A backslash escapes the next character, as in GoogleSQL.
// An unterminated span runs to the end; BigQuery will reject it either way.
func splitQuoted(sql string) (code, quoted, rest string) {
	start := strings.IndexAny(sql, "'\"`")
	if start < 0 {
		return sql, "", ""
	}
	q := sql[start]
	for i := start + 1; i < len(sql); i++ {
		switch sql[i] {
		case '\\':
			i++
		case q:
			return sql[:start], sql[start : i+1], sql[i+1:]
		}
	}
	return sql[:start], sql[start:], ""
}

// containsFold reports whether s contains sub, ignoring ASCII case, without
// allocating. sub must be lower case.
func containsFold(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		j := 0
		for j < len(sub) {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != sub[j] {
				break
			}
			j++
		}
		if j == len(sub) {
			return true
		}
	}
	return false
}
