package allow

import (
	"bufio"
	"bytes"
	"io"
	"path/filepath"
	"regexp"

	"github.com/dosco/graphjin/v2/plugin"
)

var incRe = regexp.MustCompile(`(?m)#import \"(.+)\"`)

func readGQL(fs plugin.FS, fname string) ([]byte, error) {
	var b bytes.Buffer

	if err := parseGQL(fs, fname, &b); err == plugin.ErrNotFound {
		return nil, ErrUnknownGraphQLQuery
	} else if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

func parseGQL(fs plugin.FS, fname string, r io.Writer) (err error) {
	b, err := fs.ReadFile(fname)
	if err != nil {
		return err
	}
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		m := incRe.FindStringSubmatch(s.Text())
		if len(m) == 0 {
			r.Write(s.Bytes()) //nolint: errcheck
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
