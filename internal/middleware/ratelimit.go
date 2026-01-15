package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// RateLimiter 令牌桶限流器

type RateLimiter struct {
	mu            sync.Mutex
	tokens        map[string]int
	replenishTime map[string]time.Time
	rate          int // 每秒生成的令牌数
	capacity      int // 令牌桶容量
}

// NewRateLimiter 创建限流器实例
func NewRateLimiter(rate, capacity int) *RateLimiter {
	return &RateLimiter{
		tokens:        make(map[string]int),
		replenishTime: make(map[string]time.Time),
		rate:          rate,
		capacity:      capacity,
	}
}

// Limit 限流中间件
func (rl *RateLimiter) Limit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取客户端IP
		clientIP := c.ClientIP()

		rl.mu.Lock()
		defer rl.mu.Unlock()

		now := time.Now()

		// 初始化客户端令牌桶
		if _, exists := rl.tokens[clientIP]; !exists {
			rl.tokens[clientIP] = rl.capacity
			rl.replenishTime[clientIP] = now
			// 允许请求
			rl.tokens[clientIP]--
			c.Next()
			return
		}

		// 计算需要补充的令牌数
		replenishTokens := int(now.Sub(rl.replenishTime[clientIP]).Seconds() * float64(rl.rate))
		if replenishTokens > 0 {
			// 更新令牌数，不超过容量
			if rl.tokens[clientIP]+replenishTokens > rl.capacity {
				rl.tokens[clientIP] = rl.capacity
			} else {
				rl.tokens[clientIP] += replenishTokens
			}
			rl.replenishTime[clientIP] = now
		}

		// 检查令牌是否足够
		if rl.tokens[clientIP] <= 0 {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			c.Abort()
			return
		}

		// 消耗一个令牌
		rl.tokens[clientIP]--

		c.Next()
	}
}

// GlobalRateLimiter 全局限流器实例
var GlobalRateLimiter = NewRateLimiter(100, 200) // 每秒100个请求，桶容量200

// RateLimitMiddleware 默认限流中间件
func RateLimitMiddleware() gin.HandlerFunc {
	return GlobalRateLimiter.Limit()
}
