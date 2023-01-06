package core

import (
	lru "github.com/hashicorp/golang-lru"
)

type apqCache struct {
	cache *lru.TwoQueueCache
}

func (gj *graphjin) initAPQCache() (err error) {
	gj.apq.cache, err = lru.New2Q(500)
	return
}

func (c apqCache) Get(key string) (val []byte, fromCache bool) {
	if v, ok := c.cache.Get(key); ok {
		val = v.([]byte)
		fromCache = true
	}
	return
}

func (c apqCache) Set(key string, val []byte) {
	c.cache.Add(key, val)
}
