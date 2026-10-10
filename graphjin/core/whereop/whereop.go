// Package whereop is the shared catalog of GraphJin where-clause operators and
// the column type families they apply to.
//
// The compiler (core/internal/qcode) accepts an operator on any column and
// leaves type checking to the database. The advisory validators exposed to
// agents and MCP clients (validate_where_clause and friends) instead tell a
// caller which operators fit a column, and callers trust that answer: an
// operator wrongly reported as unsupported sends an agent looking for a
// workaround that does not exist. Both the agent and the MCP server use this
// package so the two answers cannot drift apart, and so a column's type is
// classified the same way whatever the dialect spells it as
// ("timestamp without time zone", "datetime2", "NUMBER(10,2)",
// "Nullable(DateTime64(3))", ...).
package whereop

import "strings"

// Family is a coarse column type category that determines which operators fit.
type Family string

const (
	Numeric  Family = "numeric"
	Temporal Family = "timestamp"
	Text     Family = "text"
	Boolean  Family = "boolean"
	JSON     Family = "json"
	UUID     Family = "uuid"
	Geometry Family = "geometry"
	Array    Family = "array"
	// Other is a type this package does not recognise, such as a user-defined
	// enum or domain. Operators are not rejected for it.
	Other Family = "other"
)

// aliases maps every operator spelling the compiler accepts to its canonical
// name. It mirrors the operator switch in core/internal/qcode/exp.go; a test
// in that package fails if a spelling here is not accepted by the compiler.
var aliases = map[string]string{
	"eq": "eq", "equals": "eq",
	"neq": "neq", "notEquals": "neq", "not_equals": "neq",
	"gt": "gt", "greaterThan": "gt", "greater_than": "gt",
	"gte": "gte", "gteq": "gte", "greaterOrEquals": "gte", "greater_or_equals": "gte",
	"lt": "lt", "lesserThan": "lt", "lesser_than": "lt",
	"lte": "lte", "lteq": "lte", "lesserOrEquals": "lte", "lesser_or_equals": "lte",
	"in":  "in",
	"nin": "nin", "notIn": "nin", "not_in": "nin",
	"like": "like", "nlike": "nlike", "notLike": "nlike", "not_like": "nlike",
	"ilike": "ilike", "iLike": "ilike", "nilike": "nilike", "notILike": "nilike", "not_ilike": "nilike",
	"similar": "similar", "nsimilar": "nsimilar", "notSimiliar": "nsimilar", "not_similar": "nsimilar",
	"regex": "regex", "nregex": "nregex", "notRegex": "nregex", "not_regex": "nregex",
	"iregex": "iregex", "niregex": "niregex", "notIRegex": "niregex", "not_iregex": "niregex",
	"contains":    "contains",
	"containedIn": "contained_in", "contained_in": "contained_in",
	"hasInCommon": "has_in_common", "has_in_common": "has_in_common",
	"hasKey": "has_key", "has_key": "has_key",
	"hasKeyAny": "has_key_any", "has_key_any": "has_key_any",
	"hasKeyAll": "has_key_all", "has_key_all": "has_key_all",
	"isNull": "is_null", "is_null": "is_null",
	"notDistinct": "not_distinct", "ndis": "not_distinct", "not_distinct": "not_distinct",
	"dis": "distinct", "distinct": "distinct",
	"st_dwithin": "st_dwithin", "stDWithin": "st_dwithin", "st_d_within": "st_dwithin", "dwithin": "st_dwithin",
	"st_within": "st_within", "stWithin": "st_within", "within": "st_within",
	"st_contains": "st_contains", "stContains": "st_contains", "geoContains": "st_contains",
	"st_intersects": "st_intersects", "stIntersects": "st_intersects", "intersects": "st_intersects",
	"st_coveredby": "st_coveredby", "stCoveredBy": "st_coveredby", "coveredBy": "st_coveredby", "covered_by": "st_coveredby",
	"st_covers": "st_covers", "stCovers": "st_covers", "covers": "st_covers",
	"st_touches": "st_touches", "stTouches": "st_touches", "touches": "st_touches",
	"st_overlaps": "st_overlaps", "stOverlaps": "st_overlaps", "overlaps": "st_overlaps",
	"near": "near", "geoNear": "near",
}

// Aliases returns every operator spelling the compiler accepts, in no
// particular order.
func Aliases() []string {
	out := make([]string, 0, len(aliases))
	for k := range aliases {
		out = append(out, k)
	}
	return out
}

// Canonical returns the canonical name of a where operator, or "" when the
// compiler does not know it. A leading underscore is ignored, as in the
// compiler.
func Canonical(op string) string {
	if op != "" && op[0] == '_' {
		op = op[1:]
	}
	return aliases[op]
}

