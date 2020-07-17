package core

import (
	"crypto/sha1"
	"time"

	cache "github.com/go-pkgz/expirable-cache"
)

var apqCache cache.Cache

func init() {
	apqCache, _ = cache.NewCache(cache.MaxKeys(50), cache.TTL(time.Minute*30))
}

func setAPQ(hash string, value interface{}) {
	apqCache.Set(hash, value, 0)
}

func getAPQ(hash string) (interface{}, bool) {
	value, exists := apqCache.Get(hash)

	return value, exists
}

func hashSha1(query string) (string, error) {
	h := sha1.New()
	_, err := h.Write([]byte(query))
	bs := h.Sum(nil)
	return string(bs), err
}
