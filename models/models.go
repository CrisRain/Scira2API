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
	UserId        string    `json:"user_id"`
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
		UserId:        userId,
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

type ResponseChoice struct {
	Index               int           `json:"index"`
	Message             ResponseMessage `json:"message"`
	FinishReason        string        `json:"finish_reason"`
	NaturalFinishReason string        `json:"natural_finish_reason,omitempty"`
	Logprobs            any           `json:"logprobs,omitempty"`
}

type ResponseMessage struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type Choice struct {
	Index               int    `json:"index"`
	Delta               Delta  `json:"delta"`
	FinishReason        string `json:"finish_reason"`
	NaturalFinishReason string `json:"natural_finish_reason,omitempty"`
	Logprobs            any    `json:"logprobs"`
}

type Delta struct {
	Role             string `json:"role,omitempty"`
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
	AudioTokens  int `json:"audio_tokens"`
}

type CompletionTokensDetails struct {
	ReasoningTokens         int `json:"reasoning_tokens"`
	AudioTokens             int `json:"audio_tokens"`
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens"`
	RejectedPredictionTokens int `json:"rejected_prediction_tokens"`
}

type Usage struct {
	PromptTokens           int                    `json:"prompt_tokens"`
	PromptTokensDetails    PromptTokensDetails    `json:"prompt_tokens_details"`
	CompletionTokens       int                    `json:"completion_tokens"`
	CompletionTokensDetails CompletionTokensDetails `json:"completion_tokens_details"`
	TotalTokens            int                    `json:"total_tokens"`
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
			Index: 0,
			Delta: Delta{
				Role:             constants.RoleAssistant,
				Content:          content,
				ReasoningContent: reasoningContent,
			},
			FinishReason: finishReason,
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
