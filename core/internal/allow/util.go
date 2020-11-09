package allow

import (
	"os"
	"path"
	"strings"
)

func (al *List) makeDir() (string, error) {
	ap := path.Dir(al.filepath)
	if al.pathExists {
		return ap, nil
	}
	if err := os.MkdirAll(path.Join(ap, "queries"), os.ModePerm); err != nil {
		return ap, err
	}
	if err := os.MkdirAll(path.Join(ap, "fragments"), os.ModePerm); err != nil {
		return ap, err
	}
	al.pathExists = true
	return ap, nil
}

func (al *List) setFilePath(filename string) error {
	if filename != "" {
		fp := filename

		if _, err := os.Stat(fp); err == nil {
			al.filepath = fp
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	if al.filepath == "" {
		fp := "./allow.list"

		if _, err := os.Stat(fp); err == nil {
			al.filepath = fp
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	if al.filepath == "" {
		fp := "./config/allow.list"

		if _, err := os.Stat(fp); err == nil {
			al.filepath = fp
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	return nil
}

func QueryName(b string) string {
	state, s := 0, 0
	bl := len(b)

	for i := 0; i < bl; i++ {
		switch {
		case state == 2 && !isValidNameChar(b[i]):
			return b[s:i]
		case state == 1 && b[i] == '{':
			return ""
		case state == 1 && isValidNameChar(b[i]):
			s = i
			state = 2
		case i != 0 && b[i] == ' ' && (b[i-1] == 'n' || b[i-1] == 'y' || b[i-1] == 't'):
			state = 1
		}
	}

	return ""
}

func isValidNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func isGraphQL(s string) bool {
	return strings.HasPrefix(s, "query") ||
		strings.HasPrefix(s, "mutation") ||
		strings.HasPrefix(s, "subscription")
}
