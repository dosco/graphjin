package allow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/scanner"

	"gopkg.in/yaml.v3"

	"github.com/chirino/graphql/schema"
	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/internal/jsn"
	"github.com/spf13/afero"
)

const (
	expComment = iota + 1
	expVar
	expQuery
	expFrag
)

const (
	queryPath    = "/queries"
	fragmentPath = "/fragments"
)

type Item struct {
	Namespace string `yaml:",omitempty"`
	Name      string
	Comment   string `yaml:",omitempty"`
	key       string
	Query     string
	Vars      string   `yaml:",omitempty"`
	Metadata  Metadata `yaml:",inline,omitempty"`
	frags     []Frag
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
	saveChan chan Item
	fs       afero.Fs
}

type Config struct {
	Log *log.Logger
}

func New(conf Config, fs afero.Fs) (*List, error) {
	if fs == nil {
		return nil, fmt.Errorf("no filesystem defined for the allow list")
	}

	al := List{saveChan: make(chan Item), fs: fs}

	_ = fs.MkdirAll(queryPath, os.ModePerm)
	_ = fs.MkdirAll(fragmentPath, os.ModePerm)

	var err error

	go func() {
		for {
			v, ok := <-al.saveChan
			if !ok {
				break
			}
			err = al.save(v)
			if err != nil && conf.Log != nil {
				conf.Log.Println("WRN allow list save:", err)
				break
			}
		}
	}()

	return &al, err
}

func (al *List) Set(vars []byte, query string, md Metadata, namespace string) error {
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

	// item.Name = name
	item.Namespace = namespace
	item.Vars = string(vars)
	item.Metadata = md
	al.saveChan <- item
	return nil
}

func (al *List) Load() ([]Item, error) {
	var items []Item

	files, err := afero.ReadDir(al.fs, queryPath)
	if err != nil {
		return nil, fmt.Errorf("allow list: %w", err)
	}

	for _, f := range files {
		fn := f.Name()

		if f.IsDir() {
			continue
		}

		fn = strings.TrimSuffix(fn, filepath.Ext(fn))

		if item, err := al.Get(fn); err == nil {
			items = append(items, item)
		} else {
			return nil, err
		}
	}

	return items, nil
}

func (al *List) Get(name string) (Item, error) {
	var item Item

	filePath := path.Join(queryPath, name)

	fn := (filePath + ".gql")
	if ok, err := afero.Exists(al.fs, fn); ok {
		return itemFromGQL(al.fs, fn)
	} else if err != nil {
		return item, err
	}

	fn = (filePath + ".graphql")
	if ok, err := afero.Exists(al.fs, fn); ok {
		return itemFromGQL(al.fs, fn)
	} else if err != nil {
		return item, err
	}

	fn = (filePath + ".yaml")
	if ok, err := afero.Exists(al.fs, fn); ok {
		return itemFromYaml(al.fs, fn)
	} else if err != nil {
		return item, err
	}

	return item, nil
}

func itemFromYaml(fs afero.Fs, fn string) (Item, error) {
	var item Item

	b, err := afero.ReadFile(fs, fn)
	if err != nil {
		return item, err
	}

	if err := yaml.Unmarshal(b, &item); err != nil {
		return item, err
	}
	return item, nil
}

func itemFromGQL(fs afero.Fs, fn string) (Item, error) {
	var item Item

	query, err := parseGQLFile(fs, fn)
	if err != nil {
		return item, err
	}
	h, err := graph.FastParse(query)
	if err != nil {
		return item, err
	}
	item.Query = query
	item.Name = h.Name
	item.key = strings.ToLower(item.Name)

	return item, nil
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
		f.Name = fragmentName(f.Value)
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

	h, err := graph.FastParse(query)
	if err != nil {
		return err
	}

	item.Name = h.Name
	item.key = strings.ToLower(item.Name)

	if item.Name == "" {
		return errors.New("no name defined for query")
	}

	if err := al.saveItem(item, true); err != nil {
		return err
	}

	return nil
}

func (al *List) saveItem(item Item, ow bool) error {
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

	var b bytes.Buffer
	y := yaml.NewEncoder(&b)
	y.SetIndent(2)
	err = y.Encode(&item)
	if err != nil {
		return err
	}

	var fn string
	if item.Namespace != "" {
		fn = item.Namespace + "." + item.Name + ".yaml"
	} else {
		fn = item.Name + ".yaml"
	}

	if err := afero.WriteFile(
		al.fs,
		filepath.Join(queryPath, fn),
		b.Bytes(),
		0600); err != nil {
		return err
	}

	for _, fv := range item.frags {
		if item.Namespace != "" {
			fn = item.Namespace + "." + fv.Name
		} else {
			fn = fv.Name
		}
		err := afero.WriteFile(
			al.fs,
			filepath.Join(fragmentPath, fn),
			[]byte(fv.Value),
			0600)

		if err != nil {
			return err
		}
	}

	return nil
}

func (al *List) FragmentFetcher(namespace string) func(name string) (string, error) {
	return func(name string) (string, error) {
		var fn string
		if namespace != "" {
			fn = namespace + "." + name
		} else {
			fn = name
		}
		v, err := afero.ReadFile(
			al.fs,
			filepath.Join(fragmentPath, fn))

		return string(v), err
	}
}
