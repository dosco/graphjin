package serv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/dosco/super-graph/jsn"
)

// nolint: errcheck
func gqlHash(b string, vars []byte, role string) string {
	b = strings.TrimSpace(b)
	h := sha256.New()
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
		if whitespace(b[e]) {
			for e < len(b) && whitespace(b[e]) {
				e++
			}
			if e < len(b) {
				b1 = b[e]
			}
			if alphanum(b0) && alphanum(b1) {
				h.Write(space)
			}
		} else {
			starting = false
			s = e
			for e < len(b) && !whitespace(b[e]) {
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

	if len(vars) == 0 {
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

func whitespace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\t' || b == ','
}

func alphanum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func fatalInProd(err error, msg string) {
	var wg sync.WaitGroup

	if isDev() {
		log.Printf("ERR %s: %s", msg, err)
	} else {
		log.Fatalf("ERR %s: %s", msg, err)
	}

	wg.Add(1)
	wg.Wait()
}

func isDev() bool {
	return strings.HasPrefix(os.Getenv("GO_ENV"), "dev")
}

func sanitize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// Get path relative to cwd
func relpath(p string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return p
	}

	if strings.HasPrefix(p, cwd) {
		return "./" + strings.TrimLeft(p[len(cwd):], "/")
	}

	return p
}
