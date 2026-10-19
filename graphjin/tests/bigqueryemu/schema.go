package bigqueryemu

import "github.com/dosco/graphjin/tests/v3/hostedemu/snowflake/catalog"

func ParseSeed(path string) (*catalog.Schema, error) {
	return catalog.ParseSeed(path)
}
