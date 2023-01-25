package core

var dbTypes map[string]string = map[string]string{
	"timestamp without time zone": "String",
	"character varying":           "String",
	"text":                        "String",
	"smallint":                    "Int",
	"integer":                     "Int",
	"bigint":                      "Int",
	"smallserial":                 "Int",
	"serial":                      "Int",
	"bigserial":                   "Int",
	"decimal":                     "Float",
	"numeric":                     "Float",
	"real":                        "Float",
	"double precision":            "Float",
	"money":                       "Float",
	"boolean":                     "Boolean",
}

type dirArg struct {
	name  string
	desc  string
	atype string
}

type dir struct {
	name   string
	desc   string
	locs   []string
	args   []dirArg
	repeat bool
}

var dirTypes []dir = []dir{
	{
		name: "cacheControl",
		desc: "Set the cache-control header to be passed back with the query result",
		locs: []string{LOC_QUERY, LOC_MUTATION, LOC_SUBSCRIPTION},
		args: []dirArg{{
			name:  "maxAge",
			desc:  "The maximum amount of time (in seconds) a resource is considered fresh",
			atype: "Int",
		}, {
			name:  "scope",
			desc:  "Set to 'public' when any cache can store the data and 'private' when only the browser cache should",
			atype: "String",
		}},
	},
	{
		name: "skip",
		desc: "Skip field if defined condition is met",
		locs: []string{LOC_FIELD},
		args: []dirArg{{
			name:  "ifRole",
			desc:  "If current role matches",
			atype: ("roles" + SUFFIX_ENUM),
		}, {
			name:  "ifVar",
			desc:  "If a variable is true",
			atype: "String",
		}},
	},
	{
		name: "include",
		desc: "Include field if defined condition is met",
		locs: []string{LOC_FIELD},
		args: []dirArg{{
			name:  "ifRole",
			desc:  "If current role matches",
			atype: ("roles" + SUFFIX_ENUM),
		}, {
			name:  "ifVar",
			desc:  "If a variable is true",
			atype: "String",
		}},
	},
	{
		name: "add",
		desc: "Add field if defined condition is met, Similar to 'include' except field is removed when condition is not met",
		locs: []string{LOC_FIELD},
		args: []dirArg{{
			name:  "ifRole",
			desc:  "If current role matches",
			atype: ("roles" + SUFFIX_ENUM),
		}},
	},
	{
		name: "remove",
		desc: "Include field if defined condition is met. Unlike 'skip' field is remove not set to null",
		locs: []string{LOC_FIELD},
		args: []dirArg{{
			name:  "ifRole",
			desc:  "If current role matches",
			atype: ("roles" + SUFFIX_ENUM),
		}},
	},
	{
		name: "schema",
		desc: "Specify database schema to use (Postgres specific)",
		locs: []string{LOC_FIELD},
		args: []dirArg{{
			name:  "name",
			desc:  "Name of schema",
			atype: "String",
		}},
	},
	{
		name: "notRelated",
		desc: "Treat this selector as if it were a top-level selector with no relation to its parent",
		locs: []string{LOC_FIELD},
	},
	{
		name: "through",
		desc: "use the specified table as a join-table to connect this field and it's parent",
		locs: []string{LOC_FIELD},
		args: []dirArg{{
			name:  "table",
			desc:  "Table name",
			atype: "tables" + SUFFIX_ENUM,
		}},
	},
}

type exp struct {
	name  string
	desc  string
	etype string
}

const (
	likeDesc       = "Value matching pattern where '%' represents zero or more characters and '_' represents a single character. Eg. '_r%' finds values having 'r' in second position"
	notLikeDesc    = "Value not matching pattern where '%' represents zero or more characters and '_' represents a single character. Eg. '_r%' finds values not having 'r' in second position"
	iLikeDesc      = "Value matching (case-insensitive) pattern where '%' represents zero or more characters and '_' represents a single character. Eg. '_r%' finds values having 'r' in second position"
	notILikeDesc   = "Value not matching (case-insensitive) pattern where '%' represents zero or more characters and '_' represents a single character. Eg. '_r%' finds values not having 'r' in second position"
	similarDesc    = "Value matching regex pattern. Similar to the 'like' operator but with support for regex. Pattern must match entire value."
	notSimilarDesc = "Value not matching regex pattern. Similar to the 'like' operator but with support for regex. Pattern must not match entire value."
)

var expAll = []exp{
	{name: "isNull", desc: "Is value null (true) or not null (false)", etype: "Boolean"},
}

var expScalar = []exp{
	{name: "equals", desc: "Equals value"},
	{name: "notEquals", desc: "Does not equal value"},
	{name: "greaterThan", desc: "Is greater than value"},
	{name: "lesserThan", desc: "Is lesser than value"},
	{name: "greaterOrEquals", desc: "Is greater than or equals value"},
	{name: "lesserOrEquals", desc: "Is lesser than or equals value"},
	{name: "like", desc: iLikeDesc},
	{name: "notLike", desc: notLikeDesc},
	{name: "iLike", desc: iLikeDesc},
	{name: "notILike", desc: notILikeDesc},
	{name: "similar", desc: similarDesc},
	{name: "notSimilar", desc: notSimilarDesc},
	{name: "regex", desc: "Value matches regex pattern"},
	{name: "notRegex", desc: "Value not matching regex pattern"},
	{name: "iRegex", desc: "Value matches (case-insensitive) regex pattern"},
	{name: "notIRegex", desc: "Value not matching (case-insensitive) regex pattern"},
}

var expList = []exp{
	{name: "in", desc: "Is in list of values"},
	{name: "notIn", desc: "Is not in list of values"},
}

var expJSON = []exp{
	{name: "hasKey", desc: "JSON value contains this key"},
	{name: "hasKeyAny", desc: "JSON value contains any of these keys"},
	{name: "hasKeyAll", desc: "JSON value contains all of these keys"},
	{name: "contains", desc: "JSON value matches any of they key/value pairs"},
	{name: "containedIn", desc: "JSON value contains all of they key/value pairs"},
}
