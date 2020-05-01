package core

import "testing"

func TestEscQuote(t *testing.T) {
	val := "That's the worst, don''t be calling me's again"
	exp := "That''s the worst, don''''t be calling me''s again"
	ret := escSQuote([]byte(val))

	if exp != string(ret) {
		t.Errorf("escSQuote failed: %s", string(ret))
	}
}
