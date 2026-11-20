package bigquerydriver

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Scheme is the connection-string prefix for a BigQuery source.
const Scheme = "bigquery://"

// ParseDSN reads a connection string of the form
//
//	bigquery://project/dataset?location=EU&credentials_file=key.json&max_bytes_billed=1000000000
//
// Only the project and dataset are required. An unknown option is an error,
// so a misspelled one is not silently ignored.
func ParseDSN(dsn string) (Config, error) {
	var conf Config
	if !strings.HasPrefix(strings.ToLower(dsn), Scheme) {
		return conf, fmt.Errorf("bigquery: connection string must start with %s", Scheme)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return conf, fmt.Errorf("bigquery: invalid connection string: %w", err)
	}
	conf.ProjectID = u.Host
	conf.DatasetID = strings.Trim(u.Path, "/")
	if strings.Contains(conf.DatasetID, "/") {
		return conf, fmt.Errorf("bigquery: connection string must be %sproject/dataset", Scheme)
	}

	for key, vals := range u.Query() {
		val := vals[len(vals)-1]
		switch strings.ToLower(key) {
		case "location":
			conf.Location = val
		case "credentials_file":
			conf.CredentialsFile = val
		case "max_bytes_billed":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return conf, fmt.Errorf("bigquery: max_bytes_billed must be a whole number of bytes, got %q", val)
			}
			conf.MaxBytesBilled = n
		default:
			return conf, fmt.Errorf("bigquery: unknown connection string option %q", key)
		}
	}
	if err := conf.validate(); err != nil {
		return conf, err
	}
	return conf, nil
}
