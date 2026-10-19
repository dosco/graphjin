package tests_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/dosco/graphjin/tests/v3/bigqueryemu"
	"github.com/dosco/graphjin/tests/v3/bigquerylive"
	"github.com/dosco/graphjin/tests/v3/hostedemu"
)

func startBigQueryDB(ctx context.Context) (func(context.Context) error, *sql.DB, error) {
	if os.Getenv("GRAPHJIN_BIGQUERY_MOCK") == "1" {
		sqlDB := sql.OpenDB(bigqueryemu.NewConnector(bigqueryemu.Config{
			Config: hostedBigQueryConfig(strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_BACKEND"))),
		}))
		cleanup := func(context.Context) error {
			return sqlDB.Close()
		}
		if err := sqlDB.PingContext(ctx); err != nil {
			_ = cleanup(ctx)
			return nil, nil, err
		}
		return cleanup, sqlDB, nil
	}

	liveCleanup, connector, err := startBigQueryLive(ctx)
	if err != nil {
		return nil, nil, err
	}
	sqlDB := sql.OpenDB(connector)
	cleanup := func(cleanupCtx context.Context) error {
		_ = sqlDB.Close()
		return liveCleanup(cleanupCtx)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = cleanup(ctx)
		return nil, nil, err
	}
	return cleanup, sqlDB, nil
}

func startBigQueryLive(ctx context.Context) (func(context.Context) error, driver.Connector, error) {
	projectID := strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_PROJECT"))
	if projectID == "" {
		log.Printf("Skipping live bigquery tests: GRAPHJIN_BIGQUERY_PROJECT env var not set")
		os.Exit(0)
	}
	location := strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_LOCATION"))
	if location == "" {
		location = "US"
	}
	datasetID := strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_DATASET"))
	if datasetID == "" {
		name, err := newEphemeralSchemaName()
		if err != nil {
			return nil, nil, fmt.Errorf("bigquery live: generate dataset name: %w", err)
		}
		datasetID = strings.ToLower(name)
	}

	svc, err := bigquerylive.NewService(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("bigquery live: create service: %w", err)
	}
	if err := bigquerylive.CreateDataset(ctx, svc, projectID, datasetID, location); err != nil {
		return nil, nil, err
	}
	cleanup := func(ctx context.Context) error {
		return bigquerylive.DropDataset(ctx, svc, projectID, datasetID)
	}
	seedPath := "./bigquery.sql"
	if err := bigquerylive.SeedFile(ctx, svc, projectID, datasetID, location, seedPath); err != nil {
		_ = cleanup(ctx)
		return nil, nil, err
	}
	tableRows, err := bigquerylive.SeedRowCounts(seedPath)
	if err != nil {
		_ = cleanup(ctx)
		return nil, nil, err
	}
	connector := bigqueryemu.NewConnector(bigqueryemu.Config{
		Config:    hostedBigQueryConfig(bigqueryemu.BackendLive),
		ProjectID: projectID,
		DatasetID: datasetID,
		Location:  location,
		TableRows: tableRows,
	})
	return cleanup, connector, nil
}

func hostedBigQueryConfig(backend string) hostedemu.Config {
	return hostedemu.Config{
		SeedPath:   "./bigquery.sql",
		CaptureDir: strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_CAPTURE_DIR")),
		TestName:   strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_TEST_NAME")),
		RunID:      strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_RUN_ID")),
		Backend:    backend,
		Fallback:   strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_FALLBACK")),
		Discovery:  strings.TrimSpace(os.Getenv("GRAPHJIN_BIGQUERY_DISCOVERY")),
	}
}
