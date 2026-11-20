package serv

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dosco/graphjin/bigquerydriver"
	"github.com/dosco/graphjin/core/v3"
)

// initBigQuery opens a BigQuery dataset through the bigquerydriver connector.
//
// BigQuery has no host, port or password. The source is a project and a
// dataset, named in connection_string, and credentials are Application Default
// Credentials unless the string names a service-account key file.
func initBigQuery(conf *Config, openDB, useTelemetry bool, fs core.FS) (*dbConf, error) {
	connString := strings.TrimSpace(conf.DB.ConnString)
	if connString == "" {
		return nil, fmt.Errorf("bigquery requires connection_string: %sproject/dataset", bigquerydriver.Scheme)
	}
	bc, err := bigquerydriver.ParseDSN(connString)
	if err != nil {
		return nil, err
	}

	// A relative key file is read from the config folder, like every other
	// key file in the config, and not from wherever the server was started.
	if bc.CredentialsFile != "" && !filepath.IsAbs(bc.CredentialsFile) && fs != nil {
		key, err := fs.Get(bc.CredentialsFile)
		if err != nil {
			return nil, fmt.Errorf("bigquery: reading credentials_file %q: %w", bc.CredentialsFile, err)
		}
		bc.CredentialsJSON = key
		bc.CredentialsFile = ""
	}

	// The connector keeps this context to refresh credentials, so it must not
	// be one that ends.
	connector, err := bigquerydriver.NewConnector(context.Background(), bc)
	if err != nil {
		return nil, err
	}
	return &dbConf{driverName: "bigquery", connector: connector}, nil
}
