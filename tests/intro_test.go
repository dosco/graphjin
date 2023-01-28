package tests_test

import (
	_ "embed"
	"os"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
)

// //go:embed test-db.graphql
// var testSchema []byte

// func TestIntrospection1(t *testing.T) {
// 	ds, err := qcode.ParseSchema(testSchema)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	dbinfo := sdata.NewDBInfo(ds.Type,
// 		ds.Version,
// 		ds.Schema,
// 		"",
// 		ds.Columns,
// 		ds.Functions,
// 		nil)

// 	aliases := map[string][]string{
// 		"users": {"me"},
// 	}

// 	s, err := sdata.NewDBSchema(dbinfo, aliases)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	conf := &Config{}
// 	if _, err = introQuery(s, conf); err != nil {
// 		t.Error(err)
// 	}
// }

func TestIntrospection(t *testing.T) {
	dir, err := os.MkdirTemp("", "test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	fs := core.NewOsFS(dir)

	conf := newConfig(&core.Config{DBType: dbType, EnableIntrospection: true})
	_, err = core.NewGraphJin(conf, db, core.OptionSetFS(fs))
	if err != nil {
		panic(err)
	}
	b, err := fs.Get("intro.json")
	assert.NoError(t, err)
	assert.NotEmpty(t, b)
}
