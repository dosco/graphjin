package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

const workflowMetaPrefix = "// @graphjin-workflow "

type artifactTransferOptions struct {
	Dir       string
	Kinds     []string
	DryRun    bool
	Namespace string
}

type artifactTransferSummary struct {
	DryRun    bool                   `json:"dry_run,omitempty"`
	Exported  int                    `json:"exported,omitempty"`
	Imported  int                    `json:"imported,omitempty"`
	Skipped   int                    `json:"skipped,omitempty"`
	Directory string                 `json:"directory"`
	Files     []artifactTransferFile `json:"files,omitempty"`
}

type artifactTransferFile struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type artifactExportRow struct {
	Name         string          `json:"name"`
	Kind         string          `json:"kind"`
	Path         string          `json:"path,omitempty"`
	Source       string          `json:"source,omitempty"`
	Visibility   string          `json:"visibility,omitempty"`
	ReadOnly     bool            `json:"read_only,omitempty"`
	Content      string          `json:"content,omitempty"`
	ContentJSON  json.RawMessage `json:"content_json,omitempty"`
	MetadataJSON json.RawMessage `json:"metadata_json,omitempty"`
	ContentHash  string          `json:"content_hash,omitempty"`
	Status       string          `json:"status,omitempty"`
	Revision     any             `json:"revision,omitempty"`
	CreatedAt    string          `json:"created_at,omitempty"`
	UpdatedAt    string          `json:"updated_at,omitempty"`
}

type artifactImportRow struct {
	Name         string
	Kind         string
	Content      string
	ContentJSON  json.RawMessage
	MetadataJSON json.RawMessage
	File         string
}

func mcpArtifactsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "artifacts",
		Aliases: []string{"artifact"},
		Short:   "Export and import GraphJin artifacts via the configured MCP server",
		Long: `Export and import saved queries, fragments, workflows, and watches through
the configured GraphJin MCP server.

The command uses execute_graphql, so the server must expose raw queries for
export. Import additionally requires mutations to be allowed. Auth, role
filters, and artifact ownership are all enforced by the server.`,
	}
	c.AddCommand(mcpArtifactsExportCmd())
	c.AddCommand(mcpArtifactsImportCmd())
	return c
}

func mcpArtifactsExportCmd() *cobra.Command {
	var opts artifactTransferOptions
	c := &cobra.Command{
		Use:   "export [dir]",
		Short: "Export visible artifacts to a git-friendly directory tree",
		Args:  cobra.RangeArgs(0, 1),
		Run: func(cmd *cobra.Command, args []string) {
			mcpClientRedirectLog()
			opts.Dir = artifactArgDir(args)
			summary, err := exportArtifacts(cmd.Context(), cmd, opts)
			emitArtifactTransferOrExit(summary, err)
		},
	}
	c.Flags().StringArrayVar(&opts.Kinds, "kind", nil, "Artifact kind to export (repeatable: query, fragment, workflow, watch)")
	c.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Print planned writes without creating files")
	c.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace for multi-tenant deployments")
	return c
}

func mcpArtifactsImportCmd() *cobra.Command {
	var opts artifactTransferOptions
	c := &cobra.Command{
		Use:   "import [dir]",
		Short: "Import artifacts from a git-friendly directory tree",
		Args:  cobra.RangeArgs(0, 1),
		Run: func(cmd *cobra.Command, args []string) {
			mcpClientRedirectLog()
			opts.Dir = artifactArgDir(args)
			summary, err := importArtifacts(cmd.Context(), cmd, opts)
			emitArtifactTransferOrExit(summary, err)
		},
	}
	c.Flags().StringArrayVar(&opts.Kinds, "kind", nil, "Artifact kind to import (repeatable: query, fragment, workflow, watch)")
	c.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Print planned upserts without calling the server")
	c.Flags().StringVar(&opts.Namespace, "namespace", "", "Namespace for multi-tenant deployments")
	return c
}

func artifactArgDir(args []string) string {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return "."
	}
	return args[0]
}

