package core

import (
	"github.com/dosco/graphjin/core/internal/qcode"
	lru "github.com/hashicorp/golang-lru"
)

type apqInfo struct {
	op    qcode.QType
	name  string
	query string
}

type apqCache struct {
	lruCache    *lru.Cache
	staticCache map[string]apqInfo
}

func (gj *GraphJin) initapqCache() error {
	var err error
	if gj.prod {
		gj.apq.staticCache = make(map[string]apqInfo)
	} else {
		gj.apq.lruCache, err = lru.New(100)
	}
	return err
}

func (c apqCache) Get(key string) (apqInfo, bool) {
	if c.staticCache != nil {
		v, ok := c.staticCache[key]
		return v, ok
	}

	if v, ok := c.lruCache.Get(key); ok {
		return v.(apqInfo), ok
	} else {
		return apqInfo{}, ok
	}
}

func (c apqCache) Set(key string, val apqInfo) {
	if c.staticCache != nil {
		c.staticCache[key] = val
	} else {
		c.lruCache.Add(key, val)
	}
}
