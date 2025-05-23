package manager

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// ChatIdGenerator 聊天ID生成器
type ChatIdGenerator struct {
	prefix string
}

// NewChatIdGenerator 创建新的聊天ID生成器
func NewChatIdGenerator(prefix string) *ChatIdGenerator {
	if prefix == "" {
		prefix = "chat"
	}
	return &ChatIdGenerator{
		prefix: prefix,
	}
}

// GenerateId 生成聊天ID
func (g *ChatIdGenerator) GenerateId() string {
	timestamp := time.Now().Unix()
	randomBytes := make([]byte, 8)
	rand.Read(randomBytes)
	randomStr := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("%s_%d_%s", g.prefix, timestamp, randomStr)
}

// GenerateShortId 生成短聊天ID
func (g *ChatIdGenerator) GenerateShortId() string {
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	randomStr := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("%s_%s", g.prefix, randomStr)
}

// GenerateUUID 生成UUID风格的ID
func (g *ChatIdGenerator) GenerateUUID() string {
	randomBytes := make([]byte, 16)
	rand.Read(randomBytes)

	// 设置版本位和变体位
	randomBytes[6] = (randomBytes[6] & 0x0f) | 0x40 // 版本4
	randomBytes[8] = (randomBytes[8] & 0x3f) | 0x80 // 变体10

	return fmt.Sprintf("%x-%x-%x-%x-%x",
		randomBytes[0:4],
		randomBytes[4:6],
		randomBytes[6:8],
		randomBytes[8:10],
		randomBytes[10:16])
}
