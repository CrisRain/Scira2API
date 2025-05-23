package service

import (
	"math/rand"
	"strings"
)

// processContent 处理内容，移除引号并处理转义
func processContent(s string) string {
	// 移除开头和结尾的引号
	s = strings.TrimPrefix(s, "\"")
	s = strings.TrimSuffix(s, "\"")

	// 处理转义的换行符
	s = strings.ReplaceAll(s, "\\n", "\n")

	return s
}

// randString 生成随机字符串
func randString(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
