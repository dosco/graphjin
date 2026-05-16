package core

import (
	"errors"
	"testing"
)

func TestNewErrorIncludesGraphJinRepairExtension(t *testing.T) {
	errs := newError(`query { orders_aggregate { count } }`, errors.New(`table "orders_aggregate" not found`))
	if len(errs) != 1 {
		t.Fatalf("expected one error, got %d", len(errs))
	}
	if errs[0].Message != `table "orders_aggregate" not found` {
		t.Fatalf("message changed: %q", errs[0].Message)
	}
	raw := errs[0].Extensions["graphjin_repair"]
	repair, ok := raw.(ErrorRepair)
	if !ok {
		t.Fatalf("expected graphjin_repair extension, got %#v", raw)
	}
	if repair.Kind != repairKindWrongDialect {
		t.Fatalf("expected wrong dialect repair, got %+v", repair)
	}
}
