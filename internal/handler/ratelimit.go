package handler

import (
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// ipLimiter holds a token-bucket limiter and the last time it was accessed,
// so the cleanup goroutine can evict idle entries.
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter manages per-IP token buckets for a single route tier.
type RateLimiter struct {
	mu         sync.Mutex
	limiters   map[string]*ipLimiter
	r          rate.Limit // tokens per second
	burst      int
	perMinute  int // stored for Retry-After header calculation
	trustProxy bool
	log        *slog.Logger
}

// NewRateLimiter creates a RateLimiter and starts a background cleanup goroutine.
// perMinute is the sustained request rate; burst allows short spikes above that rate.
// Set perMinute to 0 to disable rate limiting entirely.
func NewRateLimiter(perMinute int, burst int, trustProxy bool, log *slog.Logger) *RateLimiter {
	rl := &RateLimiter{
		limiters:   make(map[string]*ipLimiter),
		r:          rate.Limit(float64(perMinute) / 60.0),
		burst:      burst,
		perMinute:  perMinute,
		trustProxy: trustProxy,
		log:        log,
	}
	go rl.cleanup()
	return rl
}

// Limit wraps next and rejects requests that exceed the per-IP rate.
func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	if rl.r == 0 {
		return next // rate limiting disabled
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := rl.clientIP(r)
		limiter := rl.getLimiter(ip)
		if !limiter.Allow() {
			rl.log.WarnContext(r.Context(), "rate limit exceeded",
				"ip", ip,
				"path", r.URL.Path,
				"method", r.Method,
			)
			w.Header().Set("Retry-After", RetryAfterSeconds(rl.perMinute))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// getLimiter returns (or creates) the limiter for the given IP.
func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	entry, ok := rl.limiters[ip]
	if !ok {
		entry = &ipLimiter{limiter: rate.NewLimiter(rl.r, rl.burst)}
		rl.limiters[ip] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

// cleanup evicts limiters that have been idle for more than 10 minutes.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-10 * time.Minute)
		for ip, entry := range rl.limiters {
			if entry.lastSeen.Before(cutoff) {
				delete(rl.limiters, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// clientIP extracts the request originator IP.
// When trustProxy is true it reads X-Forwarded-For; taking the last entry is safer than the first
// because well-behaved reverse proxies append the connecting IP — an attacker can only forge entries
// before the proxy's own append, so the last value is controlled by the trusted proxy.
func (rl *RateLimiter) clientIP(r *http.Request) string {
	if rl.trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Take the last comma-separated value (proxy-appended, not client-controlled).
			last := xff
			if idx := len(xff) - 1; idx >= 0 {
				for i := len(xff) - 1; i >= 0; i-- {
					if xff[i] == ',' {
						last = xff[i+1:]
						break
					}
				}
			}
			// Trim whitespace and validate.
			last = strings.TrimSpace(last)
			if ip := net.ParseIP(last); ip != nil {
				return ip.String()
			}
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// RetryAfterSeconds is a helper used in tests and documentation.
func RetryAfterSeconds(perMinute int) string {
	return strconv.Itoa(60 / max(perMinute, 1))
}
