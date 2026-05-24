package bigquerylive

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	bq "google.golang.org/api/bigquery/v2"
)

func CreateDataset(ctx context.Context, svc *bq.Service, projectID, datasetID, location string) error {
	if strings.TrimSpace(location) == "" {
		location = "US"
	}
	ds := &bq.Dataset{
		DatasetReference: &bq.DatasetReference{
			ProjectId: projectID,
			DatasetId: datasetID,
		},
		Location: location,
		Labels: map[string]string{
			"graphjin": "bigquery_live_test",
		},
	}
	if _, err := svc.Datasets.Insert(projectID, ds).Context(ctx).Do(); err != nil {
		return fmt.Errorf("bigquery create dataset %s.%s: %w", projectID, datasetID, err)
	}
	return nil
}

func DropDataset(ctx context.Context, svc *bq.Service, projectID, datasetID string) error {
	err := svc.Datasets.Delete(projectID, datasetID).DeleteContents(true).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("bigquery drop dataset %s.%s: %w", projectID, datasetID, err)
	}
	return nil
}

func SeedFile(ctx context.Context, svc *bq.Service, projectID, datasetID, location, seedPath string) error {
	data, err := os.ReadFile(seedPath)
	if err != nil {
		return err
	}
	for _, raw := range splitSQLStatements(string(data)) {
		for _, stmt := range normalizeSeedStatements(raw) {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if err := runSeedStatement(ctx, svc, projectID, datasetID, location, stmt); err != nil {
				return err
			}
		}
	}
	return nil
}

func SeedRowCounts(seedPath string) (map[string]uint64, error) {
	data, err := os.ReadFile(seedPath)
	if err != nil {
		return nil, err
	}
	return SQLRowCounts(string(data)), nil
}

func SQLRowCounts(sql string) map[string]uint64 {
	out := map[string]uint64{}
	for _, stmt := range splitSQLStatements(sql) {
		table, ok := insertTableName(stmt)
		if !ok {
			continue
		}
		if n, ok := generateArrayRowCount(stmt); ok {
			out[table] += n
			continue
		}
		if n := valuesRowCount(stmt); n > 0 {
			out[table] += n
		}
	}
	return out
}

func insertTableName(stmt string) (string, bool) {
	stmt = strings.TrimSpace(stmt)
	re := regexp.MustCompile(`(?is)^INSERT\s+INTO\s+(` + "`[^`]+`" + `|[A-Za-z_][A-Za-z0-9_\.]*)`)
	m := re.FindStringSubmatch(stmt)
	if len(m) != 2 {
		return "", false
	}
	name := strings.ToLower(strings.Trim(m[1], "`"))
	parts := strings.Split(name, ".")
	return parts[len(parts)-1], true
}

func generateArrayRowCount(stmt string) (uint64, bool) {
	re := regexp.MustCompile(`(?is)GENERATE_ARRAY\(\s*(\d+)\s*,\s*(\d+)\s*\)`)
	m := re.FindStringSubmatch(stmt)
	if len(m) != 3 {
		return 0, false
	}
	start, err1 := strconv.ParseUint(m[1], 10, 64)
	end, err2 := strconv.ParseUint(m[2], 10, 64)
	if err1 != nil || err2 != nil || end < start {
		return 0, false
	}
	return end - start + 1, true
}

func valuesRowCount(stmt string) uint64 {
	upper := strings.ToUpper(stmt)
	idx := strings.Index(upper, " VALUES")
	if idx < 0 {
		return 0
	}
	values := stmt[idx+len(" VALUES"):]
	var count uint64
	depth := 0
	inSingle := false
	inDouble := false
	for i := 0; i < len(values); i++ {
		ch := values[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '(':
			if !inSingle && !inDouble {
				depth++
			}
		case ')':
			if !inSingle && !inDouble && depth > 0 {
				depth--
				if depth == 0 {
					count++
				}
			}
		}
	}
	return count
}

func runSeedStatement(ctx context.Context, svc *bq.Service, projectID, datasetID, location, stmt string) error {
	useLegacy := false
	req := &bq.QueryRequest{
		Query:        stmt,
		UseLegacySql: &useLegacy,
		DefaultDataset: &bq.DatasetReference{
			ProjectId: projectID,
			DatasetId: datasetID,
		},
		Location:           location,
		TimeoutMs:          30000,
		UseQueryCache:      boolPtr(false),
		MaximumBytesBilled: 1000000000,
		Labels: map[string]string{
			"graphjin": "bigquery_seed",
		},
	}
	res, err := svc.Jobs.Query(projectID, req).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("bigquery seed: %w\nSQL: %s", err, stmt)
	}
	if err := firstQueryError(res.Errors); err != nil {
		return fmt.Errorf("bigquery seed: %w\nSQL: %s", err, stmt)
	}
	for res != nil && !res.JobComplete {
		if res.JobReference == nil {
			break
		}
		gr, err := svc.Jobs.GetQueryResults(res.JobReference.ProjectId, res.JobReference.JobId).
			Location(location).
			Context(ctx).
			Do()
		if err != nil {
			return fmt.Errorf("bigquery seed wait: %w\nSQL: %s", err, stmt)
		}
		if err := firstQueryError(gr.Errors); err != nil {
			return fmt.Errorf("bigquery seed wait: %w\nSQL: %s", err, stmt)
		}
		if gr.JobComplete {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return nil
}

func splitSQLStatements(sql string) []string {
	var out []string
	var b strings.Builder
	inSingle := false
	inDouble := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch ch {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ';':
			if !inSingle && !inDouble {
				out = append(out, b.String())
				b.Reset()
				continue
			}
		}
		b.WriteByte(ch)
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, b.String())
	}
	return out
}

func normalizeSeedStatements(stmt string) []string {
	stmt = regexp.MustCompile(`(?is)TO_JSON\(\s*(\[[^\)]*\])\s*\)`).ReplaceAllString(stmt, "$1")
	table, ok := createTableName(stmt)
	if !ok {
		return []string{stmt}
	}
	stmt = stripSelfReferencingFKs(stmt, table)
	return []string{stmt}
}

func createTableName(stmt string) (string, bool) {
	stmt = strings.TrimSpace(stmt)
	const prefix = "CREATE TABLE "
	if !strings.HasPrefix(strings.ToUpper(stmt), prefix) {
		return "", false
	}
	rest := strings.TrimSpace(stmt[len(prefix):])
	if rest == "" {
		return "", false
	}
	if rest[0] == '`' {
		end := strings.IndexByte(rest[1:], '`')
		if end < 0 {
			return "", false
		}
		return rest[1 : end+1], true
	}
	end := len(rest)
	for i, ch := range rest {
		if ch == '(' || ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			end = i
			break
		}
	}
	name := strings.TrimSpace(rest[:end])
	return strings.Trim(name, "`"), name != ""
}

func stripSelfReferencingFKs(stmt, table string) string {
	re := regexp.MustCompile(`(?is)(\b([A-Za-z_][A-Za-z0-9_]*)\s+[^,\n]*?)\s+REFERENCES\s+` + identPattern(table) + `\s*\(([^)]*)\)\s+NOT\s+ENFORCED`)
	return re.ReplaceAllString(stmt, "$1")
}

func identPattern(name string) string {
	name = strings.Trim(name, "`")
	parts := strings.Split(name, ".")
	if len(parts) == 0 {
		return "`?" + regexp.QuoteMeta(name) + "`?"
	}
	return "`?" + regexp.QuoteMeta(parts[len(parts)-1]) + "`?"
}
