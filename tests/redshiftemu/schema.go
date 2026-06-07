package redshiftemu

import "github.com/dosco/graphjin/tests/v3/hostedemu/redshift"

type Schema = redshift.Schema
type Table = redshift.Table
type Column = redshift.Column

func ParseSeedBytes(data []byte) (*Schema, error) {
	return redshift.ParseSeedBytes(data)
}
