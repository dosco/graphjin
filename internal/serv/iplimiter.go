package serv

import (
	"net"
	"net/http"
	"time"

	cache "github.com/go-pkgz/expirable-cache"
	"golang.org/x/time/rate"
)

var ipCache cache.Cache

func init() {
	ipCache, _ = cache.NewCache(cache.MaxKeys(10), cache.TTL(time.Minute*5))
}

func getIPLimiter(ip string) *rate.Limiter {
	v, exists := ipCache.Get(ip)
	if !exists {
		limiter := rate.NewLimiter(1, 3)
		ipCache.Set(ip, limiter, 0)
		return limiter
	}

	return v.(*rate.Limiter)
}

func rateLimiter(sc *ServConfig, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		var ip string
		var err error

		if sc.conf.RateLimiter.IPHeader != "" {
			ip = r.Header.Get(sc.conf.RateLimiter.IPHeader)
		} else {
			ip = r.Header.Get("X-Remote-Address")
		}

		if ip == "" {
			if ip, _, err = net.SplitHostPort(r.RemoteAddr); err != nil {
				sc.log.Fatalf("rate limiter: %s", err)
			}
		}

		if !getIPLimiter(ip).Allow() {
			http.Error(w, "429 Too Many Requests", http.StatusTooManyRequests)
			return
		}

		h.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}
