//go:build goexperiment.jsonv2

package jsn_test

import (
	"bytes"
	"io"
	"testing"

	"encoding/json/jsontext"
)

func BenchmarkJSONTextTokenScan(b *testing.B) {
	data := []byte(input2)
	keys := map[string]struct{}{
		"id":           {},
		"full_name":    {},
		"embed":        {},
		"email":        {},
		"__twitter_id": {},
	}

	var matches int
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		dec := jsontext.NewDecoder(bytes.NewReader(data))
		for {
			tok, err := dec.ReadToken()
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Fatal(err)
			}
			if tok.Kind() != '"' {
				continue
			}
			if _, ok := keys[tok.String()]; ok {
				matches++
			}
		}
	}
	if matches == 0 {
		b.Fatal("no token matches")
	}
}
