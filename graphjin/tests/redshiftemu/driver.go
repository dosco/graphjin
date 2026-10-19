package redshiftemu

import (
	"database/sql/driver"

	"github.com/dosco/graphjin/tests/v3/hostedemu"
	"github.com/dosco/graphjin/tests/v3/hostedemu/redshift"
)

const (
	BackendCapture = hostedemu.BackendCapture
	BackendDuckDB  = hostedemu.BackendDuckDB

	FallbackPlaceholderOnError = hostedemu.FallbackPlaceholderOnError
	FallbackStrict             = hostedemu.FallbackStrict
)

type Config = hostedemu.Config

func NewConnector(conf Config) driver.Connector {
	return hostedemu.NewConnector(conf, redshift.NewAdapter())
}

func CapturePath(dir, testName string) string {
	return hostedemu.CapturePath(dir, testName)
}
