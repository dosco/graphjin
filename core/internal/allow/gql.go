package allow

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
)

var incRe = regexp.MustCompile(`(?m)#import \"(.+)\"`)

var ErrInvalidImportPath = fmt.Errorf("%w: invalid import path", ErrUnknownGraphQLQuery)

// ReadGQL reads a graphql file and resolves all imports.
func ReadGQL(fs FS, fname string) (gql []byte, err error) {
	var b bytes.Buffer

	ok, err := fs.Exists(fname)
	if !ok {
		err = ErrUnknownGraphQLQuery
	}
	if err != nil {
		return
	}

	if err = parseGQL(fs, fname, filepath.Dir(fname), &b); err != nil {
		return
	}
	gql = b.Bytes()
	return
}

// readGQL reads a graphql file and resolves all imports.
func readGQL(fs FS, fname string) (gql []byte, err error) {
	return ReadGQL(fs, fname)
}

// parseGQL parses a graphql file and resolves all imports
func parseGQL(fs FS, fname, rootDir string, r io.Writer) (err error) {
	b, err := fs.Get(fname)
	if err != nil {
		return err
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	// The complete file is already in memory. A compact query or imported
	// fragment can exceed Scanner's default 64KiB token limit, so allow any
	// line in this file plus space for the terminating newline.
	s.Buffer(nil, len(b)+1)
	for s.Scan() {
		m := incRe.FindStringSubmatch(s.Text())
		if len(m) == 0 {
			r.Write(s.Bytes()) //nolint:errcheck
			r.Write([]byte("\n"))
			continue
		}

		incFile := m[1]
		if filepath.Ext(incFile) == "" {
			incFile += ".gql"
		}

		fn, err := resolveImportPath(rootDir, filepath.Dir(fname), incFile)
		if err != nil {
			return err
		}
		if err := parseGQL(fs, fn, rootDir, r); err != nil {
			return err
		}
	}
	return s.Err()
}

func resolveImportPath(rootDir, currentDir, incFile string) (string, error) {
	incFile = strings.TrimSpace(incFile)
	if incFile == "" || filepath.IsAbs(incFile) || strings.Contains(incFile, `\`) {
		return "", ErrInvalidImportPath
	}

	cleaned := filepath.Clean(incFile)
	if cleaned == "." || cleaned == ".." {
		return "", ErrInvalidImportPath
	}

	candidate := filepath.Join(currentDir, cleaned)
	rel, err := filepath.Rel(rootDir, candidate)
	if err != nil {
		return "", ErrInvalidImportPath
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidImportPath
	}

	return candidate, nil
}
