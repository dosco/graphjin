package allow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/scanner"

	"gopkg.in/yaml.v3"

	"github.com/chirino/graphql/schema"
	"github.com/dosco/graphjin/internal/jsn"
)

const (
	expComment = iota + 1
	expVar
	expQuery
	expFrag
)

type Item struct {
	Name     string
	Comment  string `yaml:",omitempty"`
	key      string
	Query    string
	Vars     string   `yaml:",omitempty"`
	Metadata Metadata `yaml:",inline,omitempty"`
	frags    []Frag
}

type Metadata struct {
	Order struct {
		Var    string   `yaml:"var,omitempty"`
		Values []string `yaml:"values,omitempty"`
	} `yaml:",omitempty"`
}

type Frag struct {
	Name  string
	Value string
}

type List struct {
	saveChan     chan Item
	filepath     string
	queryPath    string
	fragmentPath string
}

type Config struct {
	Log *log.Logger
}

func New(cpath string, conf Config) (*List, error) {
	var err error
	var ap string

	al := List{saveChan: make(chan Item)}

	if cpath == "" {
		ap = "./config"
	} else {
		ap = cpath
	}

	al.queryPath = path.Join(ap, "queries")
	al.fragmentPath = path.Join(ap, "fragments")

	if err := os.MkdirAll(al.queryPath, os.ModePerm); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(al.fragmentPath, os.ModePerm); err != nil {
		return nil, err
	}

	go func() {
		for v := range al.saveChan {
			err := al.save(v)

			if err != nil && conf.Log != nil {
				conf.Log.Println("WRN allow list save:", err)
			}
		}
	}()

	if err != nil {
		return nil, err
	}

	return &al, nil
}

func (al *List) Set(vars []byte, query string, md Metadata) error {
	if al.saveChan == nil {
		return errors.New("allow list is read-only")
	}

	if query == "" {
		return errors.New("empty query")
	}

	item, err := parseQuery(query)
	if err != nil {
		return err
	}

	item.Vars = string(vars)
	item.Metadata = md
	al.saveChan <- item
	return nil
}

func (al *List) Load() ([]Item, error) {
	var items []Item

	files, err := ioutil.ReadDir(al.queryPath)
	if err != nil {
		return nil, fmt.Errorf("allow list: %w", err)
	}

	for _, f := range files {
		var item Item
		var mi bool

		fn := f.Name()
		fn = strings.TrimSuffix(fn, filepath.Ext(fn))

		// migrate if old file exists
		oldFile := path.Join(al.queryPath, fn)
		if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
			b, err := ioutil.ReadFile(oldFile)
			if err != nil {
				return nil, err
			}

			item, err = parseQuery(string(b))
			if err != nil {
				return nil, err
			}
			mi = true
		}

		newFile := path.Join(al.queryPath, (fn + ".yaml"))
		if mi {
			b, err := yaml.Marshal(&item)
			if err != nil {
				return nil, err
			}
			if err := ioutil.WriteFile(newFile, b, 0644); err != nil {
				return nil, err
			}

		} else {
			b, err := ioutil.ReadFile(newFile)
			if err != nil {
				return nil, err
			}
			if err := yaml.Unmarshal(b, &item); err != nil {
				return nil, err
			}
		}

		items = append(items, item)
	}

	return items, nil
}

func parseQuery(b string) (Item, error) {
	var s scanner.Scanner
	s.Init(strings.NewReader(b))
	s.Mode ^= scanner.SkipComments

	var op, sp scanner.Position
	var item Item
	var err error

	st := expComment

	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		txt := s.TokenText()

		switch {
		case strings.HasPrefix(txt, "/*"):
			v := b[sp.Offset:s.Pos().Offset]
			item, err = setValue(st, v, item)
			sp = s.Pos()

		case strings.HasPrefix(txt, "variables"):
			v := b[sp.Offset:s.Pos().Offset]
			item, err = setValue(st, v, item)
			sp = s.Pos()
			st = expVar

		case isGraphQL(txt):
			v := b[sp.Offset:s.Pos().Offset]
			item, err = setValue(st, v, item)
			sp = op
			st = expQuery

		case strings.HasPrefix(txt, "fragment"):
			v := b[sp.Offset:s.Pos().Offset]
			item, err = setValue(st, v, item)
			sp = op
			st = expFrag
		}

		if err != nil {
			return item, err
		}

		op = s.Pos()
	}

	if st == expQuery || st == expFrag {
		v := b[sp.Offset:s.Pos().Offset]
		item, err = setValue(st, v, item)
	}

	if err != nil {
		return item, err
	}

	item.Name = QueryName(item.Query)
	item.key = strings.ToLower(item.Name)
	return item, nil
}

func setValue(st int, v string, item Item) (Item, error) {
	val := func() string {
		return strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
	}
	switch st {
	case expComment:
		item.Comment = val()

	case expVar:
		item.Vars = val()

	case expQuery:
		item.Query = val()

	case expFrag:
		f := Frag{Value: val()}
		f.Name = QueryName(f.Value)
		item.frags = append(item.frags, f)
	}

	return item, nil
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

	if err := al.saveItem(item, path.Dir(al.filepath), true); err != nil {
		return err
	}

	return nil
}

func (al *List) saveItem(item Item, ap string, ow bool) error {
	var err error

	if item.Vars != "" {
		var buf bytes.Buffer

		if err := jsn.Clear(&buf, []byte(item.Vars)); err != nil {
			return err
		}

		vj := json.RawMessage(buf.Bytes())
		if vj, err = json.MarshalIndent(vj, "", "  "); err != nil {
			return err
		}
		item.Vars = string(vj)
	}

	b, err := yaml.Marshal(&item)
	if err != nil {
		return err
	}

	fn := path.Join(al.queryPath, (item.Name + ".yaml"))
	if err := ioutil.WriteFile(fn, b, 0644); err != nil {
		return err
	}

	for _, fv := range item.frags {
		fn := path.Join(al.fragmentPath, fv.Name)
		b := []byte(fv.Value)

		if err := ioutil.WriteFile(fn, b, 0644); err != nil {
			return err
		}
	}

	return nil
}

func (al *List) FragmentFetcher() func(name string) (string, error) {
	return func(name string) (string, error) {
		v, err := ioutil.ReadFile(path.Join(al.fragmentPath, name))
		return string(v), err
	}
}

// func (al *List) GetQuery(name string) (Item, error) {
// 	var item Item
// 	var err error

// 	b, err := ioutil.ReadFile(path.Join(al.queryPath, (name + ".yaml")))
// 	if err == nil {
// 		return item, err
// 	}

// 	return parseYAML(b)
// }

// func parseYAML(b []byte) (Item, error) {
// 	var item Item
// 	err := yaml.Unmarshal(b, &item)
// 	return item, err
// }