func emitArtifactTransferOrExit(summary artifactTransferSummary, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	b, err := json.Marshal(summary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	emitOrExit(b)
}

func exportArtifacts(ctx context.Context, cmd *cobra.Command, opts artifactTransferOptions) (artifactTransferSummary, error) {
	dir, err := filepath.Abs(artifactArgDir([]string{opts.Dir}))
	if err != nil {
		return artifactTransferSummary{}, err
	}
	kinds, err := normalizeArtifactKinds(opts.Kinds)
	if err != nil {
		return artifactTransferSummary{}, err
	}
	data, err := callArtifactGraphQL(ctx, cmd, artifactExportQuery(kinds), artifactExportVars(kinds), opts.Namespace)
	if err != nil {
		return artifactTransferSummary{}, err
	}
	var out struct {
		Artifacts []artifactExportRow `json:"gj_artifacts"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return artifactTransferSummary{}, fmt.Errorf("decode artifact export data: %w", err)
	}
	sort.SliceStable(out.Artifacts, func(i, j int) bool {
		if out.Artifacts[i].Kind == out.Artifacts[j].Kind {
			return out.Artifacts[i].Name < out.Artifacts[j].Name
		}
		return out.Artifacts[i].Kind < out.Artifacts[j].Kind
	})

	summary := artifactTransferSummary{DryRun: opts.DryRun, Directory: dir}
	for _, row := range out.Artifacts {
		row.Kind = normalizeArtifactKind(row.Kind)
		if !artifactKindAllowed(row.Kind, kinds) {
			summary.Skipped++
			continue
		}
		files, err := writeArtifactExport(dir, row, opts.DryRun)
		if err != nil {
			return summary, err
		}
		summary.Exported++
		summary.Files = append(summary.Files, files...)
	}
	return summary, nil
}

func importArtifacts(ctx context.Context, cmd *cobra.Command, opts artifactTransferOptions) (artifactTransferSummary, error) {
	dir, err := filepath.Abs(artifactArgDir([]string{opts.Dir}))
	if err != nil {
		return artifactTransferSummary{}, err
	}
	kinds, err := normalizeArtifactKinds(opts.Kinds)
	if err != nil {
		return artifactTransferSummary{}, err
	}
	rows, err := readArtifactImportRows(dir, kinds)
	if err != nil {
		return artifactTransferSummary{}, err
	}
	summary := artifactTransferSummary{DryRun: opts.DryRun, Directory: dir}
	for _, row := range rows {
		summary.Files = append(summary.Files, artifactTransferFile{
			Path: filepath.ToSlash(artifactRelPath(dir, row.File)),
			Kind: row.Kind,
			Name: row.Name,
		})
		if opts.DryRun {
			summary.Imported++
			continue
		}
		if err := upsertArtifact(ctx, cmd, row, opts.Namespace); err != nil {
			return summary, err
		}
		summary.Imported++
	}
	return summary, nil
}

func artifactExportQuery(kinds []string) string {
	fields := `name kind path source visibility read_only content content_json metadata_json content_hash status revision created_at updated_at`
	if len(kinds) == 0 {
		return fmt.Sprintf(`query { gj_artifacts(order_by: { kind: asc, name: asc }) { %s } }`, fields)
	}
	return fmt.Sprintf(`query { gj_artifacts(where: { kind: { in: $kinds } }, order_by: { kind: asc, name: asc }) { %s } }`, fields)
}

func artifactExportVars(kinds []string) map[string]any {
	if len(kinds) == 0 {
		return nil
	}
	vals := make([]any, len(kinds))
	for i, kind := range kinds {
		vals[i] = kind
	}
	return map[string]any{"kinds": vals}
}

func upsertArtifact(ctx context.Context, cmd *cobra.Command, row artifactImportRow, namespace string) error {
	input := map[string]any{
		"name":    row.Name,
		"kind":    row.Kind,
		"content": row.Content,
	}
	if v, ok, err := artifactJSONVariable(row.ContentJSON); err != nil {
		return fmt.Errorf("%s content_json: %w", row.File, err)
	} else if ok {
		input["content_json"] = v
	}
	if v, ok, err := artifactJSONVariable(row.MetadataJSON); err != nil {
		return fmt.Errorf("%s metadata_json: %w", row.File, err)
	} else if ok {
		input["metadata_json"] = v
	}
	_, err := callArtifactGraphQL(ctx, cmd, `mutation { gj_artifacts(upsert: $data) { name kind content_hash status } }`, map[string]any{"data": input}, namespace)
	if err != nil {
		return fmt.Errorf("upsert %s %q: %w", row.Kind, row.Name, err)
	}
	return nil
}

func callArtifactGraphQL(ctx context.Context, cmd *cobra.Command, query string, variables map[string]any, namespace string) (json.RawMessage, error) {
	args := map[string]any{
		"query":     query,
		"variables": variables,
		"namespace": namespace,
	}
	payload, err := callTool(ctx, cmd, "execute_graphql", cleanToolArgs(args))
	if err != nil {
		return nil, err
	}
	var result struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors,omitempty"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("decode execute_graphql result: %w", err)
	}
	if len(result.Errors) > 0 {
		msgs := make([]string, 0, len(result.Errors))
		for _, e := range result.Errors {
			if e.Message != "" {
				msgs = append(msgs, e.Message)
			}
		}
		if len(msgs) == 0 {
			msgs = append(msgs, "unknown GraphQL error")
		}
		return nil, errors.New(strings.Join(msgs, "; "))
	}
	if len(bytes.TrimSpace(result.Data)) == 0 {
		return nil, errors.New("execute_graphql returned no data")
	}
	return result.Data, nil
}

func writeArtifactExport(dir string, row artifactExportRow, dryRun bool) ([]artifactTransferFile, error) {
	name, err := artifactSafeName(row.Name)
	if err != nil {
		return nil, err
	}
	kind := normalizeArtifactKind(row.Kind)
	files := []artifactTransferFile{}
	add := func(rel, content string) error {
		files = append(files, artifactTransferFile{Path: filepath.ToSlash(rel), Kind: kind, Name: row.Name})
		if dryRun {
			return nil
		}
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(content), 0644)
	}
	switch kind {
	case "saved_query":
		if err := add(filepath.ToSlash(filepath.Join("queries", name+".gql")), row.Content); err != nil {
			return nil, err
		}
		if meta := normalizedJSON(row.MetadataJSON); len(meta) > 0 {
			if err := add(filepath.ToSlash(filepath.Join("queries", name+".json")), prettyJSON(meta)); err != nil {
				return nil, err
			}
		}
	case "fragment":
		if err := add(filepath.ToSlash(filepath.Join("fragments", name+".gql")), row.Content); err != nil {
			return nil, err
		}
		if meta := normalizedJSON(row.MetadataJSON); len(meta) > 0 {
			if err := add(filepath.ToSlash(filepath.Join("fragments", name+".json")), prettyJSON(meta)); err != nil {
				return nil, err
			}
		}
	case "workflow":
		content := ensureWorkflowMetadataHeader(row.Content, row.MetadataJSON)
		if err := add(filepath.ToSlash(filepath.Join("scripts", "workflows", name+".js")), content); err != nil {
			return nil, err
		}
	case "watch":
		body, err := watchExportJSON(row)
		if err != nil {
			return nil, err
		}
		if err := add(filepath.ToSlash(filepath.Join("watches", name+".watch.json")), string(body)); err != nil {
			return nil, err
		}
	default:
		if err := add(filepath.ToSlash(filepath.Join("artifacts", kind, name+".txt")), row.Content); err != nil {
			return nil, err
		}
		if meta := normalizedJSON(row.MetadataJSON); len(meta) > 0 {
			if err := add(filepath.ToSlash(filepath.Join("artifacts", kind, name+".json")), prettyJSON(meta)); err != nil {
				return nil, err
			}
		}
	}
	return files, nil
}

func watchExportJSON(row artifactExportRow) ([]byte, error) {
	body := map[string]any{
		"name":    row.Name,
		"kind":    normalizeArtifactKind(row.Kind),
		"content": row.Content,
	}
	if v, ok, err := artifactJSONVariable(row.ContentJSON); err != nil {
		return nil, err
	} else if ok {
		body["content_json"] = v
	}
	if v, ok, err := artifactJSONVariable(row.MetadataJSON); err != nil {
		return nil, err
	} else if ok {
		body["metadata_json"] = v
	}
	return json.MarshalIndent(body, "", "  ")
}

func readArtifactImportRows(dir string, kinds []string) ([]artifactImportRow, error) {
	var rows []artifactImportRow
	add := func(row artifactImportRow) {
		row.Kind = normalizeArtifactKind(row.Kind)
		if artifactKindAllowed(row.Kind, kinds) {
			rows = append(rows, row)
		}
	}
	if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := filepath.ToSlash(artifactRelPath(dir, path))
		base := filepath.Base(path)
		ext := strings.ToLower(filepath.Ext(base))
		switch {
		case strings.HasPrefix(rel, "queries/") && (ext == ".gql" || ext == ".graphql"):
			row, err := readTextArtifactFile(path, "saved_query")
			if err != nil {
				return err
			}
			add(row)
		case strings.HasPrefix(rel, "fragments/") && (ext == ".gql" || ext == ".graphql"):
			row, err := readTextArtifactFile(path, "fragment")
			if err != nil {
				return err
			}
			add(row)
		case strings.HasPrefix(rel, "scripts/workflows/") && ext == ".js":
			row, err := readWorkflowArtifactFile(path)
			if err != nil {
				return err
			}
			add(row)
		case strings.HasPrefix(rel, "watches/") && strings.HasSuffix(base, ".watch.json"):
			row, err := readWatchArtifactFile(path)
			if err != nil {
				return err
			}
			add(row)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Kind == rows[j].Kind {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].Kind < rows[j].Kind
	})
	return rows, nil
}

func readTextArtifactFile(path, kind string) (artifactImportRow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return artifactImportRow{}, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if _, err := artifactSafeName(name); err != nil {
		return artifactImportRow{}, err
	}
	meta, err := readOptionalJSONSidecar(strings.TrimSuffix(path, filepath.Ext(path)) + ".json")
	if err != nil {
		return artifactImportRow{}, err
	}
	return artifactImportRow{Name: name, Kind: kind, Content: string(b), MetadataJSON: meta, File: path}, nil
}

func readWorkflowArtifactFile(path string) (artifactImportRow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return artifactImportRow{}, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if _, err := artifactSafeName(name); err != nil {
		return artifactImportRow{}, err
	}
	meta := workflowMetadataFromContent(b)
	return artifactImportRow{Name: name, Kind: "workflow", Content: string(b), MetadataJSON: meta, File: path}, nil
}

func readWatchArtifactFile(path string) (artifactImportRow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return artifactImportRow{}, err
	}
	var raw struct {
		Name         string          `json:"name"`
		Kind         string          `json:"kind"`
		Content      string          `json:"content"`
		ContentJSON  json.RawMessage `json:"content_json"`
		MetadataJSON json.RawMessage `json:"metadata_json"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return artifactImportRow{}, fmt.Errorf("parse %s: %w", path, err)
	}
	name := raw.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".watch.json")
	}
	if _, err := artifactSafeName(name); err != nil {
		return artifactImportRow{}, err
	}
	kind := normalizeArtifactKind(raw.Kind)
	if kind == "artifact" {
		kind = "watch"
	}
	return artifactImportRow{
		Name:         name,
		Kind:         kind,
		Content:      raw.Content,
		ContentJSON:  normalizedJSON(raw.ContentJSON),
		MetadataJSON: normalizedJSON(raw.MetadataJSON),
		File:         path,
	}, nil
}

