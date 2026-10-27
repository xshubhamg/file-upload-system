package middleware

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/xshubhamg/file-upload/internal/api/response"
	"github.com/xshubhamg/file-upload/internal/apperr"
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPLimiter is a per-client token bucket. Entries are pruned opportunistically
// so a long-running process cannot grow without bound.
type IPLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	rps     rate.Limit
	burst   int
}

func NewIPLimiter(rps float64, burst int) *IPLimiter {
	return &IPLimiter{
		entries: make(map[string]*limiterEntry),
		rps:     rate.Limit(rps),
		burst:   burst,
	}
}

func (l *IPLimiter) Allow(key string) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.entries[key]
	if !ok {
		entry = &limiterEntry{limiter: rate.NewLimiter(l.rps, l.burst)}
		l.entries[key] = entry
	}
	entry.lastSeen = now
	allowed := entry.limiter.Allow()

	if len(l.entries) > 10_000 {
		cutoff := now.Add(-10 * time.Minute)
		for k, e := range l.entries {
			if e.lastSeen.Before(cutoff) {
				delete(l.entries, k)
			}
		}
	}
	return allowed
}

func (l *IPLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !l.Allow(c.ClientIP()) {
			c.Header("Retry-After", "1")
			response.Error(c, apperr.TooManyRequests("too many requests; slow down and retry"))
			return
		}
		c.Next()
	}
}
