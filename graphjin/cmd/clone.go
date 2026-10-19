package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
)

// Cloning a running GraphJin server into a local environment.
//
// An organization that wants to tune a model on its own data has a problem: the
// environment needs to be writable and resettable, and their production database
// is neither. A clone resolves it by taking the shape and leaving the contents:
// it reads the catalog — the same description of the schema any connected agent
// already sees — and writes a local SQLite project with the same tables,
// columns, keys and relationships, filled with synthetic rows.
//
// No row of real data is ever read. The only real values that cross over are the
// closed sets the catalog itself publishes — the handful of statuses a column is
// known to take — because a task that filters on a state the business does not
// have is a task about nothing.

// cloneCatalogPageSize bounds one page of catalog cards. The read is paged by
// id rather than taken in one large limit so a big schema clones the same way a
// small one does.
const cloneCatalogPageSize = 200

type cloneOptions struct {
	URL      string
	Out      string
	Rows     int
	Seed     int64
	TokenEnv string
}

type cloneManifest struct {
	SourceURL   string            `json:"source_url"`
	GeneratedAt string            `json:"generated_at"`
	Seed        int64             `json:"seed"`
	Rows        int               `json:"rows_per_table"`
	Tables      int               `json:"tables"`
	Types       map[string]string `json:"unmapped_types,omitempty"`
	Drops       []string          `json:"dropped,omitempty"`
	Notes       []string          `json:"notes,omitempty"`
}

