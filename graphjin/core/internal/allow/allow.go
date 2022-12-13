package allow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	_log "log"
	"path/filepath"
	"strings"
	"text/scanner"

	"gopkg.in/yaml.v3"

	"github.com/chirino/graphql/schema"
	"github.com/dosco/graphjin/core/internal/graph"
	"github.com/dosco/graphjin/internal/jsn"
	"github.com/dosco/graphjin/plugin"
	lru "github.com/hashicorp/golang-lru"
)

var ErrUnknownGraphQLQuery = errors.New("unknown graphql query")

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
	Namespace string
	Name      string
	Operation string
	Query     string
	Vars      string
	frags     []Frag
}

type Frag struct {
	Name  string
	Value string
}

type List struct {
	cache    *lru.TwoQueueCache
	saveChan chan Item
	fs       plugin.FS
}

func New(log *_log.Logger, fs plugin.FS, readOnly bool) (al *List, err error) {
	if fs == nil {
		return nil, fmt.Errorf("no filesystem defined for the allow list")
	}
	al = &List{fs: fs}

	al.cache, err = lru.New2Q(1000)
	if err != nil {
		return
	}

	if readOnly {
		return
	}
	al.saveChan = make(chan Item)

	err = fs.CreateDir(filepath.Join(queryPath, fragmentPath))
	if err != nil {
		return
	}

	go func() {
		for {
			v, ok := <-al.saveChan
			if !ok {
				break
			}
			err = al.save(v, false)
			if err != nil && log != nil {
				log.Println("WRN allow list save:", err)
			}
		}
	}()

	return al, err
}

func (al *List) Set(vars json.RawMessage, query string, namespace string) error {
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

	item.Namespace = namespace
	item.Vars = string(vars)
	al.saveChan <- item
	return nil
}

