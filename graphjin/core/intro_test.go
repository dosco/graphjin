package core

import (
	_ "embed"
	"testing"

	"github.com/dosco/graphjin/v2/core/internal/qcode"
	"github.com/dosco/graphjin/v2/core/internal/sdata"
)

//go:embed test-db.schema
var testSchema []byte

func TestIntrospection(t *testing.T) {
	ds, err := qcode.ParseSchema(testSchema)
	if err != nil {
		t.Fatal(err)
	}

	dbinfo := sdata.NewDBInfo(ds.Type,
		ds.Version,
		ds.Schema,
		"",
		ds.Columns,
		ds.Functions,
		nil)

	aliases := map[string][]string{
		"users": {"me"},
	}

	s, err := sdata.NewDBSchema(dbinfo, aliases)
	if err != nil {
		t.Fatal(err)
	}

	_, err = introspection(newIntro(s, false))
	if err != nil {
		t.Error(err)
	}
}
