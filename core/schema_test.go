package core

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
	"github.com/dosco/graphjin/core/v3/internal/sdata"
)

func TestCreateSchema(t *testing.T) {
	var buf bytes.Buffer

	di1 := sdata.GetTestDBInfo()
	if err := writeSchema(di1, &buf); err != nil {
		t.Fatal(err)
	}

	ds, err := qcode.ParseSchema(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	di2 := sdata.NewDBInfo(ds.Type,
		ds.Version,
		ds.Schema,
		"",
		ds.Columns,
		ds.Functions,
		nil)

	if di1.Hash() != di2.Hash() {
		t.Fatal(fmt.Errorf("schema hashes do not match: expected %d got %d",
			di1.Hash(), di2.Hash()))
	}
}
