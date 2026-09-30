package secure

import (
	"crypto/sha256"
	"crypto/subtle"
)

// EqualString 恒定时间比较两个字符串（先 SHA-256，避免长度不等提前返回）。
func EqualString(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}
