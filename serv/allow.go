package serv

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"
)

const (
	AL_QUERY int = iota + 1
	AL_VARS
)

type allowItem struct {
	uri  string
	gql  string
	vars json.RawMessage
}

var _allowList allowList

type allowList struct {
	list     map[string]*allowItem
	filepath string
	saveChan chan *allowItem
}

func initAllowList(path string) {
	_allowList = allowList{
		list:     make(map[string]*allowItem),
		saveChan: make(chan *allowItem),
	}

	if len(path) != 0 {
		fp := fmt.Sprintf("%s/allow.list", path)

		if _, err := os.Stat(fp); err == nil {
			_allowList.filepath = fp
		} else if !os.IsNotExist(err) {
			panic(err)
		}
	}

	if len(_allowList.filepath) == 0 {
		fp := "./allow.list"

		if _, err := os.Stat(fp); err == nil {
			_allowList.filepath = fp
		} else if !os.IsNotExist(err) {
			panic(err)
		}
	}

	if len(_allowList.filepath) == 0 {
		fp := "./config/allow.list"

		if _, err := os.Stat(fp); err == nil {
			_allowList.filepath = fp
		} else if !os.IsNotExist(err) {
			panic(err)
		}
	}

	if len(_allowList.filepath) == 0 {
		panic("allow.list not found")
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
		uri:  req.ref,
		gql:  req.Query,
		vars: req.Vars,
	}
}

func (al *allowList) load() {
	b, err := ioutil.ReadFile(al.filepath)
	if err != nil {
		log.Fatal(err)
	}

	if len(b) == 0 {
		return
	}

	var uri string
	var varBytes []byte

	s, e, c := 0, 0, 0
	ty := 0

	for {
		if c == 0 && b[e] == '#' {
			s = e
			for e < len(b) && b[e] != '\n' {
				e++
			}
			if (e - s) > 2 {
				uri = strings.TrimSpace(string(b[(s + 1):e]))
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
		} else if matchPrefix(b, e, "variables") {
			if c == 0 {
				s = e + len("variables") + 1
			}
			ty = AL_VARS
		} else if b[e] == '{' {
			c++

		} else if b[e] == '}' {
			c--

			if c == 0 {
				if ty == AL_QUERY {
					q := string(b[s:(e + 1)])

					item := &allowItem{
						uri: uri,
						gql: q,
					}

					if len(varBytes) != 0 {
						item.vars = varBytes
					}

					al.list[gqlHash(q, varBytes)] = item
					varBytes = nil

				} else if ty == AL_VARS {
					varBytes = b[s:(e + 1)]
				}
				ty = 0
			}
		}

		e++
		if e >= len(b) {
			break
		}
	}
}

func (al *allowList) save(item *allowItem) {
	al.list[gqlHash(item.gql, item.vars)] = item

	f, err := os.Create(al.filepath)
	if err != nil {
		logger.Warn().Err(err).Msg("Failed to write allow list to file")
		return
	}

	defer f.Close()

	keys := []string{}
	urlMap := make(map[string][]*allowItem)

	for _, v := range al.list {
		urlMap[v.uri] = append(urlMap[v.uri], v)
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
			if len(v[i].vars) != 0 {
				vj, err := json.MarshalIndent(v[i].vars, "", "\t")
				if err != nil {
					logger.Warn().Err(err).Msg("Failed to write allow list 'vars' to file")
					continue
				}
				f.WriteString(fmt.Sprintf("variables %s\n\n", vj))
			}

			f.WriteString(fmt.Sprintf("%s\n\n", v[i].gql))
		}
	}
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
