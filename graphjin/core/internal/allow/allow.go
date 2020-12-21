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
	"strings"
	"text/scanner"

	"github.com/chirino/graphql/schema"
	"github.com/dosco/graphjin/jsn"
)

const (
	expComment = iota + 1
	expVar
	expQuery
	expFrag
)

type Item struct {
	Name    string
	Comment string
	key     string
	Query   string
	Vars    string
	frags   []Frag
}

type Frag struct {
	Name  string
	Value string
}

type List struct {
	saveChan   chan Item
	pathExists bool

	filepath     string
	queryPath    string
	fragmentPath string
}

type Config struct {
	Log *log.Logger
}

func New(filename string, conf Config) (*List, error) {
	var err error
	var ap string
	var mig bool

	al := List{saveChan: make(chan Item)}

	if err := al.setFilePath(filename); err != nil {
		return nil, err
	}

	if al.filepath == "" {
		ap = path.Dir(filename)
	} else {
		ap = path.Dir(al.filepath)
		mig = true
	}

	al.queryPath = path.Join(ap, "queries")
	al.fragmentPath = path.Join(ap, "fragments")

	if mig {
		if err := al.migrate(); err != nil {
			return nil, err
		}
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

func (al *List) Set(vars []byte, query string) error {
	if al.saveChan == nil {
		return errors.New("allow.list is read-only")
	}

	if query == "" {
		return errors.New("empty query")
	}

	items, err := parse(query)
	if err != nil {
		return err
	}

	if len(items) != 0 {
		items[0].Vars = string(vars)
		al.saveChan <- items[0]
	}

	return nil
}

func (al *List) loadFile() ([]Item, error) {
	b, err := ioutil.ReadFile(al.filepath)
	if err != nil {
		return nil, err
	}

	return parse(string(b))
}

func (al *List) Load() ([]Item, error) {
	var items []Item

	if _, err := os.Stat(al.queryPath); os.IsNotExist(err) {
		if err := os.Mkdir(al.queryPath, os.ModePerm); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	if _, err := os.Stat(al.fragmentPath); os.IsNotExist(err) {
		if err := os.Mkdir(al.fragmentPath, os.ModePerm); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	files, err := ioutil.ReadDir(al.queryPath)
	if err != nil {
		return nil, fmt.Errorf("allow list: %w", err)
	}

	for _, f := range files {
		fn := path.Join(al.queryPath, f.Name())
		b, err := ioutil.ReadFile(fn)
		if err != nil {
			return nil, fmt.Errorf("allow list: %w", err)
		}
		item, err := parse(string(b))
		if err != nil {
			return nil, err
		}
		items = append(items, item[0])
	}

	return items, nil
}

func (al *List) FragmentFetcher() func(name string) (string, error) {
	return func(name string) (string, error) {
		v, err := ioutil.ReadFile(path.Join(al.fragmentPath, name))
		return string(v), err
	}
}

func (al *List) GetQuery(name string) (Item, error) {
	v, err := ioutil.ReadFile(path.Join(al.queryPath, name))
	if err == nil {
		return Item{}, err
	}

	items, err := parse(string(v))
	if err != nil {
		return Item{}, err
	}
	return items[0], nil
}

func parse(b string) ([]Item, error) {
	var items []Item

	var s scanner.Scanner
	s.Init(strings.NewReader(b))
	s.Mode ^= scanner.SkipComments

	var op, sp scanner.Position
	var item Item

	st := expComment

	for tok := s.Scan(); tok != scanner.EOF; tok = s.Scan() {
		txt := s.TokenText()

		switch {
		case strings.HasPrefix(txt, "/*"):
			v := b[sp.Offset:s.Pos().Offset]

			if st == expQuery {
				item.Query = strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
				items = append(items, item)
			}

			if st == expFrag {
				f := Frag{Value: strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])}
				f.Name = QueryName(f.Value)
				item.frags = append(item.frags, f)
				items = append(items, item)
			}

			item = Item{}
			sp = s.Pos()

		case strings.HasPrefix(txt, "variables"):
			sp = s.Pos()
			st = expVar

		case isGraphQL(txt):
			if st == expVar {
				v := b[sp.Offset:s.Pos().Offset]
				item.Vars = strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
			}
			sp = op
			st = expQuery

		case strings.HasPrefix(txt, "fragment"):
			v := b[sp.Offset:s.Pos().Offset]

			if st == expVar {
				item.Vars = strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
			}

			if st == expQuery {
				item.Query = strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
			}

			if st == expFrag {
				f := Frag{Value: strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])}
				f.Name = QueryName(f.Value)
				item.frags = append(item.frags, f)
			}

			sp = op
			st = expFrag
		}
		op = s.Pos()
	}
	v := b[sp.Offset:s.Pos().Offset]

	if st == expQuery {
		item.Query = strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
		items = append(items, item)
	}

	if st == expFrag {
		f := Frag{Value: strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])}
		f.Name = QueryName(f.Value)
		item.frags = append(item.frags, f)
		items = append(items, item)
	}

	for i := range items {
		items[i].Name = QueryName(items[i].Query)
		items[i].key = strings.ToLower(items[i].Name)
	}

	return items, nil
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

func (al *List) migrate() error {
	list, err := al.loadFile()
	if err != nil {
		return err
	}

	ap, err := al.makeDir()
	if err != nil {
		return err
	}

	for _, v := range list {
		if err := al.saveItem(v, ap, false); err != nil {
			return err
		}
	}

	return nil
}

func (al *List) saveItem(v Item, ap string, ow bool) error {
	fn := path.Join(al.queryPath, v.Name)
	oq := true

	if !ow {
		if _, err := os.Stat(fn); err != nil && !os.IsNotExist(err) {
			return err
		} else if err == nil {
			oq = false
		}
	}

	if oq {
		f, err := os.Create(fn)
		if err != nil {
			return err
		}
		defer f.Close()

		if v.Vars != "" {
			var buf bytes.Buffer

			if err := jsn.Clear(&buf, []byte(v.Vars)); err != nil {
				return err
			}
			vj := json.RawMessage(buf.Bytes())

			if vj, err = json.MarshalIndent(vj, "", "  "); err != nil {
				return err
			}
			v.Vars = string(vj)
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

	for _, fv := range v.frags {
		fn := path.Join(al.fragmentPath, fv.Name)
		of := true

		if !ow {
			if _, err := os.Stat(fn); err != nil && !os.IsNotExist(err) {
				return err
			} else if err == nil {
				of = false
			}
		}

		if of {
			f, err := os.Create(fn)
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = f.WriteString(fmt.Sprintf("%s\n\n", fv.Value))
			if err != nil {
				return err
			}
		}
	}

	return nil
}
