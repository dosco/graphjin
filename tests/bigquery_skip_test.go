package tests_test

import "testing"

func skipBigQueryMutationsUnsupported(t *testing.T) {
	t.Helper()
	if dbType == "bigquery" {
		t.Skip("bigquery: mutations are outside the experimental BigQuery MVP; linear mutation SQL still needs GoogleSQL lowering")
	}
}

func skipBigQuerySubscriptionsUnsupported(t *testing.T) {
	t.Helper()
	if dbType == "bigquery" {
		t.Skip("bigquery: subscriptions are outside the experimental BigQuery MVP")
	}
}

func skipBigQuerySchemaDiffUnsupported(t *testing.T) {
	t.Helper()
	if dbType == "bigquery" {
		t.Skip("bigquery: schema-diff DDL/live migration support is outside the experimental BigQuery MVP")
	}
}

func skipBigQueryJSONVirtualTablesUnsupported(t *testing.T) {
	t.Helper()
	if dbType == "bigquery" {
		t.Skip("bigquery: JSON virtual tables need RelEmbedded UNNEST lowering in the dialect/simulator")
	}
}
