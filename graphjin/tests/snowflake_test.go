package tests_test

import (
	"testing"

	"github.com/dosco/graphjin/core/v3"
)

func TestSnowflakeConnectorInit(t *testing.T) {
	if dbType != "snowflake" {
		t.Skip("snowflake-only test")
	}

	conf := newConfig(&core.Config{DBType: dbType, DisableAllowList: true, Debug: true})
	gj, err := core.NewGraphJin(conf, db)
	if err != nil {
		t.Fatal(err)
	}
	if gj == nil {
		t.Fatal("expected non-nil graphjin instance")
	}
}
