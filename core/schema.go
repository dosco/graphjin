package core

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"text/tabwriter"
	"text/template"
	"unicode"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

const schemaTemplate = `
# dbinfo:{{if .Type}}{{ .Type }}{{else}}postgres{{end}},{{- .Version }},{{- .Schema }}

{{ define "schema_directive"}}
{{- if and (ne .Schema "public") (ne .Schema "")}} @schema(name: {{ .Schema }}){{end}}
{{- end}}

{{- define "relation_directive"}}
{{- if (ne .FKeyTable "")}} @relation(type: {{ .FKeyTable }}
{{- if (ne .FKeyCol "")}}, field: {{ .FKeyCol }}{{end -}}
{{- if and (ne .FKeySchema "public") (ne .FKeySchema "")}}, schema: {{ .FKeySchema }}{{end -}})
{{- end}}
{{- end}}

{{- define "function_directive"}}
{{- " @function" }}
{{- if (ne .Type "")}}(return_type: {{ .Type }}){{end}}
{{- end}}

{{- define "column_type"}}
{{- $var := .Type|dbtype }}
{{- $type := (index $var 0)|snakeToPascal }}
{{- if .Array}}[{{ $type }}]{{else}}{{ $type }}{{end}}
{{- if .NotNull}}!{{end}}
{{- "\t" }}
{{- if ne (index $var 1) ""}} @type(args: {{ (index $var 1) | printf "%q" }}){{end}}
{{- template "relation_directive" .}}
{{- end}}

{{- define "column"}}
{{ "\t" }}
{{- .Name }}:
{{- "\t"}}
{{- template "column_type" .}}
{{- if .PrimaryKey}} @id{{end}}
{{- if .UniqueKey}} @unique{{end}}
{{- if .FullText}} @search{{end}}
{{- if .Blocked}} @blocked{{end}}
{{- end}}

{{- define "func_args"}}
{{ "\t" }}
{{- .Name }}:
{{- "\t"}}
{{- $var := .Type|dbtype }}
{{- (index $var 0)|snakeToPascal }}
{{- if .Array}}[]{{end}}
{{- "\t"}}
{{- if ne (index $var 1) ""}} @type_args({{ (index $var 1) }}){{end}}
{{- end -}}

{{range .Tables -}}
type {{.Name}} 
{{- template "schema_directive" .}} {
{{- range .Columns}}{{template "column" .}}{{end}}
}

{{end -}}

{{range .Functions -}}
type {{.Name}}
{{- template "schema_directive" .}}
{{- template "function_directive" .}} {
{{- range .Inputs}}{{template "func_args" .}}{{"\t"}}@input{{end}}
{{- range .Outputs}}{{template "func_args" .}}{{"\t"}}@output{{end}}
}

{{end -}}
`

// writeSchema writes the schema to the given writer
func writeSchema(s *sdata.DBInfo, out io.Writer) (err error) {
	fn := template.FuncMap{
		"dbtype":        parseDBType,
		"snakeToPascal": snakeToPascal,
	}

	tmpl, err := template.
		New("schema").
		Funcs(fn).
		Parse(schemaTemplate)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(out, 2, 2, 2, ' ', 0)
	err = tmpl.Execute(w, s)
	if err != nil {
		return err
	}
	return
}

// snakeToPascal converts snake_case or space-separated db type names to
// PascalCase GraphQL type names.
// Examples:
//   "order_status"              -> "OrderStatus"
//   "character varying"         -> "CharacterVarying"
//   "timestamp without time zone" -> "TimestampWithoutTimeZone"
// Types without separators get only the first letter capitalized
// (e.g. "integer" -> "Integer").
func snakeToPascal(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// Treat spaces and underscores as equivalent separators so multi-word
	// db types (with spaces) become valid GraphQL type names.
	s = strings.ReplaceAll(s, " ", "_")
	parts := strings.Split(s, "_")
	var out strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		if len(r) == 0 {
			continue
		}
		out.WriteRune(unicode.ToUpper(r[0]))
		out.WriteString(strings.ToLower(string(r[1:])))
	}
	return out.String()
}

// dbTypeRe captures base type (letters, digits, underscores, spaces) and optional args in parens.
// Including underscore preserves enum and custom type names (e.g. order_status) for snakeToPascal.
var dbTypeRe = regexp.MustCompile(`([a-zA-Z0-9_ ]+)(\((.+)\))?`)

// parseDBType parses the db type string into [baseType, typeArgs].
// Base type is preserved in full so enum types like "order_status" round-trip correctly.
func parseDBType(name string) (res [2]string, err error) {
	v := dbTypeRe.FindStringSubmatch(name)
	if len(v) >= 2 {
		res[0] = strings.TrimSpace(v[1])
		if len(v) == 4 && v[3] != "" {
			res[1] = v[3]
		}
		return res, nil
	}
	return res, fmt.Errorf("invalid db type: %s", name)
}
