package core

import (
	lru "github.com/hashicorp/golang-lru"
)

type apqInfo struct {
	query string
}

type apqCache struct {
	cache *lru.TwoQueueCache
}

func (gj *graphjin) initAPQCache() (err error) {
	gj.apq.cache, err = lru.New2Q(500)
	return
}

func (c apqCache) Get(ns, key string) (info apqInfo, fromCache bool) {
	if v, ok := c.cache.Get((ns + key)); ok {
		return v.(apqInfo), true
	}
	return
}

func (c apqCache) Set(ns, key string, val apqInfo) {
	c.cache.Add((ns + key), val)
}
