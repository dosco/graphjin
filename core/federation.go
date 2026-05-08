package core

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

// FederationConfig configures Apollo Federation v2 subgraph emission.
//
// When Enabled is true the engine recognises the federation queries
// `_service { sdl }` and `_entities(representations: [_Any!]!)` on the
// root Query type and emits a federation-flavoured SDL describing every
// non-blocked table in the schema.
//
// Keys lets you override the auto-derived `@key(fields: "<pk>")` directive
// for a table — useful when a logical entity uses a non-PK key (e.g.
// `email`) or a composite key (e.g. `["id", "tenant_id"]`).
//
// Shareable, Inaccessible, and Tags accept fully-qualified field names
// formatted as "Type.field" (e.g. "User.email").
type FederationConfig struct {
	Enabled bool `mapstructure:"enabled" json:"enabled" yaml:"enabled" jsonschema:"title=Enable Apollo Federation v2,default=false"`

	// Version is the Federation spec version surfaced via @link.
	// Defaults to "v2.5" when empty.
	Version string `mapstructure:"version" json:"version" yaml:"version" jsonschema:"title=Federation Version,default=v2.5"`

	// Keys overrides the auto-derived @key for the named table. Each
	// entry is a list of column names that compose the key.
	Keys map[string][]string `mapstructure:"keys" json:"keys" yaml:"keys" jsonschema:"title=Federation Keys"`

	// Shareable, Inaccessible and Tags accept "Type.field" identifiers.
	Shareable    []string            `mapstructure:"shareable" json:"shareable" yaml:"shareable" jsonschema:"title=Shareable Fields"`
	Inaccessible []string            `mapstructure:"inaccessible" json:"inaccessible" yaml:"inaccessible" jsonschema:"title=Inaccessible Fields"`
	Tags         map[string][]string `mapstructure:"tags" json:"tags" yaml:"tags" jsonschema:"title=@tag Annotations"`
}

const defaultFederationVersion = "v2.5"

// federationImports lists the directives we always import via @link.
// Keeping this fixed avoids accidentally claiming directives we don't
// actually emit (Apollo Router validates the import set).
var federationImports = []string{
	"@key",
	"@shareable",
	"@external",
	"@requires",
	"@provides",
	"@inaccessible",
	"@tag",
}

