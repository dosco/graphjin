package serv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path"
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
	list     []*allowItem
	index    map[string]int
	filepath string
	saveChan chan *allowItem
	active   bool
}

func initAllowList(cpath string) {
	_allowList = allowList{
		index:    make(map[string]int),
		saveChan: make(chan *allowItem),
		active:   true,
	}

	if len(cpath) != 0 {
		fp := path.Join(cpath, "allow.list")

		if _, err := os.Stat(fp); err == nil {
			_allowList.filepath = fp
		} else if !os.IsNotExist(err) {
			logger.Fatal().Err(err).Send()
		}
	}

	if len(_allowList.filepath) == 0 {
		fp := "./allow.list"

		if _, err := os.Stat(fp); err == nil {
			_allowList.filepath = fp
		} else if !os.IsNotExist(err) {
			logger.Fatal().Err(err).Send()
		}
	}

	if len(_allowList.filepath) == 0 {
		fp := "./config/allow.list"

		if _, err := os.Stat(fp); err == nil {
			_allowList.filepath = fp
		} else if !os.IsNotExist(err) {
			logger.Fatal().Err(err).Send()
		}
	}

	if len(_allowList.filepath) == 0 {
		if conf.Production {
			logger.Fatal().Msg("allow.list not found")
		}

		if len(cpath) == 0 {
			_allowList.filepath = "./config/allow.list"
		} else {
			_allowList.filepath = path.Join(cpath, "allow.list")
		}

		logger.Warn().Msg("allow.list not found")
	} else {
		_allowList.load()
	}

	go func() {
		for v := range _allowList.saveChan {
			_allowList.save(v)
		}
	}()
}

func (al *allowList) add(req *gqlReq) {
	if al.active == false || len(req.ref) == 0 || len(req.Query) == 0 {
		return
	}

	var query string

	for i := 0; i < len(req.Query); i++ {
		c := req.Query[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			query = req.Query
			break

		} else if c == '{' {
			query = "query " + req.Query
			break
		}
	}

	al.saveChan <- &allowItem{
		uri:  req.ref,
		gql:  query,
		vars: req.Vars,
	}
}

func (al *allowList) load() {
	if al.active == false {
		return
	}

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
					key := gqlHash(q, varBytes, "")

					if idx, ok := al.index[key]; !ok {
						al.list = append(al.list, &allowItem{
							uri:  uri,
							gql:  q,
							vars: varBytes,
						})
						al.index[key] = len(al.list) - 1
					} else {
						item := al.list[idx]
						item.gql = q
						item.vars = varBytes
					}

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
	if al.active == false {
		return
	}

	key := gqlHash(item.gql, item.vars, "")

	if _, ok := al.index[key]; ok {
		return
	}

	al.list = append(al.list, item)
	al.index[key] = len(al.list) - 1

	f, err := os.Create(al.filepath)
	if err != nil {
		logger.Warn().Err(err).Msgf("Failed to write allow list: %s", al.filepath)
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
			if len(v[i].vars) != 0 && bytes.Equal(v[i].vars, []byte("{}")) == false {
				vj, err := json.MarshalIndent(v[i].vars, "", "\t")
				if err != nil {
					logger.Warn().Err(err).Msg("Failed to write allow list 'vars' to file")
					continue
				}
				f.WriteString(fmt.Sprintf("variables %s\n\n", vj))
			}

			if v[i].gql[0] == '{' {
				f.WriteString(fmt.Sprintf("query %s\n\n", v[i].gql))
			} else {
				f.WriteString(fmt.Sprintf("%s\n\n", v[i].gql))
			}
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
