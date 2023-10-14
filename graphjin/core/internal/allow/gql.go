package allow

import (
	"bufio"
	"bytes"
	"io"
	"path/filepath"
	"regexp"
)

var incRe = regexp.MustCompile(`(?m)#import \"(.+)\"`)

func readGQL(fs FS, fname string) (gql []byte, err error) {
	var b bytes.Buffer

	ok, err := fs.Exists(fname)
	if !ok {
		err = ErrUnknownGraphQLQuery
	}
	if err != nil {
		return
	}

	if err = parseGQL(fs, fname, &b); err != nil {
		return
	}
	gql = b.Bytes()
	return
}

func parseGQL(fs FS, fname string, r io.Writer) (err error) {
	b, err := fs.Get(fname)
	if err != nil {
		return err
	}
	s := bufio.NewScanner(bytes.NewReader(b))
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

		fn := filepath.Join(filepath.Dir(fname), incFile)
		if err := parseGQL(fs, fn, r); err != nil {
			return err
		}
	}
	return
}
