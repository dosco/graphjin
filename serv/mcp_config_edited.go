package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3/internal/mcpcompat/mcp"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

// editedConfigKey marks the context of an EditConfig call, the only caller
// whose update may carry config_yaml.
type editedConfigKey struct{}

// EditConfig checks the full text of an edited config file or, with apply,
// applies it live, through the same staged update as gj_config. It is for
// the host that runs GraphJin in its own process and owns the file: no
// GraphQL or MCP request can send config_yaml, since replacing the whole
// configuration is a step for whoever can write the file, not for whoever
// holds a token. The result is gj_config's update result. GraphJin does not
// write the file; the caller does once an apply succeeds.
func (s1 *HttpService) EditConfig(ctx context.Context, text string, apply bool) map[string]any {
	ms := &mcpServer{service: s1.Load().(*graphjinService), ctx: ctx}
	ctx = context.WithValue(ctx, editedConfigKey{}, true)
	revision := ms.currentConfigCatalogRevision(ctx)
	run := func(mode, previewID string) map[string]any {
		args := map[string]any{"config_yaml": text, "mode": mode}
		if revision != "" {
			args["expected_catalog_revision"] = revision
		}
		if previewID != "" {
			args["preview_id"] = previewID
		}
		res, err := ms.handleUpdateCurrentConfig(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}})
		return mcpResultPayload(res, err)
	}
	if !apply {
		return run("validate", "")
	}
	if !ms.service.conf.Core.IsSourcesUsed() {
		return run("apply", "")
	}
	preview := run("preview", "")
	previewID, _ := preview["preview_id"].(string)
	if preview["success"] == false || preview["valid"] != true || previewID == "" {
		// Invalid, or valid with nothing to change.
		return preview
	}
	return run("apply", previewID)
}

// editedConfigArgs are the update arguments config_yaml may be combined with.
var editedConfigArgs = map[string]bool{
	"config_yaml": true, "mode": true, "preview_id": true, "expected_catalog_revision": true, "id": true,
}

// editedConfig stages config_yaml, the full text of an edited config file,
// as the next configuration. The core sections (sources, databases, tables,
// roles and the rest) are read from the text exactly as the file would be at
// startup, so they reload live like reload_from_disk. Server settings that
// differ from the file on disk become a serv patch, so they apply live or
// report that they wait for a restart; any other changed server setting is
// refused, since it could only take effect on a restart nobody asked for.
// The caller writes the file once the update applies.
func (ms *mcpServer) editedConfig(args map[string]any, text string) (core.Config, map[string]any, map[string]any, error) {
	for key := range args {
		if !editedConfigArgs[key] {
			return core.Config{}, nil, nil, fmt.Errorf("cannot be combined with %s", key)
		}
	}
	v := ms.service.conf.viper
	if v == nil || v.ConfigFileUsed() == "" {
		return core.Config{}, nil, nil, fmt.Errorf("config file not available")
	}
	path := v.ConfigFileUsed()

	edited := map[string]any{}
	if err := yaml.Unmarshal([]byte(text), &edited); err != nil {
		return core.Config{}, nil, nil, fmt.Errorf("the config is not valid YAML: %w", err)
	}
	if edited == nil {
		return core.Config{}, nil, nil, fmt.Errorf("the config is empty")
	}
	onDisk := map[string]any{}
	if raw, err := os.ReadFile(path); err != nil {
		return core.Config{}, nil, nil, err
	} else if err := yaml.Unmarshal(raw, &onDisk); err != nil {
		return core.Config{}, nil, nil, fmt.Errorf("the config file on disk is not valid YAML: %w", err)
	}

	// Read the text as the config file, with any file it inherits from
	// still read from disk.
	overlay := afero.NewMemMapFs()
	if err := afero.WriteFile(overlay, path, []byte(text), 0o600); err != nil {
		return core.Config{}, nil, nil, err
	}
	disk, err := readInConfig(path, afero.NewCopyOnWriteFs(afero.NewReadOnlyFs(afero.NewOsFs()), overlay))
	if err != nil {
		return core.Config{}, nil, nil, err
	}
	disk.managedArtifactStore = ms.service.conf.managedArtifactStore
	applySourceCapabilitySourceDefaults(disk)
	if err := normalizeServiceSources(disk); err != nil {
		return core.Config{}, nil, nil, err
	}
	if disk.DBType == "" {
		disk.DBType = ms.service.conf.DBType
	}
	if disk.Auth.Type == "" || disk.Auth.Type == "none" {
		disk.DefaultBlock = false
	}
	disk.CacheTrackingEnabled = ms.service.conf.CacheTrackingEnabled

	coreKeys := coreConfigKeys()
	var servPatch, mcpPatch map[string]any
	var refused []string
	for _, key := range unionKeys(edited, onDisk) {
		before, after := onDisk[key], edited[key]
		if reflect.DeepEqual(before, after) || coreKeys[key] {
			continue
		}
		switch {
		case after == nil:
			// A server setting cannot be unset live; its default would only
			// return on a restart.
			refused = append(refused, key)
		case key == "mcp":
			next, ok := jsonShaped(after).(map[string]any)
			if !ok {
				return core.Config{}, nil, nil, fmt.Errorf("mcp must be an object")
			}
			mcpPatch = next
		case servWritableReload[key] != "":
			if servPatch == nil {
				servPatch = map[string]any{}
			}
			servPatch[key] = jsonShaped(after)
		default:
			refused = append(refused, key)
		}
	}
	if len(refused) > 0 {
		return core.Config{}, nil, nil, fmt.Errorf("%s cannot be changed while GraphJin runs; only the data configuration, mcp and %s can",
			strings.Join(refused, ", "), strings.Join(sortedKeys(servWritableReload), ", "))
	}
	return cloneCoreConfig(disk.Core), servPatch, mcpPatch, nil
}

// coreConfigKeys are the top-level config keys read into core.Config.
func coreConfigKeys() map[string]bool {
	keys := map[string]bool{}
	var walk func(t reflect.Type)
	walk = func(t reflect.Type) {
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			name, opts, _ := strings.Cut(field.Tag.Get("mapstructure"), ",")
			if strings.Contains(opts, "squash") && field.Type.Kind() == reflect.Struct {
				walk(field.Type)
				continue
			}
			if name == "-" {
				continue
			}
			// Untagged fields match their lowercased name, as mapstructure
			// matches keys case-insensitively.
			keys[strings.ToLower(field.Name)] = true
			for _, tag := range []string{name, field.Tag.Get("yaml"), field.Tag.Get("json")} {
				if tag, _, _ = strings.Cut(tag, ","); tag != "" && tag != "-" {
					keys[tag] = true
				}
			}
		}
	}
	walk(reflect.TypeOf(core.Config{}))
	return keys
}

func unionKeys(a, b map[string]any) []string {
	seen := map[string]bool{}
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// jsonShaped gives a YAML value the shape a JSON update argument has, so it
// goes through the same validation.
func jsonShaped(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}