func (al *List) Upgrade() (err error) {
	files, err := al.fs.ReadDir(queryPath)
	if err != nil {
		return fmt.Errorf("%w (%s)", err, queryPath)
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		ext := filepath.Ext(f.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		item, err := al.Get(filepath.Join(queryPath, f.Name()))
		if err != nil {
			return err
		}

		if err := al.save(item, false); err != nil {
			return err
		}
	}
	return
}

func (al *List) GetByName(name string, useCache bool) (item Item, err error) {
	if useCache {
		if v, ok := al.cache.Get(name); ok {
			item = v.(Item)
			return
		}
	}

	fpath := filepath.Join(queryPath, name)
	exts := []string{".gql", ".graphql", ".yml", ".yaml"}
	for _, ext := range exts {
		if item, err = al.Get((fpath + ext)); err == nil {
			break
		} else if err != plugin.ErrNotFound {
			return item, err
		}
	}

	if useCache && err == nil {
		al.cache.Add(name, item)
	}

	return
}

var errUnknownFileType = errors.New("not a graphql file")

func (al *List) Get(filePath string) (item Item, err error) {
	switch filepath.Ext(filePath) {
	case ".gql", ".graphql":
		return itemFromGQL(al.fs, filePath)
	case ".yml", ".yaml":
		return itemFromYaml(al.fs, filePath)
	default:
		return item, errUnknownFileType
	}
}

func itemFromYaml(fs plugin.FS, filePath string) (Item, error) {
	var item Item

	b, err := fs.ReadFile(filePath)
	if err != nil {
		return item, err
	}

	if err := yaml.Unmarshal(b, &item); err != nil {
		return item, err
	}

	h, err := graph.FastParse(item.Query)
	if err != nil {
		return item, err
	}
	item.Operation = h.Operation

	qi, err := parseQuery(item.Query)
	if err != nil {
		return item, err
	}

	for _, f := range qi.frags {
		b, err := fs.ReadFile(filepath.Join(fragmentPath, f.Name))
		if err != nil {
			return item, err
		}
		item.frags = append(item.frags, Frag{Name: f.Name, Value: string(b)})
	}

	return item, nil
}

func itemFromGQL(fs plugin.FS, filePath string) (item Item, err error) {
	fn := filepath.Base(filePath)
	fn = strings.TrimSuffix(fn, filepath.Ext(fn))
	queryNS, queryName := splitName(fn)

	if queryName == "" {
		return item, fmt.Errorf("invalid filename: %s", filePath)
	}

	query, err := parseGQLFile(fs, filePath)
	if err != nil {
		return item, err
	}

	h, err := graph.FastParse(query)
	if err != nil {
		return item, err
	}

	item.Namespace = queryNS
	item.Operation = h.Operation
	item.Name = queryName
	item.Query = query
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
	period := 0
	frags := make(map[string]struct{})

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
		default:
			if period == 3 && txt != "." {
				frags[txt] = struct{}{}
			}
			if period != 3 && txt == "." {
				period++
			} else {
				period = 0
			}
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

	for k := range frags {
		item.frags = append(item.frags, Frag{Name: k})
	}
	return item, nil
}

func setValue(st int, v string, item Item) (Item, error) {
	val := func() string {
		return strings.TrimSpace(v[:strings.LastIndexByte(v, '}')+1])
	}
	switch st {
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

func (al *List) save(item Item, safe bool) error {
	var buf bytes.Buffer
	var err error

	qd := &schema.QueryDocument{}
	if err := qd.Parse(item.Query); err != nil {
		return err
	}

	qvars := strings.TrimSpace(item.Vars)
	if qvars != "" && qvars != "{}" {
		if err := jsn.Clear(&buf, []byte(qvars)); err != nil {
			return err
		}

		vj := json.RawMessage(buf.Bytes())
		if vj, err = json.MarshalIndent(vj, "", "  "); err != nil {
			return err
		}
		buf.Reset()

		d := schema.Directive{
			Name: "json",
			Args: schema.ArgumentList{{
				Name:  "schema:",
				Value: schema.ToLiteral(string(vj)),
			}},
		}
		qd.Operations[0].Directives = append(qd.Operations[0].Directives, &d)
		// Bug in chirino/graphql forces us to add the space after the query name
		qd.Operations[0].Name = qd.Operations[0].Name + " "
	}
	qd.WriteTo(&buf)

	item.Name = strings.TrimSpace(qd.Operations[0].Name)
	if item.Name == "" {
		return errors.New("no query name defined: only named queries are saved to the allow list")
	}

	return al.saveItem(
		item.Namespace,
		item.Name,
		buf.String(),
		item.frags,
		safe)
}

func (al *List) saveItem(
	ns, name, content string, frags []Frag, safe bool) error {

	var qfn string
	if ns != "" {
		qfn = ns + "." + name + ".gql"
	} else {
		qfn = name + ".gql"
	}

	var gqlContent bytes.Buffer
	fmap := make(map[string]struct{})

	for _, fv := range frags {
		var fn string
		if ns != "" {
			fn = ns + "." + fv.Name
		} else {
			fn = fv.Name
		}
		fn += ".gql"

		if _, ok := fmap[fn]; !ok {
			fh := fmt.Sprintf(`#import "./fragments/%s"`, fn)
			gqlContent.WriteString(fh)
			gqlContent.WriteRune('\n')
			fmap[fn] = struct{}{}
		}

		fragFile := filepath.Join(queryPath, "fragments", fn)
		if safe {
			if ok, err := al.fs.Exists(fragFile); ok {
				continue
			} else if err != nil {
				return err
			}
		}

		err := al.fs.CreateFile(fragFile, []byte(fv.Value))
		if err != nil {
			return err
		}
	}
	if gqlContent.Len() != 0 {
		gqlContent.WriteRune('\n')
	}
	gqlContent.WriteString(content)

	queryFile := filepath.Join(queryPath, qfn)
	if safe {
		if ok, err := al.fs.Exists(queryFile); ok {
			return nil
		} else if err != nil {
			return err
		}
	}

	err := al.fs.CreateFile(queryFile, gqlContent.Bytes())
	if err != nil {
		return err
	}
	return nil
}

// func (al *List) fetchFragment(namespace, name string) (string, error) {
// 	var fn string
// 	if namespace != "" {
// 		fn = namespace + "." + name
// 	} else {
// 		fn = name
// 	}
// 	v, err := al.fs.ReadFile(filepath.Join(fragmentPath, fn))
// 	if err != nil {
// 		return "", err
// 	}
// 	return string(v), err
// }

func splitName(v string) (string, string) {
	i := strings.LastIndex(v, ".")
	if i == -1 {
		return "", v
	} else if i < len(v)-1 {
		return v[:i], v[(i + 1):]
	}
	return "", ""
}
