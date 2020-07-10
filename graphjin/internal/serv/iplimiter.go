package serv

import (
	"errors"
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

func limit(servConf *ServConfig, w http.ResponseWriter, r *http.Request) error {
	// log.Println("limit")

	// X-Remote-Address is used when super graph configure behind load balancer
	remoteAddr := r.Header.Get("X-Remote-Address")
	// log.Println("X-Remote-Address ", remoteAddr)
	// use request Remote Address if X-Remote-Address not found
	if remoteAddr == "" {
		var err error
		remoteAddr, _, err = net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			servConf.log.Println(err.Error())
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return errors.New("Internal Server Error")
		}
	}

	if !getIPLimiter(remoteAddr).Allow() {
		http.Error(w, "429 Too Many Requests", http.StatusTooManyRequests)
		return errors.New("StatusTooManyRequests")
	}
	return nil
}
