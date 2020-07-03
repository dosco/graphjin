package serv

import (
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// IPLimiter hold rate limiter object and last time seen
type IPLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPLimiters is map hold IP as key and IPLimiter as value
var IPLimiters = make(map[string]*IPLimiter)
var limiterMute sync.Mutex

// Run a background goroutine to remove old entries from the visitors map.
func init() {
	//TODO - Improvement - this goroutine should not start if rate limiter is disable
	go cleanupIPs()
}

func getIPLimiter(ip string) *rate.Limiter {
	limiterMute.Lock()
	defer limiterMute.Unlock()

	v, ok := IPLimiters[ip]
	if !ok {
		limiter := rate.NewLimiter(rate.Limit(conf.RateLimiter.Rate), conf.RateLimiter.Bucket)
		IPLimiters[ip] = &IPLimiter{limiter, time.Now()}
		return limiter
	}
	v.lastSeen = time.Now()
	return v.limiter
}

// Every minute check the map for IP Limiter if new request doesn't come from same ip more than 5 minutes and remove IP from map.
func cleanupIPs() {
	for {
		time.Sleep(time.Minute)

		limiterMute.Lock()
		for ip, v := range IPLimiters {
			if time.Since(v.lastSeen) > 5*time.Minute {
				delete(IPLimiters, ip)
			}
		}
		limiterMute.Unlock()
	}
}

//limit is mideleware function to keep track and implement IP limiter
func limit(w http.ResponseWriter, r *http.Request) error {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		log.Println(err.Error())
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return errors.New("Internal Server Error")
	}

	limiter := getIPLimiter(ip)
	if limiter.Allow() == false {
		http.Error(w, http.StatusText(429), http.StatusTooManyRequests)
		return errors.New("StatusTooManyRequests")
	}
	return nil

}
