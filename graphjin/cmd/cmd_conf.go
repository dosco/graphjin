package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// `graphjin config` is the offline, local-file counterpart to the server-backed
// `graphjin cli config` group. It reads and edits the config files on disk
// without contacting a running server, so it works before the server can even
// start. Edits preserve comments and formatting (yaml.v3 node AST) and are
// validated through the real loader before anything is written.

var (
	confFile   string // --file override for the target config file
	confRawOut bool   // --raw: do not redact sensitive values in get/explain
)

func configCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Inspect and edit local GraphJin config files (offline)",
		Long: `Read and edit the GraphJin config files on disk without a running server.

The target file defaults to <path>/<env>.yml where <env> comes from GO_ENV
(dev, prod, or agentic; dev when unset) and <path> is the global --path flag.
Use --file to point at a specific config file.

Examples:
  graphjin config get rate_limiter.rate
  graphjin config set log_level debug
  graphjin config unset rate_limiter.bucket
  graphjin config explain rate_limiter.rate
  graphjin config validate
  graphjin config schema > config/config.schema.json
  graphjin config docs prod`,
	}
	c.PersistentFlags().StringVar(&confFile, "file", "", "config file to operate on (default: <path>/<env>.yml)")

	c.AddCommand(configGetCmd())
	c.AddCommand(configSetCmd())
	c.AddCommand(configUnsetCmd())
	c.AddCommand(configExplainCmd())
	c.AddCommand(configValidateCmd())
	c.AddCommand(configSchemaCmd())
	c.AddCommand(configDocsCmd())
	return c
}

// configTargetFile resolves which file config edits/reads operate on.
func configTargetFile() string {
	if confFile != "" {
		abs, err := filepath.Abs(confFile)
		if err != nil {
			log.Fatalf("invalid --file: %s", err)
		}
		return abs
	}
	cp, err := filepath.Abs(cpath)
	if err != nil {
		log.Fatalf("invalid --path: %s", err)
	}
	name := serv.GetConfigName()
	for _, ext := range []string{".yml", ".yaml", ".json"} {
		p := filepath.Join(cp, name+ext)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join(cp, name+".yml")
}

// loadEffective loads the target config through the full pipeline (defaults,
// inheritance, env overrides) and returns it plus its effective settings map.
func loadEffective() (*serv.Config, map[string]any) {
	target := configTargetFile()
	conf, err := serv.ReadInConfig(target)
	if err != nil {
		log.Fatalf("failed to read config %s: %s", target, err)
	}
	return conf, conf.EffectiveSettings()
}

func configGetCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "get <dotted.path>",
		Short: "Print the effective value of a config key",
		Long:  "Print the value of a config key after defaults, inheritance, and GJ_/SJ_ env overrides are applied.",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			_, settings := loadEffective()
			key := strings.ToLower(args[0])
			val, ok := lookupDotted(settings, key)
			if !ok {
				log.Fatalf("no such config key: %s", args[0])
			}
			if !confRawOut && isSensitiveDottedKey(key) {
				fmt.Println("[REDACTED] (use --raw to reveal)")
				return
			}
			fmt.Println(renderValue(val))
		},
	}
	c.Flags().BoolVar(&confRawOut, "raw", false, "reveal sensitive values instead of redacting them")
	return c
}

func configExplainCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "explain <dotted.path>",
		Short: "Explain a config key: value, where it comes from, scope, and docs",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			key := strings.ToLower(args[0])
			conf, settings := loadEffective()
			val, ok := lookupDotted(settings, key)

			scope, reload := serv.ConfigKeyScope(key)
			fmt.Printf("key:      %s\n", key)
			if ok {
				shown := renderValue(val)
				if !confRawOut && isSensitiveDottedKey(key) {
					shown = "[REDACTED]"
				}
				fmt.Printf("value:    %s\n", shown)
			} else {
				fmt.Printf("value:    (unset — using built-in default)\n")
			}
			fmt.Printf("scope:    %s\n", scope)
			if reload != "" {
				fmt.Printf("reload:   %s\n", reload)
			}
			fmt.Printf("source:   %s\n", explainProvenance(conf, key))

			if title, desc := schemaDocFor(key); title != "" || desc != "" {
				fmt.Println("docs:")
				if title != "" {
					fmt.Printf("  %s\n", title)
				}
				if desc != "" {
					fmt.Printf("  %s\n", desc)
				}
			}
		},
	}
	c.Flags().BoolVar(&confRawOut, "raw", false, "reveal sensitive values instead of redacting them")
	return c
}

func configSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <dotted.path> <value>",
		Short: "Set a config key in the target file (preserves comments)",
		Long:  "Set a config key. The value is parsed as YAML (so numbers, booleans, and lists work). The change is validated through the real loader before the file is written.",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			editConfigFile(strings.ToLower(args[0]), args[1], false)
		},
	}
}

func configUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <dotted.path>",
		Short: "Remove a config key from the target file (preserves comments)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			editConfigFile(strings.ToLower(args[0]), "", true)
		},
	}
}

func configValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the local config files (offline, no connectivity)",
		Long:  "Load and structurally validate the config files without connecting to any database. Complements `graphjin serve test`, which also checks connectivity.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			target := configTargetFile()
			conf, err := serv.ReadInConfig(target)
			if err != nil {
				log.Fatalf("config invalid: %s", err)
			}
			if err := validateCoreOffline(conf); err != nil {
				log.Fatalf("config invalid: %s", err)
			}
			fmt.Printf("ok: %s is valid\n", target)
		},
	}
}

func configSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the config JSON Schema (for editors and tooling)",
		Long:  "Print the JSON Schema for the GraphJin config file. Redirect it next to your config and reference it with a `# yaml-language-server: $schema=...` modeline for editor autocomplete.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			os.Stdout.Write(serv.ConfigJSONSchema())
		},
	}
}

func configDocsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "docs [variant]",
		Short: "Print an annotated example config (dev, prod, or agentic)",
		Long:  "Print an inline-documented example config template — the same files `graphjin serve new` scaffolds. Variant is dev (default), prod, or agentic.",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			variant := "dev"
			if len(args) == 1 {
				variant = args[0]
			}
			fmt.Print(serv.ConfigDocsTemplate(variant))
		},
	}
}

// editConfigFile applies a comment-preserving set/unset to the target file,
// validates the result through the real loader on an in-memory copy of the
// config directory, and only then writes to disk.
func editConfigFile(key, rawValue string, unset bool) {
	target := configTargetFile()
	if strings.EqualFold(filepath.Ext(target), ".json") {
		log.Fatalf("config set/unset only supports YAML files; %s is JSON", target)
	}

	orig, err := os.ReadFile(target)
	if err != nil {
		log.Fatalf("cannot read %s: %s", target, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(orig, &doc); err != nil {
		log.Fatalf("cannot parse %s: %s", target, err)
	}
	if doc.Kind == 0 { // empty file → start a fresh document/mapping
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}

	path := strings.Split(key, ".")
	if unset {
		if !unsetYAMLPath(rootMapping(&doc), path) {
			log.Fatalf("key not present in %s: %s", target, key)
		}
	} else {
		setYAMLPath(rootMapping(&doc), path, rawValue)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		log.Fatalf("cannot encode config: %s", err)
	}
	enc.Close()
	updated := buf.Bytes()

	// Hard gate: the edited file must still load (catches YAML syntax and type
	// errors the edit might introduce). Semantic validation is a soft warning —
	// a scaffold mid-configuration (e.g. database source still commented out)
	// is intentionally incomplete, and blocking edits on it would be hostile.
	loaded, err := loadEditedConfig(target, updated)
	if err != nil {
		log.Fatalf("refusing to write: edit would make %s unloadable: %s", target, err)
	}

	if err := os.WriteFile(target, updated, 0o600); err != nil {
		log.Fatalf("cannot write %s: %s", target, err)
	}
	action := "set"
	if unset {
		action = "unset"
	}
	if verr := validateCoreOffline(loaded); verr != nil {
		log.Warnf("%s %s in %s (config not fully valid yet: %s)", action, key, target, verr)
		return
	}
	log.Infof("%s %s in %s", action, key, target)
}

// loadEditedConfig copies the config directory into an in-memory fs, overlays
// the edited file, and runs the real loader so inheritance and env handling
// match production behavior exactly — without writing to disk.
func loadEditedConfig(target string, updated []byte) (*serv.Config, error) {
	dir := filepath.Dir(target)
	memFs := afero.NewMemMapFs()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".yml", ".yaml", ".json":
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			if err := afero.WriteFile(memFs, filepath.Join(dir, e.Name()), b, 0o600); err != nil {
				return nil, err
			}
		}
	}
	if err := afero.WriteFile(memFs, target, updated, 0o600); err != nil {
		return nil, err
	}
	return serv.ReadInConfigFS(target, memFs)
}

// validateCoreOffline runs the same normalize-then-validate sequence the engine
// uses at startup (see core initConfig), so offline validation matches how the
// server would actually load the config — without any database connection.
func validateCoreOffline(conf *serv.Config) error {
	if err := conf.Core.ValidateIsSourcesUsed(); err != nil {
		return err
	}
	if err := conf.Core.NormalizeSources(); err != nil {
		return err
	}
	return conf.Core.Validate()
}

// --- YAML node helpers (comment-preserving) ---

func rootMapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			m := &yaml.Node{Kind: yaml.MappingNode}
			doc.Content = []*yaml.Node{m}
			return m
		}
		return doc.Content[0]
	}
	return doc
}

