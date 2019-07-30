package serv

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"
)

type allowItem struct {
	uri string
	gql string
}

var _allowList allowList

type allowList struct {
	list     map[string]*allowItem
	saveChan chan *allowItem
}

func initAllowList() {
	_allowList = allowList{
		list:     make(map[string]*allowItem),
		saveChan: make(chan *allowItem),
	}
	_allowList.load()

	go func() {
		for v := range _allowList.saveChan {
			_allowList.save(v)
		}
	}()
}

func (al *allowList) add(req *gqlReq) {
	if len(req.ref) == 0 || len(req.Query) == 0 {
		return
	}

	al.saveChan <- &allowItem{
		uri: req.ref,
		gql: req.Query,
	}
}

func (al *allowList) load() {
	filename := "./config/allow.list"
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return
	}

	b, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal(err)
	}

	if len(b) == 0 {
		return
	}

	var uri string

	s, e, c := 0, 0, 0

	for {
		if c == 0 && b[e] == '#' {
			s = e
			for b[e] != '\n' && e < len(b) {
				e++
			}
			if (e - s) > 2 {
				uri = strings.TrimSpace(string(b[(s + 1):e]))
			}
		}
		if b[e] == '{' {
			if c == 0 {
				s = e
			}
			c++
		} else if b[e] == '}' {
			c--
			if c == 0 {
				q := b[s:(e + 1)]
				al.list[gqlHash(q)] = &allowItem{
					uri: uri,
					gql: string(q),
				}
			}
		}

		e++
		if e >= len(b) {
			break
		}
	}
}

func (al *allowList) save(item *allowItem) {
	al.list[gqlHash([]byte(item.gql))] = item

	f, err := os.Create("./config/allow.list")
	if err != nil {
		panic(err)
	}

	defer f.Close()

	keys := []string{}
	urlMap := make(map[string][]string)

	for _, v := range al.list {
		urlMap[v.uri] = append(urlMap[v.uri], v.gql)
	}

	for k := range urlMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for i := range keys {
		k := keys[i]
		v := urlMap[k]

		f.WriteString(fmt.Sprintf("# %s\n\n", k))

		for i := range v {
			f.WriteString(fmt.Sprintf("query %s\n\n", v[i]))
		}
	}
}
