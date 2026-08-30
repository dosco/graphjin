package main

import (
	"fmt"
	"strings"
)

// Mapping a real database's types onto GraphJin's DDL vocabulary.
//
// Types arrive as the database reported them — "bigint", "character varying(255)",
// "timestamp with time zone", "INTEGER" — because that is what introspection
// records. The DDL is written in PascalCase names the schema parser converts
// back to the same snake-spaced form, so the mapping has to be exact in both
// directions.
//
// Anything unrecognised becomes Text and says so in a comment rather than
// failing the clone. A column whose type nobody could map is still worth having:
// tasks can count it, filter it and read it, and the comment tells whoever looks
// what it really was.

// cloneTypeMap is keyed on the normalized (lowercased, size-stripped) type.
var cloneTypeMap = map[string]string{
	"bigint": "Bigint", "big int": "Bigint", "bigserial": "Bigint", "big serial": "Bigint",
	"int": "Int", "integer": "Int", "int4": "Int", "serial": "Int",
	"smallint": "Smallint", "small int": "Smallint", "int2": "Smallint", "tinyint": "Smallint",
	"int8": "Bigint", "mediumint": "Int",
	"float": "Float", "float4": "Float", "float8": "Double", "real": "Real",
	"double": "Double", "double precision": "Double",
	"decimal": "Decimal", "numeric": "Numeric", "money": "Money",
	"boolean": "Boolean", "bool": "Boolean",
	"text": "Text", "string": "Text", "clob": "Text", "longtext": "Text", "mediumtext": "Text",
	"varchar": "Varchar", "character varying": "Varchar", "nvarchar": "Varchar",
	"char": "Char", "character": "Char", "bpchar": "Char",
	"timestamp": "Timestamp", "datetime": "Timestamp",
	"timestamp with time zone": "TimestampWithTimeZone", "timestamptz": "TimestampWithTimeZone",
	"timestamp without time zone": "Timestamp",
	"date":                        "Date",
	"time":                        "Time", "time with time zone": "TimeWithTimeZone", "timetz": "TimeWithTimeZone",
	"interval": "Interval",
	"json":     "Json", "jsonb": "Jsonb",
	// Uuid, never UUID: the schema parser splits PascalCase on capitals, so
	// "UUID" becomes the type "u u i d".
	"uuid":  "Uuid",
	"bytea": "Bytea", "blob": "Bytea", "bytes": "Bytea", "varbinary": "Bytea",
	"xml": "Xml",
}

// cloneTypeMapping is one column's resolved DDL type.
type cloneTypeMapping struct {
	// Base is the PascalCase DDL type name.
	Base string
	// Args carries a size or precision, when the original had one.
	Args string
	// Mapped is false when nothing recognised the type and it fell back to Text.
	Mapped bool
	// Original is the type string the database reported.
	Original string
}

// DDL renders the type as it appears in a .ddl file, including the not-null
// marker and any size argument.
func (m cloneTypeMapping) DDL(notNull bool) string {
	out := m.Base
	if notNull {
		out += "!"
	}
	if m.Args != "" {
		out += fmt.Sprintf(" @type(args: %q)", m.Args)
	}
	return out
}

// mapCloneColumnType resolves a column's type, using its name to recover what
// the database's own type system could not express.
//
// SQLite has no date type: a column declared Date and one declared Text both
// introspect as "text", so a clone taken from SQLite would lose every date and
// with it every question about a period. GraphJin's catalog already reads these
// names as dates when deciding what to publish, so a clone reads them the same
// way. The cost of being wrong is a synthetic column holding timestamps instead
// of prose, which is a far smaller loss than a schema with no dates in it.
func mapCloneColumnType(name, raw string) cloneTypeMapping {
	mapping := mapCloneType(raw)
	if mapping.Base != "Text" {
		return mapping
	}
	lowered := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasSuffix(lowered, "_at"), strings.HasSuffix(lowered, "_time"),
		strings.HasSuffix(lowered, "_timestamp"):
		mapping.Base = "TimestampWithTimeZone"
	case strings.HasSuffix(lowered, "_date"), lowered == "date":
		mapping.Base = "Date"
	default:
		return mapping
	}
	mapping.Args = ""
	// The mapping is inferred rather than reported, so it is not a fallback and
	// should not be flagged as unmapped.
	mapping.Mapped = true
	return mapping
}

func mapCloneType(raw string) cloneTypeMapping {
	original := strings.TrimSpace(raw)
	base, args := splitTypeArgs(strings.ToLower(original))
	base = strings.Join(strings.Fields(base), " ")
	if mapped, ok := cloneTypeMap[base]; ok {
		return cloneTypeMapping{Base: mapped, Args: args, Mapped: true, Original: original}
	}
	// Arrays and vendor types land here. Text holds anything the seed writes and
	// keeps the table usable rather than dropping it.
	return cloneTypeMapping{Base: "Text", Mapped: false, Original: original}
}

// splitTypeArgs separates "numeric(10,2)" into its base and its arguments.
func splitTypeArgs(value string) (string, string) {
	open := strings.IndexByte(value, '(')
	if open < 0 || !strings.HasSuffix(value, ")") {
		return value, ""
	}
	return strings.TrimSpace(value[:open]), strings.TrimSpace(value[open+1 : len(value)-1])
}
