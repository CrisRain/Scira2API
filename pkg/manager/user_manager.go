package manager

import (
	"math/rand"
	"time"
)

// UserManager 用户管理器
type UserManager struct {
	userIds []string
	index   int64
}

// NewUserManager 创建新的用户管理器
func NewUserManager(userIds []string) *UserManager {
	if len(userIds) == 0 {
		panic("userIds cannot be empty")
	}

	return &UserManager{
		userIds: userIds,
		index:   int64(rand.New(rand.NewSource(time.Now().UnixNano())).Intn(len(userIds))),
	}
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

// GetUserCount 获取用户数量
func (um *UserManager) GetUserCount() int {
	return len(um.userIds)
}

// GetAllUserIds 获取所有用户ID
func (um *UserManager) GetAllUserIds() []string {
	// 返回副本避免外部修改
	result := make([]string, len(um.userIds))
	copy(result, um.userIds)
	return result
}
