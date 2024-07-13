package core

import (
	lru "github.com/hashicorp/golang-lru"
)

type Cache struct {
	cache *lru.TwoQueueCache
}

// initCache initializes the cache
func (gj *graphjinEngine) initCache() (err error) {
	gj.cache.cache, err = lru.New2Q(500)
	return
}

// Get returns the value from the cache
func (c Cache) Get(key string) (val []byte, fromCache bool) {
	if v, ok := c.cache.Get(key); ok {
		val = v.([]byte)
		fromCache = true
	}
	return
}

// Set sets the value in the cache
func (c Cache) Set(key string, val []byte) {
	c.cache.Add(key, val)
}
