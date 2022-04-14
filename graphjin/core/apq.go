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
	cache *lru.TwoQueueCache
}

func (gj *graphjin) initAPQCache() error {
	var err error
	gj.apq.cache, err = lru.New2Q(500)
	return err
}

func (c apqCache) Get(ns, key string) (apqInfo, bool) {
	if v, ok := c.cache.Get((ns + key)); ok {
		return v.(apqInfo), true
	}
	return apqInfo{}, false
}

func (c apqCache) Set(ns, key string, val apqInfo) {
	c.cache.Add((ns + key), val)
}