// fetchCloneCatalog reads every card the clone needs, a page at a time.
//
// This deliberately does not reuse the generator's catalog snapshot. That
// query's exact shape is hashed into every frozen suite's fingerprint, so
// widening it to carry per-column evidence would invalidate published
// benchmarks. This asks its own question and leaves that one alone.
func fetchCloneCatalog(ctx context.Context, client *http.Client, baseURL string, headers map[string]string) ([]gjeval.ImportRow, error) {
	// Paging is by offset rather than by a cursor on id.
	//
	// The catalog root is a projection over an in-memory snapshot: it honours
	// limit and offset, and quietly ignores a comparison like id > "…" — a
	// cursor loop against it returns the same first page forever. Offset is
	// exact here because the snapshot is stable for the length of a clone and
	// the read is ordered by id.
	const query = `query {
  gj_catalog(
    where: { kind: { in: ["table", "column", "relationship", "saved_query"] } }
    order_by: { id: asc }
    limit: %d
    offset: %d
  ) { id kind name title summary database_name schema_name table_name column_name details_json examples_json evidence_json }
}`
	var rows []gjeval.ImportRow
	seen := map[string]bool{}
	for offset := 0; ; offset += cloneCatalogPageSize {
		if offset > 200000 {
			return nil, fmt.Errorf("catalog paging did not terminate")
		}
		payload := map[string]any{"query": fmt.Sprintf(query, cloneCatalogPageSize, offset)}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v1/graphql", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		request.Header.Set("Content-Type", "application/json")
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
		_ = response.Body.Close()
		if err != nil {
			return nil, err
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, fmt.Errorf("catalog read failed with HTTP %d: %s", response.StatusCode, truncateForError(string(raw)))
		}
		var envelope struct {
			Data struct {
				Catalog []gjeval.ImportRow `json:"gj_catalog"`
			} `json:"data"`
			Errors []map[string]any `json:"errors"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return nil, fmt.Errorf("decode catalog page: %w", err)
		}
		if len(envelope.Errors) != 0 {
			return nil, fmt.Errorf("catalog read returned errors: %s", truncateForError(string(raw)))
		}
		page := envelope.Data.Catalog
		if len(page) == 0 {
			break
		}
		// A server that ignored offset would hand back the same page forever;
		// stopping on already-seen ids turns that into a complete clone of what
		// it did return rather than an endless loop.
		fresh := 0
		for _, row := range page {
			if seen[row.ID] {
				continue
			}
			seen[row.ID] = true
			rows = append(rows, row)
			fresh++
		}
		if fresh == 0 || len(page) < cloneCatalogPageSize {
			break
		}
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("the server returned no catalog cards; check the token has catalog access")
	}
	return rows, nil
}

func truncateForError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 400 {
		return value[:400]
	}
	return value
}

// cloneWorldSpec turns an imported schema into the world the renderers write.
//
// Tables are ordered parents-first so a seed script inserts a row only after
// whatever it points at exists.
func cloneWorldSpec(schema gjeval.ImportedSchema, opts cloneOptions, appName string) (worldSpec, map[string]string) {
	unmapped := map[string]string{}
	rowsPerTable := opts.Rows
	if rowsPerTable <= 0 {
		rowsPerTable = 12
	}
	parents := map[string]map[string]gjeval.ImportedRelationship{}
	for _, edge := range schema.Relationships {
		if parents[edge.FromTable] == nil {
			parents[edge.FromTable] = map[string]gjeval.ImportedRelationship{}
		}
		parents[edge.FromTable][edge.FromColumn] = edge
	}

	ordered := topoSortTables(schema, parents)
	rowCounts := map[string]int{}
	for _, table := range ordered {
		rowCounts[table.Name] = rowsPerTable
	}

	world := worldSpec{Name: appName, Domain: "clone", Seed: opts.Seed}
	for _, table := range ordered {
		out := worldTable{Name: table.Name, RowCount: rowsPerTable}
		for _, column := range table.Columns {
			mapping := mapCloneColumnType(column.Name, column.Type)
			if !mapping.Mapped {
				unmapped[table.Name+"."+column.Name] = mapping.Original
			}
			rendered := worldColumn{
				Name:     column.Name,
				Values:   column.ObservedValues,
				Nullable: !column.NotNull,
				Unique:   column.UniqueKey,
			}
			switch {
			case column.Name == table.PrimaryKey:
				rendered.Type = mapping.DDL(true) + " @id"
			case parents[table.Name] != nil && parents[table.Name][column.Name].ToTable != "":
				edge := parents[table.Name][column.Name]
				rendered.Type = mapping.DDL(column.NotNull) +
					fmt.Sprintf(" @relation(type: %s, field: %s)", edge.ToTable, edge.ToColumn)
				rendered.Ref = &worldRef{Table: edge.ToTable, Rows: rowCounts[edge.ToTable]}
			case column.UniqueKey:
				rendered.Type = mapping.DDL(column.NotNull) + " @unique"
			default:
				rendered.Type = mapping.DDL(column.NotNull)
			}
			if !mapping.Mapped {
				rendered.Note = "original type: " + mapping.Original
			}
			if out.Label == "" && rendered.Ref == nil && column.Name != table.PrimaryKey &&
				strings.HasPrefix(rendered.Type, "Text") && len(column.ObservedValues) == 0 {
				out.Label = column.Name
			}
			out.Columns = append(out.Columns, rendered)
		}
		world.Tables = append(world.Tables, out)
	}
	for _, name := range schema.FileSources {
		world.FileSources = append(world.FileSources, worldFileSource{
			Name: name, Root: filepath.Join("files", name), Files: syntheticPolicyFiles(name),
		})
	}
	return world, unmapped
}

// syntheticPolicyFiles writes the documents a cloned file source serves.
//
// Nothing here comes from the original. The catalog publishes that a document
// source exists and what it is called; it does not publish a single file name
// or a line of content, and the clone never asks for any. What these are is
// somewhere plausible for an authored policy to live, and something for a
// listing to return that is not empty.
//
// Three is enough: a task either lists what is there or opens one document, and
// both work at three. Generating more would only make the clone bigger.
func syntheticPolicyFiles(source string) []worldFile {
	subject := strings.ReplaceAll(source, "_", " ")
	return []worldFile{
		{
			Name: "policy-overview.md",
			Contents: "# Policy overview\n\nThis directory holds the written standards for " + subject +
				". Each document states one requirement, who it applies to, and when it was last reviewed.\n\n" +
				"Documents here are the authority on what is required. The database records what is " +
				"actually happening, which is not the same question.\n",
		},
		{
			Name: "operations-handbook.md",
			Contents: "# Operations handbook\n\nDay-to-day practice for the team responsible for " + subject +
				".\n\n## Escalation\n\nAnything the team cannot resolve within its own standard is raised " +
				"to the operations lead the same working day.\n\n## Records\n\nDecisions that change a " +
				"published standard are recorded here before they take effect.\n",
		},
		{
			Name: "reference-notes.md",
			Contents: "# Reference notes\n\nBackground for anyone new to " + subject +
				".\n\nThese notes explain terms used in the other documents. They do not set any " +
				"requirement themselves; where they disagree with a published standard, the standard wins.\n",
		},
	}
}

// topoSortTables orders tables so every parent is created before whatever
// references it. A cycle keeps its members in name order rather than failing —
// self-referencing schemas are ordinary, and the seed's foreign keys are drawn
// from a fixed row range regardless.
func topoSortTables(schema gjeval.ImportedSchema, parents map[string]map[string]gjeval.ImportedRelationship) []gjeval.ImportedTable {
	byName := map[string]gjeval.ImportedTable{}
	for _, table := range schema.Tables {
		byName[table.Name] = table
	}
	var ordered []gjeval.ImportedTable
	placed := map[string]bool{}
	var visit func(name string, seen map[string]bool)
	visit = func(name string, seen map[string]bool) {
		if placed[name] || seen[name] {
			return
		}
		seen[name] = true
		for _, edge := range parents[name] {
			if edge.ToTable != name {
				visit(edge.ToTable, seen)
			}
		}
		if table, ok := byName[name]; ok && !placed[name] {
			placed[name] = true
			ordered = append(ordered, table)
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		visit(name, map[string]bool{})
	}
	return ordered
}

// writeCloneExtras writes the parts of a project that are not schema or data.
func writeCloneExtras(directory string, schema gjeval.ImportedSchema, manifest cloneManifest) error {
	for _, saved := range schema.SavedQueries {
		name := strings.TrimSpace(saved.Name)
		if name == "" || strings.ContainsAny(name, `/\.`) {
			continue
		}
		path := filepath.Join(directory, "queries", name+".graphql")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(saved.Query+"\n"), 0o600); err != nil {
			return err
		}
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "clone-manifest.json"), append(encoded, '\n'), 0o600)
}

func renderCloneReadme(manifest cloneManifest, schema gjeval.ImportedSchema) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Cloned environment\n\nSchema learned from %s on %s.\n\n",
		manifest.SourceURL, manifest.GeneratedAt)
	out.WriteString("The tables, columns, keys and relationships are the source's. " +
		"**Every row here is synthetic.** No data was read from the source: the only real " +
		"values are the closed sets the catalog publishes for a column, which any agent " +
		"with catalog access already sees.\n\n")
	fmt.Fprintf(&out, "## Tables (%d)\n\n", len(schema.Tables))
	for _, table := range schema.Tables {
		fmt.Fprintf(&out, "- `%s` — %d columns, key `%s`\n", table.Name, len(table.Columns), table.PrimaryKey)
	}
	if len(manifest.Types) != 0 {
		out.WriteString("\n## Types that did not map\n\nThese became `Text`; the original is in a DDL comment.\n\n")
		keys := make([]string, 0, len(manifest.Types))
		for key := range manifest.Types {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(&out, "- `%s` was `%s`\n", key, manifest.Types[key])
		}
	}
	out.WriteString("\n## Use\n\n```bash\n" +
		"graphjin eval create --demo --path . --writable --scale 300 --composition coverage\n" +
		"graphjin env serve --path . --suite eval/suite.yml --pool 4\n```\n\n" +
		"The `--demo` flag is what provisions the schema and seeds it; `--path` alone " +
		"attaches to an existing database and cannot reset between episodes.\n")
	return out.String()
}

// runClone performs the whole clone.
func runClone(ctx context.Context, opts cloneOptions, status io.Writer) (string, error) {
	baseURL := strings.TrimSpace(opts.URL)
	headers := map[string]string{}
	if baseURL == "" {
		clientConfig, err := LoadClientConfig()
		if err != nil {
			return "", err
		}
		if clientConfig == nil || strings.TrimSpace(clientConfig.Server) == "" {
			return "", fmt.Errorf("no server configured; pass --url or run `graphjin cli setup`")
		}
		baseURL = clientConfig.Server
		if token := strings.TrimSpace(clientConfig.Token); token != "" {
			headers["Authorization"] = "Bearer " + token
		}
	}
	tokenEnv := strings.TrimSpace(opts.TokenEnv)
	if tokenEnv == "" {
		tokenEnv = "GRAPHJIN_EVAL_TOKEN"
	}
	if token := strings.TrimSpace(os.Getenv(tokenEnv)); token != "" {
		headers["Authorization"] = "Bearer " + token
	}

	client := &http.Client{Timeout: 120 * time.Second}
	rows, err := fetchCloneCatalog(ctx, client, baseURL, headers)
	if err != nil {
		return "", err
	}
	schema, report, err := gjeval.ImportSchema(rows)
	if err != nil {
		return "", err
	}

	out := strings.TrimSpace(opts.Out)
	if out == "" {
		out = "./clone"
	}
	world, unmapped := cloneWorldSpec(schema, opts, "Cloned Environment")
	manifest := cloneManifest{
		SourceURL: baseURL, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Seed: opts.Seed, Rows: world.Tables[0].RowCount, Tables: len(schema.Tables),
		Types: unmapped, Notes: report.Notes,
	}
	for _, drop := range report.Drops {
		manifest.Drops = append(manifest.Drops, fmt.Sprintf("%s %s: %s", drop.Kind, drop.ID, drop.Reason))
	}
	for _, source := range world.FileSources {
		// Said plainly in the artifact, because "we cloned your file source" is
		// exactly the sentence someone would read as "you copied our documents".
		manifest.Notes = append(manifest.Notes, fmt.Sprintf(
			"file source %s: %d documents written by this tool; no file name or file content was read from the source",
			source.Name, len(source.Files)))
	}
	if err := writeWorld(world, out); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(out, "README.md"), []byte(renderCloneReadme(manifest, schema)), 0o600); err != nil {
		return "", err
	}
	if err := writeCloneExtras(out, schema, manifest); err != nil {
		return "", err
	}
	if status != nil {
		fmt.Fprintf(status, "Cloned %d tables, %d relationships, %d saved queries from %s.\n",
			len(schema.Tables), len(schema.Relationships), len(schema.SavedQueries), baseURL)
		for _, drop := range manifest.Drops {
			fmt.Fprintf(status, "  skipped %s\n", drop)
		}
		if len(unmapped) != 0 {
			fmt.Fprintf(status, "  %d column type(s) had no mapping and became Text; see clone-manifest.json\n", len(unmapped))
		}
	}
	return out, nil
}
