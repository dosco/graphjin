package core

import (
	"crypto/sha256"
	"encoding/hex"
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

func hashSha256(query string) string {
	sha256Bytes := sha256.Sum256([]byte(query))
	return hex.EncodeToString(sha256Bytes[:])
}
