package serv

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"sort"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/dosco/super-graph/jsn"
)

func mkkey(h *xxhash.Digest, k1 string, k2 string) uint64 {
	h.WriteString(k1)
	h.WriteString(k2)
	v := h.Sum64()
	h.Reset()

	return v
}

func gqlHash(b string, vars []byte, role string) string {
	b = strings.TrimSpace(b)
	h := sha1.New()
	query := "query"

	s, e := 0, 0
	space := []byte{' '}
	starting := true

	var b0, b1 byte

	if len(b) == 0 {
		return ""
	}

	for {
		if starting && b[e] == 'q' {
			n := 0
			se := e
			for e < len(b) && n < len(query) && b[e] == query[n] {
				n++
				e++
			}
			if n != len(query) {
				io.WriteString(h, strings.ToLower(b[se:e]))
			}
		}
		if e >= len(b) {
			break
		}
		if ws(b[e]) {
			for e < len(b) && ws(b[e]) {
				e++
			}
			if e < len(b) {
				b1 = b[e]
			}
			if al(b0) && al(b1) {
				h.Write(space)
			}
		} else {
			starting = false
			s = e
			for e < len(b) && ws(b[e]) == false {
				e++
			}
			if e != 0 {
				b0 = b[(e - 1)]
			}
			io.WriteString(h, strings.ToLower(b[s:e]))
		}
		if e >= len(b) {
			break
		}
	}

	if len(role) != 0 {
		io.WriteString(h, role)
	}

	if vars == nil || len(vars) == 0 {
		return hex.EncodeToString(h.Sum(nil))
	}

	fields := jsn.Keys([]byte(vars))

	sort.Slice(fields, func(i, j int) bool {
		return bytes.Compare(fields[i], fields[j]) == -1
	})

	for i := range fields {
		h.Write(fields[i])
	}

	return hex.EncodeToString(h.Sum(nil))
}

func ws(b byte) bool {
	return b == ' ' || b == '\n' || b == '\t' || b == ','
}

func al(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func gqlName(b string) string {
	state, s := 0, 0

	for i := 0; i < len(b); i++ {
		switch {
		case state == 2 && b[i] == '{':
			return b[s:i]
		case state == 2 && b[i] == ' ':
			return b[s:i]
		case state == 1 && b[i] == '{':
			return ""
		case state == 1 && b[i] != ' ':
			s = i
			state = 2
		case state == 1 && b[i] == ' ':
			continue
		case i != 0 && b[i] == ' ' && (b[i-1] == 'n' || b[i-1] == 'y'):
			state = 1
		}
	}

	return ""
}

func findStmt(role string, stmts []stmt) *stmt {
	for i := range stmts {
		if stmts[i].role.Name != role {
			continue
		}
		return &stmts[i]
	}
	return nil
}
