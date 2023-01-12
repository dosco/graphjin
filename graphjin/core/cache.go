package core

import (
	lru "github.com/hashicorp/golang-lru"
)

type Cache struct {
	cache *lru.TwoQueueCache
}

func (gj *graphjin) initCache() (err error) {
	gj.cache.cache, err = lru.New2Q(500)
	return
}

func (c Cache) Get(key string) (val []byte, fromCache bool) {
	if v, ok := c.cache.Get(key); ok {
		val = v.([]byte)
		fromCache = true
	}
	return
}

func (c Cache) Set(key string, val []byte) {
	c.cache.Add(key, val)
}
