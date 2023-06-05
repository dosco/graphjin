package allow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	_log "log"
	"path/filepath"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/graph"
	lru "github.com/hashicorp/golang-lru"
)

type FS interface {
	Get(path string) (data []byte, err error)
	Put(path string, data []byte) (err error)
	Exists(path string) (exists bool, err error)
}

var ErrUnknownGraphQLQuery = errors.New("unknown graphql query")

const (
	queryPath = "/queries"
)

type Item struct {
	Namespace  string
	Operation  string
	Name       string
	ActionJSON map[string]json.RawMessage
	Query      []byte
	Fragments  []Fragment
}

type Fragment struct {
	Name  string
	Value []byte
}

type List struct {
	cache    *lru.TwoQueueCache
	saveChan chan Item
	fs       FS
}

func New(log *_log.Logger, fs FS, readOnly bool) (al *List, err error) {
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

	go func() {
		for {
			v, ok := <-al.saveChan
			if !ok {
				break
			}
			err = al.save(v)
			if err != nil && log != nil {
				log.Println("WRN allow list:", err)
			}
		}
	}()

	return al, err
}

func (al *List) Set(item Item) error {
	if al.saveChan == nil {
		return errors.New("allow list is read-only")
	}

	if len(item.Query) == 0 {
		return errors.New("empty query")
	}

	al.saveChan <- item
	return nil
}

func (al *List) GetByName(name string, useCache bool) (item Item, err error) {
	if useCache {
		if v, ok := al.cache.Get(name); ok {
			item = v.(Item)
			return
		}
	}

	fp := filepath.Join(queryPath, name)
	var ok bool

	if ok, err = al.fs.Exists((fp + ".gql")); err != nil {
		return
	} else if ok {
		item, err = al.get(queryPath, name, ".gql", useCache)
		return
	}

	if ok, err = al.fs.Exists((fp + ".graphql")); err != nil {
		return
	} else if ok {
		item, err = al.get(queryPath, name, ".gql", useCache)
	} else {
		err = ErrUnknownGraphQLQuery
	}
	return
}

func (al *List) get(queryPath, name, ext string, useCache bool) (item Item, err error) {
	queryNS, queryName := splitName(name)

	var query []byte
	query, err = readGQL(al.fs, filepath.Join(queryPath, (name+ext)))
	if err != nil {
		return
	}

	var h graph.FPInfo
	h, err = graph.FastParseBytes(query)
	if err != nil {
		return
	}

	var vars []byte

	jsonFile := filepath.Join(queryPath, (name + ".json"))
	ok, err := al.fs.Exists(jsonFile)
	if ok {
		vars, err = al.fs.Get(jsonFile)
	}
	if err != nil {
		return
	}

	item.Namespace = queryNS
	item.Operation = h.Operation
	item.Name = queryName
	item.Query = query

	if len(vars) != 0 {
		if err = json.Unmarshal(vars, &item.ActionJSON); err != nil {
			return
		}
	}

	if useCache {
		al.cache.Add(name, item)
	}
	return
}

func (al *List) save(item Item) (err error) {
	item.Name = strings.TrimSpace(item.Name)
	if item.Name == "" {
		err = errors.New("no query name defined: only named queries are saved to the allow list")
		return
	}
	return al.saveItem(item)
}

func (al *List) saveItem(item Item) (err error) {
	var queryFile string
	if item.Namespace != "" {
		queryFile = item.Namespace + "." + item.Name
	} else {
		queryFile = item.Name
	}

	fmap := make(map[string]struct{}, len(item.Fragments))
	var buf bytes.Buffer

	for _, f := range item.Fragments {
		var fragFile string
		if item.Namespace != "" {
			fragFile = item.Namespace + "." + f.Name
		} else {
			fragFile = f.Name
		}

		if _, ok := fmap[fragFile]; !ok {
			fh := fmt.Sprintf(`#import "./fragments/%s"`, fragFile)
			buf.WriteString(fh)
			buf.WriteRune('\n')
			fmap[fragFile] = struct{}{}
		}

		ff := filepath.Join(queryPath, "fragments", (fragFile + ".gql"))
		err = al.fs.Put(ff, []byte(f.Value))
		if err != nil {
			return
		}
	}
	if buf.Len() != 0 {
		buf.WriteRune('\n')
	}
	buf.Write(bytes.TrimSpace(item.Query))

	qf := filepath.Join(queryPath, (queryFile + ".gql"))
	err = al.fs.Put(qf, bytes.TrimSpace(buf.Bytes()))
	if err != nil {
		return
	}

	if len(item.ActionJSON) != 0 {
		var vars []byte
		jf := filepath.Join(queryPath, (queryFile + ".json"))
		vars, err = json.MarshalIndent(item.ActionJSON, "", "  ")
		if err != nil {
			return
		}
		err = al.fs.Put(jf, vars)
	}
	return
}

func splitName(v string) (string, string) {
	i := strings.LastIndex(v, ".")
	if i == -1 {
		return "", v
	} else if i < len(v)-1 {
		return v[:i], v[(i + 1):]
	}
	return "", ""
}
