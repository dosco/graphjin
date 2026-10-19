package tests_test

import (
	"context"
	"database/sql"

	"github.com/dosco/graphjin/tests/v3/redshiftemu"
)

func startRedshiftDB(context.Context) (func(context.Context) error, *sql.DB, error) {
	db := sql.OpenDB(redshiftemu.NewConnector(redshiftemu.Config{
		SeedPath: "./redshift.sql",
		Backend:  redshiftemu.BackendDuckDB,
		Fallback: redshiftemu.FallbackStrict,
		TestName: "redshift-integration",
		RunID:    "unit",
	}))
	return func(context.Context) error { return db.Close() }, db, nil
}
