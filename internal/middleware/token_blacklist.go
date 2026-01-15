package middleware

import (
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenBlacklist 令牌黑名单管理

type TokenBlacklist struct {
	mu        sync.Mutex
	blacklist map[string]time.Time
	cleanup   time.Duration
}

// NewTokenBlacklist 创建令牌黑名单实例
func NewTokenBlacklist(cleanup time.Duration) *TokenBlacklist {
	bl := &TokenBlacklist{
		blacklist: make(map[string]time.Time),
		cleanup:   cleanup,
	}

	// 启动定期清理任务
	go bl.startCleanup()

	return bl
}

// AddToken 添加令牌到黑名单
func (bl *TokenBlacklist) AddToken(tokenString string) error {
	// 解析令牌获取过期时间
	claims := &JWTClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// 这里不需要实际验证签名，只需要获取过期时间
		return []byte("dummy"), nil
	})

	if err != nil && !token.Valid {
		return err
	}

	bl.mu.Lock()
	defer bl.mu.Unlock()

	// 添加到黑名单，保存过期时间
	bl.blacklist[tokenString] = claims.ExpiresAt.Time

	return nil
}

// IsTokenBlacklisted 检查令牌是否在黑名单中
func (bl *TokenBlacklist) IsTokenBlacklisted(tokenString string) bool {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	_, exists := bl.blacklist[tokenString]
	return exists
}

// startCleanup 启动定期清理任务
func (bl *TokenBlacklist) startCleanup() {
	ticker := time.NewTicker(bl.cleanup)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			bl.cleanupExpired()
		}
	}
}

// cleanupExpired 清理过期的黑名单令牌
func (bl *TokenBlacklist) cleanupExpired() {
	now := time.Now()

	bl.mu.Lock()
	defer bl.mu.Unlock()

	for token, expiry := range bl.blacklist {
		if now.After(expiry) {
			delete(bl.blacklist, token)
		}
	}
}

// 全局令牌黑名单实例
var GlobalTokenBlacklist = NewTokenBlacklist(1 * time.Hour)