// setYAMLPath sets a scalar (parsed from YAML source) at the dotted path,
// creating intermediate mappings as needed and preserving sibling comments.
func setYAMLPath(m *yaml.Node, path []string, rawValue string) {
	key := path[0]
	if len(path) == 1 {
		valNode := &yaml.Node{}
		if err := yaml.Unmarshal([]byte(rawValue), valNode); err != nil || len(valNode.Content) == 0 {
			// Fall back to a plain string scalar.
			valNode = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: rawValue}
		} else {
			valNode = valNode.Content[0]
		}
		if existing := mappingValue(m, key); existing != nil {
			*existing = *valNode
			return
		}
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, valNode)
		return
	}
	child := mappingValue(m, key)
	if child == nil || child.Kind != yaml.MappingNode {
		newChild := &yaml.Node{Kind: yaml.MappingNode}
		if child == nil {
			m.Content = append(m.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, newChild)
		} else {
			*child = *newChild
		}
		child = newChild
	}
	setYAMLPath(child, path[1:], rawValue)
}

// unsetYAMLPath removes the key at the dotted path. Returns false if absent.
func unsetYAMLPath(m *yaml.Node, path []string) bool {
	key := path[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			if len(path) == 1 {
				m.Content = append(m.Content[:i], m.Content[i+2:]...)
				return true
			}
			if m.Content[i+1].Kind == yaml.MappingNode {
				return unsetYAMLPath(m.Content[i+1], path[1:])
			}
			return false
		}
	}
	return false
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// --- lookups, provenance, docs ---

func lookupDotted(settings map[string]any, key string) (any, bool) {
	parts := strings.Split(key, ".")
	var cur any = settings
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[p]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// explainProvenance reports which layer supplies a key's effective value:
// a GJ_/SJ_ environment override, the target file, its inherited parent, or a
// built-in default.
func explainProvenance(conf *serv.Config, key string) string {
	envKey := "GJ_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	if _, ok := os.LookupEnv(envKey); ok {
		return "environment variable " + envKey
	}
	sjKey := "SJ_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	if _, ok := os.LookupEnv(sjKey); ok {
		return "environment variable " + sjKey
	}

	target := configTargetFile()
	if raw := readRawYAML(target); raw != nil {
		if _, ok := lookupDotted(raw, key); ok {
			return "config file " + filepath.Base(target)
		}
	}
	if parent := inheritedParentFile(conf, target); parent != "" {
		if raw := readRawYAML(parent); raw != nil {
			if _, ok := lookupDotted(raw, key); ok {
				return "inherited config file " + filepath.Base(parent)
			}
		}
	}
	return "built-in default"
}

func inheritedParentFile(conf *serv.Config, target string) string {
	if conf == nil || strings.TrimSpace(conf.Inherits) == "" {
		return ""
	}
	name := conf.Inherits
	if filepath.Ext(name) == "" {
		name += filepath.Ext(target)
	}
	return filepath.Join(filepath.Dir(target), name)
}

func readRawYAML(path string) map[string]any {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := yaml.Unmarshal(b, &out); err != nil {
		return nil
	}
	return lowerKeys(out)
}

// lowerKeys recursively lower-cases map keys to match viper's canonical form.
func lowerKeys(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		lk := strings.ToLower(k)
		if child, ok := val.(map[string]any); ok {
			out[lk] = lowerKeys(child)
		} else {
			out[lk] = val
		}
	}
	return out
}

func renderValue(v any) string {
	switch v.(type) {
	case map[string]any, []any:
		b, err := yaml.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return strings.TrimRight(string(b), "\n")
	default:
		return fmt.Sprintf("%v", v)
	}
}

func isSensitiveDottedKey(key string) bool {
	last := key
	if i := strings.LastIndex(key, "."); i >= 0 {
		last = key[i+1:]
	}
	for _, s := range []string{"password", "secret", "token", "passphrase", "private_key", "client_key", "api_key", "apikey", "authorization", "cookie", "connection_string"} {
		if strings.Contains(last, s) {
			return true
		}
	}
	return false
}

func schemaDocFor(key string) (title, description string) {
	head := strings.SplitN(key, ".", 2)[0]
	schema := parseSchemaProperties()
	prop, ok := schema[head]
	if !ok {
		return "", ""
	}
	m, ok := prop.(map[string]any)
	if !ok {
		return "", ""
	}
	if t, ok := m["title"].(string); ok {
		title = t
	}
	if d, ok := m["description"].(string); ok {
		description = d
	}
	return title, description
}

func parseSchemaProperties() map[string]any {
	var schema struct {
		Properties map[string]any `yaml:"properties"`
	}
	// The embedded schema is JSON, which yaml.v3 parses as a superset.
	if err := yaml.Unmarshal(serv.ConfigJSONSchema(), &schema); err != nil {
		return map[string]any{}
	}
	return schema.Properties
}