func readOptionalJSONSidecar(path string) (json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	raw := normalizedJSON(b)
	if len(raw) == 0 {
		return nil, nil
	}
	return raw, nil
}

func ensureWorkflowMetadataHeader(content string, meta json.RawMessage) string {
	if strings.HasPrefix(strings.TrimLeft(content, "\ufeff"), workflowMetaPrefix) {
		return content
	}
	raw := normalizedJSON(meta)
	if len(raw) == 0 {
		return content
	}
	return workflowMetaPrefix + string(raw) + "\n" + content
}

func workflowMetadataFromContent(content []byte) json.RawMessage {
	first, _, _ := bytes.Cut(content, []byte("\n"))
	first = bytes.TrimPrefix(first, []byte("\xef\xbb\xbf"))
	if !bytes.HasPrefix(first, []byte(workflowMetaPrefix)) {
		return nil
	}
	return normalizedJSON(bytes.TrimSpace(bytes.TrimPrefix(first, []byte(workflowMetaPrefix))))
}

func normalizeArtifactKinds(kinds []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(kinds))
	for _, raw := range kinds {
		for _, part := range strings.Split(raw, ",") {
			kind := normalizeArtifactKind(part)
			if kind == "artifact" && strings.TrimSpace(part) == "" {
				continue
			}
			if kind == "queries" {
				kind = "saved_query"
			}
			if kind == "watches" {
				kind = "watch"
			}
			if strings.ContainsAny(kind, `/\`) {
				return nil, fmt.Errorf("invalid artifact kind %q", part)
			}
			if !seen[kind] {
				seen[kind] = true
				out = append(out, kind)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func normalizeArtifactKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "query", "queries", "saved-query", "saved_query":
		return "saved_query"
	case "fragment", "fragments":
		return "fragment"
	case "workflow", "workflows":
		return "workflow"
	case "watch", "watches":
		return "watch"
	default:
		if strings.TrimSpace(kind) == "" {
			return "artifact"
		}
		return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(kind), "-", "_"))
	}
}

func artifactKindAllowed(kind string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	kind = normalizeArtifactKind(kind)
	for _, a := range allowed {
		if kind == a {
			return true
		}
	}
	return false
}

func artifactSafeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("invalid artifact name %q", name)
	}
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("artifact name %q cannot contain path separators", name)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("artifact name %q cannot contain control characters", name)
		}
	}
	return name, nil
}

func artifactRelPath(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return rel
}

func normalizedJSON(raw json.RawMessage) json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || strings.TrimSpace(s) == "" {
			return nil
		}
		decoded := bytes.TrimSpace([]byte(s))
		if json.Valid(decoded) {
			raw = decoded
		}
	}
	if !json.Valid(raw) {
		return nil
	}
	cp := make([]byte, len(raw))
	copy(cp, raw)
	return cp
}

func prettyJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(normalizedJSON(raw), &v); err != nil {
		return string(raw)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(b) + "\n"
}

func artifactJSONVariable(raw json.RawMessage) (any, bool, error) {
	raw = normalizedJSON(raw)
	if len(raw) == 0 {
		return nil, false, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false, err
	}
	return v, true, nil
}
