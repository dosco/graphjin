package snowflakeemu

import "github.com/dosco/graphjin/tests/v3/hostedemu/snowflake/catalog"

type Schema = catalog.Schema
type Table = catalog.Table
type Column = catalog.Column

func ParseSeed(path string) (*Schema, error) {
	return catalog.ParseSeed(path)
}

func ParseSeedBytes(data []byte) (*Schema, error) {
	return catalog.ParseSeedBytes(data)
}
