package bigqueryemu

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/tests/v3/bigquerylive"
	"github.com/dosco/graphjin/tests/v3/hostedemu"
	"github.com/dosco/graphjin/tests/v3/hostedemu/bigquery"
)

const (
	BackendCapture = hostedemu.BackendCapture
	BackendDuckDB  = hostedemu.BackendDuckDB
	BackendLive    = "live"

	FallbackPlaceholderOnError = hostedemu.FallbackPlaceholderOnError
	FallbackStrict             = hostedemu.FallbackStrict
)

type Config struct {
	hostedemu.Config
	ProjectID string
	DatasetID string
	Location  string
	TableRows map[string]uint64
}

func NewConnector(conf Config) driver.Connector {
	if strings.EqualFold(strings.TrimSpace(conf.Backend), BackendLive) {
		live, err := bigquerylive.NewConnector(context.Background(), bigquerylive.Config{
			ProjectID:  conf.ProjectID,
			DatasetID:  conf.DatasetID,
			Location:   conf.Location,
			CaptureDir: conf.CaptureDir,
			TestName:   conf.TestName,
			RunID:      conf.RunID,
			TableRows:  conf.TableRows,
		})
		return &routedConnector{connector: live, initErr: err}
	}
	return hostedemu.NewConnector(conf.Config, bigquery.NewAdapter())
}

func CapturePath(dir, testName string) string {
	return hostedemu.CapturePath(dir, testName)
}

type routedConnector struct {
	connector driver.Connector
	initErr   error
}

func (c *routedConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if c.initErr != nil {
		return nil, c.initErr
	}
	if c.connector == nil {
		return nil, fmt.Errorf("bigquery connector is not initialized")
	}
	return c.connector.Connect(ctx)
}

func (c *routedConnector) Driver() driver.Driver {
	if c.connector != nil {
		return c.connector.Driver()
	}
	return &errorDriver{err: c.initErr}
}

type errorDriver struct {
	err error
}

func (d *errorDriver) Open(string) (driver.Conn, error) {
	if d.err != nil {
		return nil, d.err
	}
	return nil, fmt.Errorf("bigquery connector is not initialized")
}
