package utils

import (
	"crypto/rand"
	"math/big"
)

// RandomString 生成指定长度的随机字符串
func RandomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			panic("secure random source unavailable: " + err.Error())
		}
		b[i] = letters[index.Int64()]
	}
	return string(b)
}
