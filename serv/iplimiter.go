package serv

import (
	"net"
	"net/http"
	"strings"
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

func rateLimiter(s1 *Service, h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		var iph, ip string
		var err error
		s := s1.Load().(*service)

		if s.conf.RateLimiter.IPHeader != "" {
			iph = r.Header.Get(s.conf.RateLimiter.IPHeader)
		} else {
			iph = r.Header.Get("X-Forwarded-For")
		}

		if iph != "" {
			v := strings.Split(iph, ",")
			switch n := len(v); {
			case n > 1:
				ip = strings.TrimSpace(v[n-2])
			case n == 1:
				ip = v[0]
			}

		} else {
			ip, _, err = net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				s.zlog.Error("Rate Limiter", []zapcore.Field{zap.Error(err)}...)
				return
			}
		}

		if !getIPLimiter(ip,
			s.conf.RateLimiter.Rate,
			s.conf.RateLimiter.Bucket).Allow() {
			http.Error(w, "429 Too Many Requests", http.StatusTooManyRequests)
			return
		}

		h.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}
