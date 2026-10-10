package whereop

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		typ   string
		array bool
		want  Family
	}{
		// Postgres
		{"timestamp without time zone", false, Temporal},
		{"timestamp with time zone", false, Temporal},
		{"timestamptz", false, Temporal},
		{"timestamp(6) with time zone", false, Temporal},
		{"time without time zone", false, Temporal},
		{"date", false, Temporal},
		{"interval", false, Temporal},
		{"double precision", false, Numeric},
		{"numeric(10,2)", false, Numeric},
		{"bigint", false, Numeric},
		{"character varying", false, Text},
		{"character varying(255)", false, Text},
		{"citext", false, Text},
		{"boolean", false, Boolean},
		{"jsonb", false, JSON},
		{"uuid", false, UUID},
		{"geometry(Point,4326)", false, Geometry},
		{"point", false, Geometry},
		{"integer[]", false, Array},
		{"text", true, Array},
		{"order_status", false, Other},
		{"bit(8)", false, Other},
		{"bit varying(8)", false, Other},
		{"tstzrange", false, Other},
		{"bytea", false, Other},
		// MySQL / MariaDB
		{"tinyint(1)", false, Boolean},
		{"tinyint(4)", false, Numeric},
		{"int unsigned", false, Numeric},
		{"bigint(20) unsigned", false, Numeric},
		{"datetime", false, Temporal},
		{"varchar(255)", false, Text},
		{"enum('a','b')", false, Text},
		// SQL Server
		{"datetime2", false, Temporal},
		{"datetimeoffset", false, Temporal},
		{"nvarchar(max)", false, Text},
		{"bit", false, Boolean},
		{"uniqueidentifier", false, UUID},
		// Oracle
		{"NUMBER", false, Numeric},
		{"NUMBER(1)", false, Boolean},
		{"NUMBER(10,2)", false, Numeric},
		{"VARCHAR2(100)", false, Text},
		{"TIMESTAMP(6) WITH TIME ZONE", false, Temporal},
		{"BINARY_DOUBLE", false, Numeric},
		// ClickHouse / Snowflake / DuckDB
		{"Nullable(DateTime64(3))", false, Temporal},
		{"LowCardinality(String)", false, Text},
		{"UInt64", false, Numeric},
		{"Decimal(18, 4)", false, Numeric},
		{"TIMESTAMP_NTZ", false, Temporal},
		{"HUGEINT", false, Numeric},
		{"Array(String)", false, Array},
		{"BIGNUMERIC", false, Numeric},
		{"SUPER", false, JSON},
		{"timeuuid", false, UUID},
	}
	for _, c := range cases {
		if got := Classify(c.typ, c.array); got != c.want {
			t.Errorf("Classify(%q, %v) = %s, want %s", c.typ, c.array, got, c.want)
		}
	}
}

func TestAllowed(t *testing.T) {
	cases := []struct {
		op, typ string
		array   bool
		canon   string
		ok      bool
	}{
		// GitHub issue #656: range filters on timestamp columns.
		{"gte", "timestamp without time zone", false, "gte", true},
		{"greater_than", "timestamp with time zone", false, "gt", true},
		{"lt", "date", false, "lt", true},
		{"lte", "datetime2", false, "lte", true},
		{"gt", "double precision", false, "gt", true},
		{"ilike", "character varying", false, "ilike", true},
		{"not_ilike", "character varying", false, "nilike", true},
		{"not_in", "uuid", false, "nin", true},
		{"distinct", "integer", false, "distinct", true},
		{"in", "text", true, "in", true},
		{"has_in_common", "text", true, "has_in_common", true},
		{"st_dwithin", "geometry(Point,4326)", false, "st_dwithin", true},
		{"_eq", "integer", false, "eq", true},
		// Fits no family member.
		{"like", "timestamp", false, "like", false},
		{"gt", "boolean", false, "gt", false},
		{"overlaps", "text", true, "st_overlaps", false},
		// Unknown type: not rejected.
		{"gt", "order_status", false, "gt", true},
		// Not an operator at all.
		{"between", "timestamp", false, "", false},
	}
	for _, c := range cases {
		canon, ok := Allowed(c.op, c.typ, c.array)
		if canon != c.canon || ok != c.ok {
			t.Errorf("Allowed(%q, %q, %v) = (%q, %v), want (%q, %v)", c.op, c.typ, c.array, canon, ok, c.canon, c.ok)
		}
	}
}

func TestOperatorsAreCanonical(t *testing.T) {
	for family, ops := range familyOperators {
		for _, op := range ops {
			if Canonical(op) != op {
				t.Errorf("%s lists %q, which is not a canonical operator name", family, op)
			}
		}
	}
}
