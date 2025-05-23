package manager

import (
	"math/rand"
	"sync/atomic"
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
	userIdsLength := int64(len(um.userIds))
	newIndex := atomic.AddInt64(&um.index, 1)
	return um.userIds[newIndex%userIdsLength]
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
