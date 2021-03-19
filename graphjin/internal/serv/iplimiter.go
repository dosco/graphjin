package serv

import (
	"net"
	"net/http"
	"time"

	cache "github.com/go-pkgz/expirable-cache"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/time/rate"
)

var ipCache cache.Cache

func init() {
	ipCache, _ = cache.NewCache(cache.MaxKeys(10), cache.TTL(time.Minute*5))
}

func getIPLimiter(ip string, limit float64, bucket int) *rate.Limiter {
	v, exists := ipCache.Get(ip)
	if !exists {
		limiter := rate.NewLimiter(rate.Limit(limit), bucket)
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
			ip, _, err = net.SplitHostPort(r.RemoteAddr)
		}

		if err != nil {
			sc.zlog.Error("Rate limiter (Remote IP)", []zapcore.Field{zap.Error(err)}...)
			return
		}

		if !getIPLimiter(ip, sc.conf.RateLimiter.Rate, sc.conf.RateLimiter.Bucket).Allow() {
			http.Error(w, "429 Too Many Requests", http.StatusTooManyRequests)
			return
		}

		h.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}
