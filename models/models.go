package models

import (
	"scira2api/pkg/constants"
	"time"
)

// OpenAI API 相关结构体

type OpenAIModelResponse struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by,omitempty"`
}

// APIError 表示API错误信息
type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

// ErrorContainer 包装错误信息
type ErrorContainer struct {
	Error APIError `json:"error"`
}

type OpenAIChatCompletionsRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// Message 消息结构体
type Message struct {
	Role    string        `json:"role"`
	Content string        `json:"content"`
	Parts   []MessagePart `json:"parts,omitempty"`
}

// MessagePart 消息部分结构体
type MessagePart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ToSciraMessage 转换为Scira格式的消息
func (m *Message) ToSciraMessage() Message {
	return Message{
		Role:    m.Role,
		Content: m.Content,
		Parts: []MessagePart{
			{
				Type: "text",
				Text: m.Content,
			},
		},
	}
}

// Scira API 相关结构体

type SciraChatCompletionsRequest struct {
	ID            string    `json:"id"`
	Group         string    `json:"group"`
	Messages      []Message `json:"messages"`
	SelectedModel string    `json:"model"`
	TimeZone      string    `json:"timezone"`
	UserID        string    `json:"user_id"`
}

// ToSciraChatCompletionsRequest 转换为Scira格式的聊天请求
func (oai *OpenAIChatCompletionsRequest) ToSciraChatCompletionsRequest(model, chatId, userId string) *SciraChatCompletionsRequest {
	sciraMessages := make([]Message, len(oai.Messages))
	for i, message := range oai.Messages {
		sciraMessages[i] = message.ToSciraMessage()
	}

	return &SciraChatCompletionsRequest{
		ID:            chatId,
		Group:         constants.ChatGroup,
		TimeZone:      constants.DefaultTimeZone,
		SelectedModel: model,
		UserID:        userId,
		Messages:      sciraMessages,
	}
}

// OpenAI 流式响应结构体

type OpenAIChatCompletionsStreamResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Provider          string   `json:"provider,omitempty"`
	Model             string   `json:"model"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
	Created           int64    `json:"created"`
	Choices           []Choice `json:"choices"`
	Usage             Usage    `json:"usage,omitempty"`
	Stream            bool     `json:"stream,omitempty"`
}

// OpenAI 非流式响应结构体
type OpenAIChatCompletionsResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Provider          string   `json:"provider,omitempty"`
	Model             string   `json:"model"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
	Created           int64    `json:"created"`
	Choices           []ResponseChoice `json:"choices"`
	Usage             Usage    `json:"usage"`
}

// BaseChoice 基础选择结构体，包含共同字段
type BaseChoice struct {
	Index               int    `json:"index"`
	FinishReason        string `json:"finish_reason"`
	NaturalFinishReason string `json:"natural_finish_reason,omitempty"`
	Logprobs            any    `json:"logprobs,omitempty"`
}

// ResponseChoice 非流式响应的选择结构体
type ResponseChoice struct {
	BaseChoice
	Message ResponseMessage `json:"message"`
}

type ResponseMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// Choice 流式响应的选择结构体
type Choice struct {
	BaseChoice
	Delta Delta `json:"delta"`
}

type Delta struct {
	Role             string `json:"role,omitempty"`
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// Usage 结构体，仅包含核心token统计字段
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// 构建响应的辅助函数

// NewOaiStreamResponse 创建新的OpenAI流式响应
func NewOaiStreamResponse(id string, timestamp int64, model string, choices []Choice) *OpenAIChatCompletionsStreamResponse {
	return &OpenAIChatCompletionsStreamResponse{
		ID:       id,
		Object:   constants.ObjectChatCompletionChunk,
		Provider: constants.ProviderScira,
		Model:    model,
		Created:  timestamp,
		Choices:  choices,
	}
}

// NewChoice 创建新的选择项
func NewChoice(content, reasoningContent, finishReason string) []Choice {
	return []Choice{
		{
			BaseChoice: BaseChoice{
				Index:        0,
				FinishReason: finishReason,
			},
			Delta: Delta{
				Role:             constants.RoleAssistant,
				Content:          content,
				ReasoningContent: reasoningContent,
			},
		},
	}
}

// NewModelResponse 创建模型响应
func NewModelResponse(id string) OpenAIModelResponse {
	return OpenAIModelResponse{
		ID:      id,
		Created: time.Now().Unix(),
		Object:  "model",
		OwnedBy: constants.ProviderScira,
	}
}
