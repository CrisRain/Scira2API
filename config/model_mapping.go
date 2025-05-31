package config

// ModelMapping 定义模型名称映射
var ModelMapping = map[string]string{
	"claude-4-sonnet":                "scira-anthropic",
	"claude-4-sonnet-thinking":       "scira-anthropic-thinking",
	"gpt-4o":                         "scira-4o",
	"o4-mini":                        "scira-o4-mini",
	"grok-3":                         "scira-grok-3",
	"grok-3-mini":                    "scira-default",
	"grok-2-vision":                  "scira-vision",
	"gemini-2.5-flash-preview-05-20": "scira-google",
	"gemini-2.5-pro-preview-05-06":   "scira-google-pro",
}
