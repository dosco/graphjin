package catalog

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

const redactedValue = "[REDACTED]"

type ConfigField struct {
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	HasValue    bool   `json:"has_value"`
	Sensitive   bool   `json:"sensitive"`
	Sensitivity string `json:"sensitivity,omitempty"`
	Value       string `json:"value,omitempty"`
}

func ConfigFields(conf any) []ConfigField {
	if conf == nil {
		return nil
	}
	var out []ConfigField
	walkValue(reflect.ValueOf(conf), "", &out)
	return out
}

func ConfigFingerprint(conf any) any {
	if conf == nil {
		return nil
	}
	return configFingerprintValue(reflect.ValueOf(conf), "")
}

func configFingerprintValue(v reflect.Value, path string) any {
	if !v.IsValid() {
		return nil
	}
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}

	if sensitivity := sensitivityForName(path, ""); sensitivity != "" {
		return map[string]any{
			"sensitive": sensitivity,
			"has_value": hasValue(v),
		}
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		out := make(map[string]any)
		for i := 0; i < v.NumField(); i++ {
			sf := t.Field(i)
			if sf.PkgPath != "" {
				continue
			}
			name := fieldName(sf)
			if name == "-" || name == "" {
				continue
			}
			nextPath := name
			if path != "" {
				nextPath = path + "." + name
			}
			if sensitivity := sensitivityFromTag(sf); sensitivity != "" {
				out[name] = map[string]any{
					"sensitive": sensitivity,
					"has_value": hasValue(v.Field(i)),
				}
				continue
			}
			out[name] = configFingerprintValue(v.Field(i), nextPath)
		}
		return out
	case reflect.Map:
		if v.Len() == 0 {
			return map[string]any{}
		}
		type mapItem struct {
			key   string
			value reflect.Value
		}
		items := make([]mapItem, 0, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			items = append(items, mapItem{key: reflectValueString(iter.Key()), value: iter.Value()})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })
		out := make(map[string]any, len(items))
		for _, item := range items {
			nextPath := item.key
			if path != "" {
				nextPath = path + "." + item.key
			}
			out[item.key] = configFingerprintValue(item.value, nextPath)
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]any, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			out = append(out, configFingerprintValue(v.Index(i), fmt.Sprintf("%s.%d", path, i)))
		}
		return out
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.String:
		return v.String()
	default:
		return reflectValueString(v)
	}
}

func reflectValueString(v reflect.Value) string {
	if !v.IsValid() {
		return ""
	}
	if v.CanInterface() {
		return fmt.Sprint(v.Interface())
	}
	return fmt.Sprint(v)
}

func walkValue(v reflect.Value, path string, out *[]ConfigField) {
	if !v.IsValid() {
		return
	}
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			if path != "" {
				*out = append(*out, ConfigField{Path: path, Kind: v.Kind().String()})
			}
			return
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			sf := t.Field(i)
			if sf.PkgPath != "" {
				continue
			}
			name := fieldName(sf)
			if name == "-" || name == "" {
				continue
			}
			nextPath := name
			if path != "" {
				nextPath = path + "." + name
			}
			walkConfigField(v.Field(i), sf, nextPath, out)
		}
	case reflect.Map:
		*out = append(*out, ConfigField{Path: path, Kind: v.Kind().String(), HasValue: v.Len() != 0})
	case reflect.Slice, reflect.Array:
		*out = append(*out, ConfigField{Path: path, Kind: v.Kind().String(), HasValue: v.Len() != 0})
	default:
		*out = append(*out, scalarConfigField(path, v, sensitivityForName(path, "")))
	}
}

func walkConfigField(v reflect.Value, sf reflect.StructField, path string, out *[]ConfigField) {
	sensitivity := sensitivityFromTag(sf)
	if sensitivity == "" {
		sensitivity = sensitivityForName(path, sf.Name)
	}

	if sensitivity != "" {
		*out = append(*out, scalarConfigField(path, v, sensitivity))
		return
	}

	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			*out = append(*out, ConfigField{Path: path, Kind: v.Kind().String()})
			return
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		walkValue(v, path, out)
	case reflect.Map:
		if looksSensitiveName(path) {
			*out = append(*out, scalarConfigField(path, v, sensitivityForName(path, sf.Name)))
			return
		}
		*out = append(*out, ConfigField{Path: path, Kind: v.Kind().String(), HasValue: v.Len() != 0})
	case reflect.Slice, reflect.Array:
		*out = append(*out, ConfigField{Path: path, Kind: v.Kind().String(), HasValue: v.Len() != 0})
	default:
		*out = append(*out, scalarConfigField(path, v, ""))
	}
}

func scalarConfigField(path string, v reflect.Value, sensitivity string) ConfigField {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return ConfigField{Path: path, Kind: v.Kind().String(), Sensitive: sensitivity != "", Sensitivity: sensitivity}
		}
		v = v.Elem()
	}

	field := ConfigField{
		Path:        path,
		Sensitive:   sensitivity != "",
		Sensitivity: sensitivity,
	}
	if !v.IsValid() {
		return field
	}
	field.Kind = v.Kind().String()
	field.HasValue = hasValue(v)
	if field.Sensitive {
		if field.HasValue {
			field.Value = redactedValue
		}
		return field
	}
	if field.HasValue && v.CanInterface() {
		field.Value = fmt.Sprint(v.Interface())
	}
	return field
}

func hasValue(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}
	switch v.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() != 0
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return v.Float() != 0
	case reflect.Pointer, reflect.Interface:
		return !v.IsNil()
	default:
		return !v.IsZero()
	}
}

func fieldName(sf reflect.StructField) string {
	for _, tagName := range []string{"mapstructure", "json", "yaml"} {
		tag := sf.Tag.Get(tagName)
		if tag == "" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name != "" {
			return name
		}
	}
	return strings.ToLower(sf.Name[:1]) + sf.Name[1:]
}

func sensitivityFromTag(sf reflect.StructField) string {
	for _, tagName := range []string{"graphjin", "jsonschema_extras"} {
		tag := sf.Tag.Get(tagName)
		if tag == "" {
			continue
		}
		for _, part := range strings.Split(tag, ",") {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "sensitive=") {
				return strings.TrimPrefix(part, "sensitive=")
			}
			if strings.HasPrefix(part, "x-graphjin-sensitive=") {
				return strings.TrimPrefix(part, "x-graphjin-sensitive=")
			}
		}
	}
	return ""
}

func sensitivityForName(path, field string) string {
	name := strings.ToLower(path + "." + field)
	switch {
	case strings.Contains(name, "connection_string"), strings.Contains(name, "connstring"):
		return "connection"
	case strings.Contains(name, "private_key_pem"), strings.Contains(name, "client_key"):
		return "key_material"
	case strings.Contains(name, "private_key_path"):
		return "secret_ref"
	case strings.Contains(name, "key_passphrase"), strings.Contains(name, "password"), strings.Contains(name, "secret_key"), strings.Contains(name, "client_secret"):
		return "secret"
	case strings.Contains(name, "token"):
		return "token"
	case strings.Contains(name, "server_cert"), strings.Contains(name, "client_cert"), strings.Contains(name, "certificate"):
		return "certificate"
	default:
		return ""
	}
}

func looksSensitiveName(path string) bool {
	return sensitivityForName(path, "") != ""
}
