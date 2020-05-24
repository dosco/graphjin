package allow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"

	"github.com/chirino/graphql/schema"
	"github.com/dosco/super-graph/jsn"
)

const (
	AL_QUERY int = iota + 1
	AL_VARS
)

type Item struct {
	Name    string
	key     string
	Query   string
	Vars    json.RawMessage
	Comment string
}

type List struct {
	filepath string
	saveChan chan Item
}

type Config struct {
	CreateIfNotExists bool
	Persist           bool
}

func New(filename string, conf Config) (*List, error) {
	al := List{}

	if filename != "" {
		fp := filename

		if _, err := os.Stat(fp); err == nil {
			al.filepath = fp
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if al.filepath == "" {
		fp := "./allow.list"

		if _, err := os.Stat(fp); err == nil {
			al.filepath = fp
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if al.filepath == "" {
		fp := "./config/allow.list"

		if _, err := os.Stat(fp); err == nil {
			al.filepath = fp
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}

	if al.filepath == "" {
		if !conf.CreateIfNotExists {
			return nil, errors.New("allow.list not found")
		}

		if filename == "" {
			al.filepath = "./config/allow.list"
		} else {
			al.filepath = filename
		}
	}

	var err error

	if conf.Persist {
		al.saveChan = make(chan Item)

		go func() {
			for v := range al.saveChan {
				if err = al.save(v); err != nil {
					break
				}
			}
		}()
	}

	if err != nil {
		return nil, err
	}

	return &al, nil
}

func (al *List) IsPersist() bool {
	return al.saveChan != nil
}

func (al *List) Set(vars []byte, query, comment string) error {
	if al.saveChan == nil {
		return errors.New("allow.list is read-only")
	}

	if query == "" {
		return errors.New("empty query")
	}

	var q string

	for i := 0; i < len(query); i++ {
		c := query[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			q = query
			break

		} else if c == '{' {
			q = "query " + query
			break
		}
	}

	al.saveChan <- Item{
		Comment: comment,
		Query:   q,
		Vars:    vars,
	}

	return nil
}

func (al *List) Load() ([]Item, error) {
	var list []Item
	varString := "variables"

	b, err := ioutil.ReadFile(al.filepath)
	if err != nil {
		return list, err
	}

	if len(b) == 0 {
		return list, nil
	}

	var comment bytes.Buffer
	var varBytes []byte

	itemMap := make(map[string]struct{})

	s, e, c := 0, 0, 0
	ty := 0

	for {
		fq := false

		if c == 0 && b[e] == '#' {
			s = e
			for e < len(b) && b[e] != '\n' {
				e++
			}
			if (e - s) > 2 {
				comment.Write(b[(s + 1):(e + 1)])
			}
		}

		if e >= len(b) {
			break
		}

		if matchPrefix(b, e, "query") || matchPrefix(b, e, "mutation") {
			if c == 0 {
				s = e
			}
			ty = AL_QUERY
		} else if matchPrefix(b, e, varString) {
			if c == 0 {
				s = e + len(varString) + 1
			}
			ty = AL_VARS
		} else if b[e] == '{' {
			c++

		} else if b[e] == '}' {
			c--

			if c == 0 {
				if ty == AL_QUERY {
					fq = true
				} else if ty == AL_VARS {
					varBytes = b[s:(e + 1)]
				}
				ty = 0
			}
		}

		if fq {
			query := string(b[s:(e + 1)])
			name := QueryName(query)
			key := strings.ToLower(name)

			if _, ok := itemMap[key]; !ok {
				v := Item{
					Name:    name,
					key:     key,
					Query:   query,
					Vars:    varBytes,
					Comment: comment.String(),
				}
				list = append(list, v)
				comment.Reset()
			}

			varBytes = nil

		}

		e++
		if e >= len(b) {
			break
		}
	}

	return list, nil
}

func (al *List) save(item Item) error {
	var buf bytes.Buffer

	qd := &schema.QueryDocument{}

	if err := qd.Parse(item.Query); err != nil {
		return err
	}

	qd.WriteTo(&buf)
	query := buf.String()
	buf.Reset()

	item.Name = QueryName(query)
	item.key = strings.ToLower(item.Name)

	if item.Name == "" {
		return nil
	}

	list, err := al.Load()
	if err != nil {
		return err
	}

	index := -1

	for i, v := range list {
		if strings.EqualFold(v.Name, item.Name) {
			index = i
			break
		}
	}

	if index != -1 {
		if list[index].Comment != "" {
			item.Comment = list[index].Comment
		}
		list[index] = item
	} else {
		list = append(list, item)
	}

	f, err := os.Create(al.filepath)
	if err != nil {
		return err
	}

	defer f.Close()

	sort.Slice(list, func(i, j int) bool {
		return strings.Compare(list[i].key, list[j].key) == -1
	})

	for _, v := range list {
		cmtLines := strings.Split(v.Comment, "\n")

		i := 0
		for _, c := range cmtLines {
			if c = strings.TrimSpace(c); c == "" {
				continue
			}

			_, err := f.WriteString(fmt.Sprintf("# %s\n", c))
			if err != nil {
				return err
			}
			i++
		}

		if i != 0 {
			if _, err := f.WriteString("\n"); err != nil {
				return err
			}
		} else {
			if _, err := f.WriteString(fmt.Sprintf("# Query named %s\n\n", v.Name)); err != nil {
				return err
			}
		}

		if len(v.Vars) != 0 && !bytes.Equal(v.Vars, []byte("{}")) {
			buf.Reset()

			if err := jsn.Clear(&buf, v.Vars); err != nil {
				return fmt.Errorf("failed to clean vars: %w", err)
			}
			vj := json.RawMessage(buf.Bytes())

			vj, err = json.MarshalIndent(vj, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal vars: %w", err)
			}

			_, err = f.WriteString(fmt.Sprintf("variables %s\n\n", vj))
			if err != nil {
				return err
			}
		}

		if v.Query[0] == '{' {
			_, err = f.WriteString(fmt.Sprintf("query %s\n\n", v.Query))
		} else {
			_, err = f.WriteString(fmt.Sprintf("%s\n\n", v.Query))
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func matchPrefix(b []byte, i int, s string) bool {
	if (len(b) - i) < len(s) {
		return false
	}
	for n := 0; n < len(s); n++ {
		if b[(i+n)] != s[n] {
			return false
		}
	}
	return true
}

func QueryName(b string) string {
	state, s := 0, 0

	for i := 0; i < len(b); i++ {
		switch {
		case state == 2 && !isValidNameChar(b[i]):
			return b[s:i]
		case state == 1 && b[i] == '{':
			return ""
		case state == 1 && isValidNameChar(b[i]):
			s = i
			state = 2
		case i != 0 && b[i] == ' ' && (b[i-1] == 'n' || b[i-1] == 'y'):
			state = 1
		}
	}

	return ""
}

func isValidNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}
