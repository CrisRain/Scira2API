package manager

import (
	"math/rand"
)

// UserManager 用户管理器
type UserManager struct{}

// NewUserManager 创建新的用户管理器
func NewUserManager() *UserManager {
	return &UserManager{}
}

// GetNextUserId 获取下一个用户ID（轮询方式）
func (um *UserManager) GetNextUserId() string {
	// 生成符合 user-0cjy35d3tm26 格式的随机字符串
	prefix := "user-"
	const letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	randomPart := string(b)
	return prefix + randomPart
}
