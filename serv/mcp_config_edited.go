package serv

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

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
