package core

import (
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/psql"
)

func TestCapIntParam(t *testing.T) {
	p := psql.Param{Name: "limit", Type: "integer", Max: 20}
	for _, tc := range []struct {
		in      interface{}
		want    int64
		wantErr bool
	}{
		{int64(3), 3, false},
		{int64(20), 20, false},
		{int64(5000), 20, false},
		{3, 3, false},
		{float64(7), 7, false},
		{float64(1e30), 20, false},
		{"8", 8, false},
		{nil, 20, false},
		{int64(-1), 0, true},
		{float64(2.5), 0, true},
		{"ten", 0, true},
		{true, 0, true},
	} {
		got, err := capIntParam(p, tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("capIntParam(%v) = %v, want error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("capIntParam(%v) = %v, %v; want %d", tc.in, got, err, tc.want)
		}
	}
}