var (
	equality   = []string{"eq", "neq", "in", "nin", "is_null", "distinct", "not_distinct"}
	comparison = []string{"gt", "gte", "lt", "lte"}
	pattern    = []string{"like", "nlike", "ilike", "nilike", "similar", "nsimilar", "regex", "nregex", "iregex", "niregex"}
	spatial    = []string{"st_dwithin", "st_within", "st_contains", "st_intersects", "st_coveredby", "st_covers", "st_touches", "st_overlaps", "near"}

	familyOperators = map[Family][]string{
		Numeric:  concat(equality, comparison),
		Temporal: concat(equality, comparison),
		Text:     concat(equality, comparison, pattern),
		Boolean:  equality,
		UUID:     equality,
		JSON:     {"has_key", "has_key_any", "has_key_all", "contains", "contained_in", "is_null"},
		// The compiler rewrites `in` on an array column to has_in_common.
		Array:    {"contains", "contained_in", "has_in_common", "in", "is_null"},
		Geometry: concat(spatial, []string{"is_null"}),
		Other:    concat(equality, comparison),
	}
)

func concat(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}

// FamilyOperators returns a copy of the canonical operators that fit a family.
func FamilyOperators(f Family) []string {
	return append([]string(nil), familyOperators[f]...)
}

// Operators returns the canonical operators that fit a column type.
func Operators(dbType string, isArray bool) []string {
	return FamilyOperators(Classify(dbType, isArray))
}

// Allowed reports whether op is a known operator and whether it fits the
// column type. canonical is "" when the operator is unknown. Operators on a
// column of the Other family are always allowed.
func Allowed(op, dbType string, isArray bool) (canonical string, ok bool) {
	canonical = Canonical(op)
	if canonical == "" {
		return "", false
	}
	family := Classify(dbType, isArray)
	if family == Other {
		return canonical, true
	}
	for _, v := range familyOperators[family] {
		if v == canonical {
			return canonical, true
		}
	}
	return canonical, false
}

// Classify maps a database column type, as any supported dialect reports it,
// to a Family.
func Classify(dbType string, isArray bool) Family {
	t := strings.ToLower(strings.TrimSpace(dbType))
	if isArray || strings.HasSuffix(t, "[]") || strings.HasPrefix(t, "array") {
		return Array
	}
	t = unwrapWrappers(t)

	compact := strings.ReplaceAll(t, " ", "")
	switch {
	case compact == "tinyint(1)", compact == "number(1)", compact == "number(1,0)", compact == "bit(1)":
		return Boolean
	}

	base := baseTypeName(t)
	switch base {
	case "bool", "boolean":
		return Boolean
	case "bit":
		// MSSQL's bit is a boolean; Postgres bit(n)/bit varying are bit strings.
		if t == "bit" {
			return Boolean
		}
		return Other
	case "smallint", "integer", "int", "bigint", "tinyint", "mediumint",
		"int2", "int4", "int8", "int16", "int32", "int64", "int128", "int256",
		"uint8", "uint16", "uint32", "uint64", "uint128", "uint256",
		"serial", "smallserial", "bigserial", "serial2", "serial4", "serial8",
		"decimal", "dec", "numeric", "number", "fixed", "bignumeric", "bigdecimal",
		"decimal32", "decimal64", "decimal128", "decimal256",
		"real", "float", "float4", "float8", "float32", "float64",
		"double", "binary_float", "binary_double",
		"money", "smallmoney", "oid", "byteint", "hugeint", "ubigint", "uinteger", "usmallint", "utinyint":
		return Numeric
	case "date", "date32", "time", "timetz", "timestamp", "timestamptz",
		"timestamp_ntz", "timestamp_ltz", "timestamp_tz",
		"datetime", "datetime2", "datetime64", "smalldatetime", "datetimeoffset",
		"interval", "year":
		return Temporal
	case "char", "character", "nchar", "varchar", "nvarchar", "varchar2", "nvarchar2",
		"bpchar", "text", "tinytext", "mediumtext", "longtext", "ntext", "citext",
		"string", "fixedstring", "clob", "nclob", "name", "enum", "set", "ascii", "varchar_ignorecase":
		return Text
	case "json", "jsonb", "variant", "object", "super":
		return JSON
	case "uuid", "uniqueidentifier", "timeuuid":
		return UUID
	case "geometry", "geography", "point", "linestring", "polygon",
		"multipoint", "multilinestring", "multipolygon", "geometrycollection",
		"sdo_geometry", "st_geometry":
		return Geometry
	}
	return Other
}

// unwrapWrappers strips ClickHouse-style type wrappers that do not change how
// a value compares, such as Nullable(DateTime) and LowCardinality(String).
func unwrapWrappers(t string) string {
	for {
		trimmed := false
		for _, w := range []string{"nullable(", "lowcardinality("} {
			if strings.HasPrefix(t, w) && strings.HasSuffix(t, ")") {
				t = strings.TrimSpace(t[len(w) : len(t)-1])
				trimmed = true
			}
		}
		if !trimmed {
			return t
		}
	}
}

// baseTypeName returns the leading type name of a declared type, dropping
// length/precision arguments and trailing qualifiers: "varchar(255)" →
// "varchar", "timestamp(6) with time zone" → "timestamp", "double precision"
// → "double", "int unsigned" → "int", "character varying" → "character".
func baseTypeName(t string) string {
	end := len(t)
	if i := strings.IndexAny(t, "( "); i >= 0 {
		end = i
	}
	return strings.Trim(t[:end], `"`)
}
