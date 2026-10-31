package graph

import "testing"

func TestParseExponentNumbers(t *testing.T) {
	for _, raw := range []string{"1e3", "1.25e-3", "-2E+4"} {
		if _, err := Parse([]byte(`{ products(where: {price: {gt: ` + raw + `}}) { id } }`)); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
}

func TestRejectIncompleteExponentNumbers(t *testing.T) {
	for _, raw := range []string{"1e", "1e+", "1.25E-", "1e3x", "1.e2", "-2.E+3", "-e2", "+e2", "-.2e2", "+1e2", "01e2", "01.2e2", "1e2i"} {
		if _, err := Parse([]byte(`{ products(where: {price: {gt: ` + raw + `}}) { id } }`)); err == nil {
			t.Errorf("accepted invalid number %s", raw)
		}
	}
}