// BuildFederationSDL emits a Federation v2 subgraph SDL for the supplied
// schema. The output is intentionally compact and deterministic so it
// can be diffed across schema reloads and supergraph compositions.
//
// Tables marked Blocked, alias-only tables, and tables with no primary
// key are skipped — Apollo entities require a @key directive.
func BuildFederationSDL(schema *sdata.DBSchema, conf FederationConfig) (string, error) {
	if schema == nil {
		return "", errors.New("federation: schema is nil")
	}

	version := conf.Version
	if version == "" {
		version = defaultFederationVersion
	}

	type entity struct {
		Type    string
		Table   sdata.DBTable
		KeyCols []string // GraphQL field names (not necessarily DB column names)
	}

	tables := schema.GetTables()
	entities := make([]entity, 0, len(tables))
	for _, t := range tables {
		if t.Blocked || t.Type == "remote" {
			continue
		}
		if t.Name == "" {
			continue
		}
		// @key requires at least one identifying column.
		keyCols := federationKeyCols(t, conf.Keys)
		if len(keyCols) == 0 {
			continue
		}
		entities = append(entities, entity{
			Type:    federationTypeName(t.Name),
			Table:   t,
			KeyCols: keyCols,
		})
	}
	sort.Slice(entities, func(i, j int) bool { return entities[i].Type < entities[j].Type })

	shareable := indexFQFields(conf.Shareable)
	inaccessible := indexFQFields(conf.Inaccessible)
	tags := indexFQFieldTags(conf.Tags)

	var sb strings.Builder
	fmt.Fprintf(&sb,
		`extend schema @link(url: "https://specs.apollo.dev/federation/%s", import: [%s])`+"\n\n",
		version,
		quoteJoin(federationImports),
	)

	for _, e := range entities {
		fmt.Fprintf(&sb, "type %s @key(fields: %q) {\n", e.Type, strings.Join(e.KeyCols, " "))
		for _, col := range visibleColumns(e.Table) {
			fieldName := col.Name
			gqlType := mapColumnType(col)
			line := fmt.Sprintf("  %s: %s", fieldName, gqlType)
			fq := e.Type + "." + fieldName
			if _, ok := shareable[fq]; ok {
				line += " @shareable"
			}
			if _, ok := inaccessible[fq]; ok {
				line += " @inaccessible"
			}
			for _, tag := range tags[fq] {
				line += fmt.Sprintf(" @tag(name: %q)", tag)
			}
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		sb.WriteString("}\n\n")
	}

	if len(entities) == 0 {
		// Apollo requires at least one type. An empty subgraph is a
		// configuration error worth surfacing rather than silently
		// emitting an unusable schema.
		return "", errors.New("federation: no tables eligible for @key — every table is blocked, remote, or missing a primary key")
	}

	// Federation built-ins. The _Any scalar carries the entity reference;
	// _Service exposes the subgraph SDL; _Entity unions every @key type.
	sb.WriteString("scalar _Any\n\n")
	sb.WriteString("type _Service {\n  sdl: String!\n}\n\n")

	sb.WriteString("union _Entity = ")
	for i, e := range entities {
		if i > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(e.Type)
	}
	sb.WriteString("\n\n")

	sb.WriteString("extend type Query {\n")
	sb.WriteString("  _service: _Service!\n")
	sb.WriteString("  _entities(representations: [_Any!]!): [_Entity]!\n")
	sb.WriteString("}\n")

	return sb.String(), nil
}

// federationKeyCols resolves the @key column list for a table. An
// explicit override in conf wins; otherwise the table's primary
// column(s) are used. Returns an empty slice when no key is available.
func federationKeyCols(t sdata.DBTable, overrides map[string][]string) []string {
	if cols, ok := overrides[t.Name]; ok && len(cols) > 0 {
		return cols
	}
	if len(t.PrimaryCols) > 0 {
		out := make([]string, 0, len(t.PrimaryCols))
		for _, c := range t.PrimaryCols {
			out = append(out, c.Name)
		}
		return out
	}
	if t.PrimaryCol.Name != "" {
		return []string{t.PrimaryCol.Name}
	}
	return nil
}

// visibleColumns returns the table columns that should appear in SDL —
// non-blocked columns, deterministic order. This intentionally mirrors
// the existing GraphQL surface: anything the engine refuses to query
// shouldn't appear in the subgraph either.
func visibleColumns(t sdata.DBTable) []sdata.DBColumn {
	out := make([]sdata.DBColumn, 0, len(t.Columns))
	for _, c := range t.Columns {
		if c.Blocked || c.Name == "" {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// federationTypeName converts a snake_case table name into the
// PascalCase type name typical for GraphQL schemas. We keep this
// deterministic rather than honoring the engine's CamelCase config
// because federation supergraphs need stable names.
func federationTypeName(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// mapColumnType produces a best-effort GraphQL type for a DB column.
// The mapping is intentionally narrow — federation needs ENOUGH type
// info to compose a supergraph, not a perfect round-trip. Unknown
// types fall back to String.
func mapColumnType(c sdata.DBColumn) string {
	gql := "String"
	switch strings.ToLower(c.Type) {
	case "bigint", "smallint", "int", "int2", "int4", "int8", "integer":
		gql = "Int"
	case "numeric", "decimal", "real", "double", "double precision", "float", "float4", "float8":
		gql = "Float"
	case "boolean", "bool":
		gql = "Boolean"
	case "json", "jsonb":
		gql = "JSON"
	case "uuid":
		gql = "ID"
	}
	if c.PrimaryKey {
		gql = "ID"
	}
	if c.NotNull {
		gql += "!"
	}
	return gql
}

// indexFQFields turns ["User.email", "Order.total"] into a lookup set.
func indexFQFields(in []string) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for _, fq := range in {
		out[fq] = struct{}{}
	}
	return out
}

func indexFQFieldTags(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for fq, tags := range in {
		dup := make([]string, len(tags))
		copy(dup, tags)
		sort.Strings(dup)
		out[fq] = dup
	}
	return out
}

func quoteJoin(s []string) string {
	parts := make([]string, len(s))
	for i, v := range s {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return strings.Join(parts, ", ")
}

// errFederationEntitiesNotImplemented is returned for `_entities` queries
// until the resolver lands. Apollo Router will treat this as a partial
// subgraph — composition succeeds and queries that don't need
// cross-subgraph entity resolution still work end-to-end.
var errFederationEntitiesNotImplemented = errors.New(
	"federation: _entities resolver is not yet implemented; this subgraph supports composition and _service{sdl} queries",
)

// handleFederationQuery checks if the request is a federation built-in
// (`_service` or `_entities`) and produces a JSON response. Returns
// handled=false when the query is a regular GraphQL document and should
// flow through the normal pipeline.
//
// Detection is intentionally a substring scan over the raw query text:
// federation root fields begin with an underscore — a character that
// cannot appear at the start of a normal GraphQL field name in this
// codebase — so false positives only occur for queries that embed the
// literal "_service" or "_entities" inside a string variable, which is
// vanishingly rare and acceptable for an opt-in mode.
func (gj *graphjinEngine) handleFederationQuery(r GraphqlReq) (handled bool, data []byte, err error) {
	q := r.query
	if len(q) == 0 {
		return false, nil, nil
	}
	hasService := bytesContainsToken(q, []byte("_service"))
	hasEntities := bytesContainsToken(q, []byte("_entities"))
	if !hasService && !hasEntities {
		return false, nil, nil
	}

	if hasEntities {
		return true, nil, errFederationEntitiesNotImplemented
	}

	sdl, err := gj.getFederationSDL()
	if err != nil {
		return true, nil, err
	}
	resp, err := jsonMarshalServiceSDL(sdl)
	if err != nil {
		return true, nil, err
	}
	return true, resp, nil
}

// getFederationSDL returns the cached SDL, building it on first use.
func (gj *graphjinEngine) getFederationSDL() (string, error) {
	gj.federationSDLOnce.Do(func() {
		var schema *sdata.DBSchema
		if pdb := gj.primaryDB(); pdb != nil {
			schema = pdb.schema
		}
		if schema == nil {
			gj.federationSDLErr = errors.New("federation: primary database schema is not initialized")
			return
		}
		sdl, err := BuildFederationSDL(schema, gj.conf.Federation)
		gj.federationSDL = sdl
		gj.federationSDLErr = err
	})
	return gj.federationSDL, gj.federationSDLErr
}

// jsonMarshalServiceSDL produces the response payload Apollo expects:
// `{"_service":{"sdl":"..."}}`. The outer envelope is added by the
// engine's response wrapper.
func jsonMarshalServiceSDL(sdl string) ([]byte, error) {
	var b strings.Builder
	b.WriteString(`{"_service":{"sdl":`)
	b.WriteString(strconv.Quote(sdl))
	b.WriteString(`}}`)
	return []byte(b.String()), nil
}

// bytesContainsToken reports whether `tok` appears as a standalone
// identifier in `s` (no alphanumeric or underscore on either side).
// Avoids false positives like `my_service` matching `_service`.
func bytesContainsToken(s, tok []byte) bool {
	idx := 0
	for {
		i := bytes.Index(s[idx:], tok)
		if i < 0 {
			return false
		}
		pos := idx + i
		left := pos == 0 || !isIdentChar(s[pos-1])
		right := pos+len(tok) == len(s) || !isIdentChar(s[pos+len(tok)])
		if left && right {
			return true
		}
		idx = pos + 1
	}
}

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}
