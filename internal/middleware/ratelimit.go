package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*rateBucket
	rate     float64
	capacity float64
}

type rateBucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimiter(rate, capacity int) *RateLimiter {
	return &RateLimiter{
		buckets:  make(map[string]*rateBucket),
		rate:     float64(rate),
		capacity: float64(capacity),
	}
}

func (rl *RateLimiter) Limit() gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := remoteIP(c.Request.RemoteAddr)
		now := time.Now()

		rl.mu.Lock()
		bucket, exists := rl.buckets[clientIP]
		if !exists {
			bucket = &rateBucket{tokens: minFloat(rl.rate, rl.capacity), last: now}
			rl.buckets[clientIP] = bucket
		}
		bucket.tokens = minFloat(rl.capacity, bucket.tokens+now.Sub(bucket.last).Seconds()*rl.rate)
		bucket.last = now
		if bucket.tokens < 1 {
			rl.mu.Unlock()
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}
		bucket.tokens--
		rl.mu.Unlock()

		c.Next()
	}
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

var GlobalRateLimiter = NewRateLimiter(100, 200)
var LoginRateLimiter = NewRateLimiter(1, 5)

func RateLimitMiddleware() gin.HandlerFunc {
	return GlobalRateLimiter.Limit()
}

func LoginRateLimitMiddleware() gin.HandlerFunc {
	return LoginRateLimiter.Limit()
}
