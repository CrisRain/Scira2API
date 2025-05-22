package models

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

// 定义结构体
type MessagePart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Message struct {
	Role    string        `json:"role"`
	Content string        `json:"content"`
	Parts   []MessagePart `json:"parts,omitempty"`
}

func (oai *Message) ToSciraMessage() Message {
	return Message{
		Role:    oai.Role,
		Content: oai.Content,
		Parts: []MessagePart{
			{
				Type: "text",
				Text: oai.Content,
			},
		},
	}
}

type SciraChatCompletionsRequest struct {
	ID            string    `json:"id"`
	Group         string    `json:"group"`
	Messages      []Message `json:"messages"`
	SelectedModel string    `json:"model"`
	TimeZone      string    `json:"timezone"`
	UserId        string    `json:"user_id"`
}

func (oai *OpenAIChatCompletionsRequest) ToSciraChatCompletionsRequest(model string, chatId string, userId string) *SciraChatCompletionsRequest {
	sciraMessages := make([]Message, len(oai.Messages))
	for i, message := range oai.Messages {
		sciraMessages[i] = message.ToSciraMessage()
	}

	return &SciraChatCompletionsRequest{
		ID:            chatId,
		Group:         "chat",
		TimeZone:      "Asia/Shanghai",
		SelectedModel: model,
		UserId:        userId,
		Messages:      sciraMessages,
	}
}

type OpenAIChatCompletionsStreamResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Provider          string   `json:"provider"`
	Model             string   `json:"model"`
	SystemFingerprint string   `json:"system_fingerprint"`
	Created           int64    `json:"created"`
	Choices           []Choice `json:"choices"`
	Usage             Usage    `json:"usage"`
	Stream            bool     `json:"stream,omitempty"`
}

type Choice struct {
	Index               int    `json:"index"`
	Delta               Delta  `json:"delta"`
	FinishReason        string `json:"finish_reason"`
	NaturalFinishReason string `json:"natural_finish_reason"`
	Logprobs            any    `json:"logprobs"`
}

type Delta struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func NewOaiStreamResponse(id string, time int64, model string, choices []Choice) *OpenAIChatCompletionsStreamResponse {
	return &OpenAIChatCompletionsStreamResponse{
		ID:       id,
		Object:   "chat.completion.chunk",
		Provider: "scira",
		Model:    model,
		Created:  time,
		Choices:  choices,
	}
}

func NewChoice(content string, reasoningContent string, finishReason string) []Choice {
	return []Choice{
		{
			Index:        0,
			Delta:        Delta{Role: "assistant", Content: content, ReasoningContent: reasoningContent},
			FinishReason: finishReason,
		},
	}
}
