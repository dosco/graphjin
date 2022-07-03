package allow

import (
	"bufio"
	"bytes"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/afero"
)

var incRe = regexp.MustCompile(`(?m)#import \"(.+)\"`)

func parseGQLFile(fs afero.Fs, fname string) (string, error) {
	var sb strings.Builder

	if err := parseGQL(fs, fname, &sb); err != nil {
		return "", err
	}

	return sb.String(), nil
}

func parseGQL(fs afero.Fs, fname string, sb *strings.Builder) error {
	b, err := afero.ReadFile(fs, fname)
	if err != nil {
		return err
	}

	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		m := incRe.FindStringSubmatch(s.Text())
		if len(m) == 0 {
			sb.Write(s.Bytes())
			continue
		}

		fn := filepath.Join(filepath.Dir(fname), m[1])
		if err := parseGQL(fs, fn, sb); err != nil {
			return err
		}
	}

	return nil
}
