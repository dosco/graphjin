package allow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"
	"text/scanner"

	"github.com/chirino/graphql/schema"
	"github.com/dosco/super-graph/jsn"
)

const (
	expComment = iota + 1
	expVar
	expQuery
)

type Item struct {
	Name    string
	key     string
	Query   string
	Vars    string
	Comment string
}

type List struct {
	filepath string
	saveChan chan Item
}

type Config struct {
	CreateIfNotExists bool
	Persist           bool
	Log               *log.Logger
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

		if file, err := os.OpenFile(al.filepath, os.O_RDONLY|os.O_CREATE, 0644); err != nil {
			return nil, err
		} else {
			file.Close()
		}
	}

	var err error

	if conf.Persist {
		al.saveChan = make(chan Item)

		go func() {
			for v := range al.saveChan {
				err := al.save(v)

				if err != nil && conf.Log != nil {
					conf.Log.Println("WRN allow list save:", err)
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

	al.saveChan <- Item{
		Comment: comment,
		Query:   query,
		Vars:    string(vars),
	}

	return nil
}

func (al *List) Load() ([]Item, error) {
	b, err := ioutil.ReadFile(al.filepath)
	if err != nil {
		return nil, err
	}

	return parse(string(b), al.filepath)
}

func parse(b, filename string) ([]Item, error) {
	var items []Item

	var s scanner.Scanner
	s.Init(strings.NewReader(b))
	s.Filename = filename
	s.Mode ^= scanner.SkipComments

	var op, sp scanner.Position
	var item Item

	newComment := false
	st := expComment

	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		txt := s.TokenText()

		switch {
		case strings.HasPrefix(txt, "/*"):
			if st == expQuery {
				v := b[sp.Offset:s.Pos().Offset]
				item.Query = strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
				items = append(items, item)
			}
			item = Item{Comment: strings.TrimSpace(txt[2 : len(txt)-2])}
			sp = s.Pos()
			st = expComment
			newComment = true

		case !newComment && strings.HasPrefix(txt, "#"):
			if st == expQuery {
				v := b[sp.Offset:s.Pos().Offset]
				item.Query = strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
				items = append(items, item)
			}
			item = Item{}
			sp = s.Pos()
			st = expComment

		case strings.HasPrefix(txt, "variables"):
			if st == expComment {
				v := b[sp.Offset:s.Pos().Offset]
				item.Comment = strings.TrimSpace(v[:strings.IndexByte(v, '\n')])
			}
			sp = s.Pos()
			st = expVar

		case isGraphQL(txt):
			if st == expVar {
				v := b[sp.Offset:s.Pos().Offset]
				item.Vars = strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
			}
			sp = op
			st = expQuery

		}
		op = s.Pos()
	}

	if st == expQuery {
		v := b[sp.Offset:s.Pos().Offset]
		item.Query = strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
		items = append(items, item)
	}

	for i := range items {
		items[i].Name = QueryName(items[i].Query)
		items[i].key = strings.ToLower(items[i].Name)
	}

	return items, nil
}

func isGraphQL(s string) bool {
	return strings.HasPrefix(s, "query") ||
		strings.HasPrefix(s, "mutation") ||
		strings.HasPrefix(s, "subscription")
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

	for i, v := range list {
		var vars string
		if v.Vars != "" {
			buf.Reset()
			if err := jsn.Clear(&buf, []byte(v.Vars)); err != nil {
				continue
			}
			vj := json.RawMessage(buf.Bytes())

			if vj, err = json.MarshalIndent(vj, "", "  "); err != nil {
				continue
			}
			vars = string(vj)
		}
		list[i].Vars = vars
		list[i].Comment = strings.TrimSpace(v.Comment)
	}

	for _, v := range list {
		if v.Comment != "" {
			_, err = f.WriteString(fmt.Sprintf("/* %s */\n\n", v.Comment))
		} else {
			_, err = f.WriteString(fmt.Sprintf("/* %s */\n\n", v.Name))
		}

		if err != nil {
			return err
		}

		if v.Vars != "" {
			_, err = f.WriteString(fmt.Sprintf("variables %s\n\n", v.Vars))
			if err != nil {
				return err
			}
		}

		_, err = f.WriteString(fmt.Sprintf("%s\n\n", v.Query))
		if err != nil {
			return err
		}
	}

	return nil
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
