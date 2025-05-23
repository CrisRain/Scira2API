package service

import (
	"math/rand"
	"strings"
	"strconv"
	// "log" // 可选: 用于调试 Unquote 失败
)

// processContent 处理内容，移除引号并处理转义
func processContent(s string) string {
	// 尝试使用 strconv.Unquote，它能处理标准的 Go 转义。
	// strconv.Unquote 要求字符串是被引号包围的。

	// 情况 1: 字符串 s 已经是双引号包围的。
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if unquoted, err := strconv.Unquote(s); err == nil {
			return unquoted
		}
		// 如果有引号但 Unquote 失败 (例如, "\"abc\\x\""), 则会进入回退逻辑。
		// log.Printf("Debug: strconv.Unquote failed for already quoted string: %s, err: %v", s, err)
	} else {
		// 情况 2: 字符串 s 没有被双引号包围。
		// 尝试添加双引号再 Unquote。
		// 这是为了处理类似 "hello\\nworld" 这样的输入，期望得到 "hello\nworld"。
		quotedS := "\"" + s + "\""
		if unquoted, err := strconv.Unquote(quotedS); err == nil {
			return unquoted
		}
		// 如果添加引号后 Unquote 仍然失败, 则会进入回退逻辑。
		// log.Printf("Debug: strconv.Unquote failed for artificially quoted string: %s (original: %s), err: %v", quotedS, s, err)
	}

	// 回退逻辑: 如果 strconv.Unquote 不适用或失败。
	// 此时的 s 是原始输入字符串。
	processedS := s

	// 1. 移除首尾可能存在的双引号。
	// 使用 TrimPrefix 和 TrimSuffix 更安全，避免索引越界。
	processedS = strings.TrimPrefix(processedS, "\"")
	processedS = strings.TrimSuffix(processedS, "\"")

	// 2. 处理常见的转义字符。
	// 使用 strings.NewReplacer 以正确的顺序处理。
	// \\ 必须首先被替换，以避免错误地处理 \\" 中的 \。
	replacer := strings.NewReplacer(
		"\\\\", "\\", // 处理 \\ -> \
		"\\\"", "\"", // 处理 \" -> "
		"\\n", "\n",   // 处理 \n -> newline
		"\\t", "\t",   // 处理 \t -> tab
		"\\r", "\r",   // 处理 \r -> carriage return
	)
	processedS = replacer.Replace(processedS)

	return processedS
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
