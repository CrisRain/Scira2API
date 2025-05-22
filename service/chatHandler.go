package service

import (
	"bufio"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"scira2api/config"
	"scira2api/log"
	"scira2api/models"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/imroc/req/v3"
)

const BASE_URL = "https://scira.ai/"

type ChatHandler struct {
	Config     *config.Config
	Client     *req.Client
	RestyClient *resty.Client
	index      int64
}

func NewChatHandler(config *config.Config) *ChatHandler {
	client := req.C().ImpersonateChrome().SetTimeout(time.Minute * 5).SetBaseURL(BASE_URL)
	if config.HttpProxy != "" {
		client.SetProxyURL(config.HttpProxy)
	}
	client.SetCommonHeader("Content-Type", "application/json")
	client.SetCommonHeader("Accept", "*/*")
	client.SetCommonHeader("Origin", BASE_URL)
	
	// 初始化 resty 客户端
	restyClient := resty.New().
		SetTimeout(time.Minute * 5).
		SetBaseURL(BASE_URL).
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "*/*").
		SetHeader("Origin", BASE_URL)
	
	// 设置代理（如果有）
	if config.HttpProxy != "" {
		restyClient.SetProxy(config.HttpProxy)
	}
	
	return &ChatHandler{
		Config:     config,
		Client:     client,
		RestyClient: restyClient,
		index:      int64(rand.Intn(len(config.UserIds))),
	}
}

func (h *ChatHandler) ModelGetHandler(c *gin.Context) {
	data := make([]models.OpenAIModelResponse, len(h.Config.Models))
	for _, model := range h.Config.Models {
		model := models.OpenAIModelResponse{
			ID:      model,
			Created: time.Now().Unix(),
			Object:  "model",
		}
		data = append(data, model)
	}

	c.JSON(200, gin.H{
		"object": "list",
		"data":   data,
	})
}

// 处理请求
func (h *ChatHandler) ChatCompletionsHandler(c *gin.Context) {
	var request models.OpenAIChatCompletionsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		log.Error("bind json error: %s", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := h.chatParamCheck(request)
	if err != nil {
		log.Error("chat param check error: %s", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if request.Stream {
		err = h.doChatRequestAsync(c, request)
		if err != nil {
			log.Error("request failed: %s", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	} else {
		resultChan := h.doChatRequestRegular(request)
		result := <-resultChan
		resp, chatId, userId, err := result.Resp, result.ChatId, result.UserId, result.Err
		if err != nil {
			log.Error("request failed after retries: %s. UserId: %s, ChatId: %s", err, userId, chatId)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		h.handleRegularResponse(c, resp, request.Model)
	}
	// if h.Config.ChatDelete {
	// 	go h.deleteChat(chatId, userId)
	// }
}

func (h *ChatHandler) chatParamCheck(request models.OpenAIChatCompletionsRequest) error {
	if request.Model == "" {
		return errors.New("model is required")
	}
	if !slices.Contains(h.Config.Models, request.Model) {
		return errors.New("model is not supported")
	}
	if len(request.Messages) == 0 {
		return errors.New("messages is required")
	}
	return nil
}

func (h *ChatHandler) getUserId() string {
	userIdsLength := int64(len(h.Config.UserIds))
	newIndex := atomic.AddInt64(&h.index, 1)
	return h.Config.UserIds[newIndex%userIdsLength]
}

func (h *ChatHandler) getChatId() string {
	// 生成15字节的随机数据（base64编码后约为20个字符）
	randomBytes := make([]byte, 15)
	crand.Read(randomBytes)
	// 使用base64编码，得到URL安全的字符串
	encoded := base64.RawURLEncoding.EncodeToString(randomBytes)

	// 确保长度为21（包括连字符）
	if len(encoded) < 20 {
		encoded = encoded + strings.Repeat("A", 20-len(encoded))
	} else if len(encoded) > 20 {
		encoded = encoded[:20]
	}

	// 在第11个位置后插入连字符
	return encoded[:11] + "-" + encoded[11:]
}

// chatRequestResult holds the outcome of a chat request operation.
type chatRequestResult struct {
	Resp   *resty.Response
	ChatId string
	UserId string
	Err    error
}

// doChatRequestRegular performs the chat request operation asynchronously for non-streaming requests.
// It sends the result (response, chatId, userId, error) to the returned channel.
func (h *ChatHandler) doChatRequestRegular(request models.OpenAIChatCompletionsRequest) <-chan chatRequestResult {
	resultChan := make(chan chatRequestResult, 1) // Buffered channel to prevent goroutine leak if receiver is not ready

	go func() {
		defer close(resultChan)

		var resp *resty.Response
		var err error
		var chatId string
		var userId string

		// Assuming h.Config.Retry is the total number of attempts.
		// If Retry is 0 or negative, default to 1 attempt.
		attempts := h.Config.Retry
		if attempts <= 0 {
			attempts = 1
		}

		for i := 0; i < attempts; i++ {
			chatId = h.getChatId()
			userId = h.getUserId()
			log.Info("Attempt %d/%d: Request use userId: %s, generate chatId: %s", i+1, attempts, userId, chatId)

			sciraRequest := request.ToSciraChatCompletionsRequest(request.Model, chatId, userId)
			currentResp, currentErr := h.RestyClient.R().
				SetHeader("Referer", BASE_URL).
				SetBody(sciraRequest).
				Post("/api/search")
			
			// log.Info("sciraRequest: %v", sciraRequest)

			if currentErr == nil {
				resp = currentResp
				err = nil
				log.Info("Attempt %d/%d successful. UserId: %s, ChatId: %s", i+1, attempts, userId, chatId)
				break
			}

			err = currentErr
			log.Error("Attempt %d/%d failed. UserId: %s, ChatId: %s, Error: %s", i+1, attempts, userId, chatId, err)

			if i == attempts-1 {
				log.Error("All %d attempts failed for chat request. Last error: %s", attempts, err)
			} else {
				time.Sleep(500 * time.Millisecond)
			}

		}
		resultChan <- chatRequestResult{Resp: resp, ChatId: chatId, UserId: userId, Err: err}
	}()
	return resultChan
}

// doChatRequestAsync performs the chat request operation asynchronously for streaming requests.
// It processes each line of the response as it arrives and directly sends it to the client.
func (h *ChatHandler) doChatRequestAsync(c *gin.Context, request models.OpenAIChatCompletionsRequest) error {
	var chatId string
	var userId string

	// 设置响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		log.Error("Streaming unsupported: ResponseWriter does not implement http.Flusher")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported by the server"})
		return errors.New("streaming unsupported by the server")
	}

	// 不需要初始化响应对象，因为我们直接发送原始数据

	// 添加心跳机制
	clientDone := c.Request.Context().Done()
	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()
	
	// 启动心跳协程
	go func() {
		for {
			select {
			case <-heartbeatTicker.C:
				select {
				case <-clientDone:
					return // 客户端已断开，停止心跳
				default:
					// 发送注释行作为心跳，保持连接活跃
					_, err := fmt.Fprint(c.Writer, ": heartbeat\n\n")
					if err != nil {
						log.Error("Error sending heartbeat: %v", err)
						return
					}
					flusher.Flush()
				}
			case <-clientDone:
				return // 客户端已断开，停止心跳
			}
		}
	}()

	// Assuming h.Config.Retry is the total number of attempts.
	// If Retry is 0 or negative, default to 1 attempt.
	attempts := h.Config.Retry
	if attempts <= 0 {
		attempts = 1
	}

	var err error

	for i := 0; i < attempts; i++ {
		chatId = h.getChatId()
		userId = h.getUserId()
		log.Info("Attempt %d/%d: Request use userId: %s, generate chatId: %s", i+1, attempts, userId, chatId)

		sciraRequest := request.ToSciraChatCompletionsRequest(request.Model, chatId, userId)
		
		// 使用 resty 发送请求
		resp, currentErr := h.RestyClient.R().
			SetHeader("Referer", BASE_URL).
			SetBody(sciraRequest).
			SetDoNotParseResponse(true).
			Execute("POST", "/api/search")
		
		if currentErr != nil {
			err = currentErr
			log.Error("Attempt %d/%d failed. UserId: %s, ChatId: %s, Error: %s", i+1, attempts, userId, chatId, err)

			if i == attempts-1 {
				log.Error("All %d attempts failed for chat request. Last error: %s", attempts, err)
				return err
			} else {
				time.Sleep(500 * time.Millisecond)
				continue
			}
		}

		if resp.StatusCode() != http.StatusOK {
			err = fmt.Errorf("HTTP error: %d", resp.StatusCode())
			log.Error("Attempt %d/%d failed. UserId: %s, ChatId: %s, Error: %s", i+1, attempts, userId, chatId, err)

			if i == attempts-1 {
				log.Error("All %d attempts failed for chat request. Last error: %s", attempts, err)
				return err
			} else {
				time.Sleep(500 * time.Millisecond)
				continue
			}
		}

		// 请求成功，开始处理响应
		log.Info("Attempt %d/%d successful. UserId: %s, ChatId: %s", i+1, attempts, userId, chatId)
		
		// 设置扫描器
		scanner := bufio.NewScanner(resp.RawBody())
		defer resp.RawBody().Close()
		
		// 设置更大的缓冲区以处理潜在的大行数据
		buf := make([]byte, 128*1024) // 初始 128KB
		scanner.Buffer(buf, 2*1024*1024) // 最大 2MB
		
		// 生成OpenAI流式响应的ID
		id := fmt.Sprintf("chatcmpl-%s%s", time.Now().Format("20060102150405"), randString(10))
		created := time.Now().Unix()

		// 发送初始消息，包含角色信息
		initialDelta := models.Delta{Role: "assistant", Content: "", ReasoningContent: ""}
		initialChoice := []models.Choice{
			{
				Index:        0,
				Delta:        initialDelta,
				FinishReason: "",
			},
		}
		initialResponse := models.OpenAIChatCompletionsStreamResponse{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   request.Model,
			Choices: initialChoice,
		}
		
		initialJSON, err := json.Marshal(initialResponse)
		if err != nil {
			log.Error("Error marshaling initial response: %v", err)
			return err
		}
		
		_, writeErr := fmt.Fprintf(c.Writer, "data: %s\n\n", initialJSON)
		if writeErr != nil {
			log.Error("Error writing initial response: %v", writeErr)
			return writeErr
		}
		flusher.Flush()

		// 逐行处理响应
		for scanner.Scan() {
			select {
			case <-clientDone:
				log.Info("Client disconnected, stopping stream.")
				return nil // 客户端断开连接，停止处理
			default:
				// 继续处理
			}

			line := scanner.Text()
			trimmedLine := strings.TrimSpace(line)
			if trimmedLine == "" {
				continue // 跳过空行
			}

			// 处理不同类型的数据并转换为OpenAI流式格式
			if strings.HasPrefix(trimmedLine, "g:") || strings.HasPrefix(trimmedLine, "0:") {
				var content string
				var reasoningContent string
				
				if strings.HasPrefix(trimmedLine, "g:") {
					reasoningContent = processContent(trimmedLine[2:])
				} else if strings.HasPrefix(trimmedLine, "0:") {
					content = processContent(trimmedLine[2:])
				}
				
				// 创建OpenAI格式的流式响应
				delta := models.Delta{Content: content, ReasoningContent: reasoningContent}
				choice := []models.Choice{
					{
						Index:        0,
						Delta:        delta,
						FinishReason: "",
					},
				}
				
				response := models.OpenAIChatCompletionsStreamResponse{
					ID:      id,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   request.Model,
					Choices: choice,
				}
				
				// 转换为JSON
				jsonData, err := json.Marshal(response)
				if err != nil {
					log.Error("Error marshaling response: %v", err)
					continue
				}
				
				// 发送给客户端
				_, writeErr := fmt.Fprintf(c.Writer, "data: %s\n\n", jsonData)
				if writeErr != nil {
					log.Error("Error writing to stream: %v", writeErr)
					return writeErr
				}
				
				// 立即刷新，确保实时传输
				flusher.Flush()
				
			} else {
				// 记录其他类型的数据，但不发送
				// logLen := 10
				// if len(trimmedLine) < 10 {
				// 	logLen = len(trimmedLine)
				// }
				// log.Info("Skipped data: %s", trimmedLine[:logLen])
			}
		}

		// 检查扫描器错误
		if err := scanner.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				log.Error("Error reading stream from scira source: buffer limit exceeded. Error: %v", err)
			} else {
				log.Error("Error reading stream from scira source: %v", err)
			}
			return err
		}

		// 发送完成消息
		select {
		case <-clientDone:
			log.Info("Client disconnected before completion message could be sent.")
		default:
			// 发送带有完成原因的最终消息
			finalChoice := []models.Choice{
				{
					Index:        0,
					Delta:        models.Delta{},
					FinishReason: "stop",
				},
			}
			
			finalResponse := models.OpenAIChatCompletionsStreamResponse{
				ID:      id,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   request.Model,
				Choices: finalChoice,
			}
			
			finalJSON, err := json.Marshal(finalResponse)
			if err != nil {
				log.Error("Error marshaling final response: %v", err)
				return err
			}
			
			_, writeErr := fmt.Fprintf(c.Writer, "data: %s\n\n", finalJSON)
			if writeErr != nil {
				log.Error("Error writing final response: %v", writeErr)
				return writeErr
			}
			
			// 发送完成标记
			_, writeErr = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
			if writeErr != nil {
				log.Error("Error writing [DONE] to stream: %v", writeErr)
				return writeErr
			} else {
				log.Info("Stream completed. Final message and [DONE] sent to client.")
			}
			flusher.Flush()
		}

		// 请求成功并完成处理，退出重试循环
		return nil
	}

	// 如果所有重试都失败，返回最后一个错误
	return err
}

// 处理常规响应
func (h *ChatHandler) handleRegularResponse(c *gin.Context, resp *resty.Response, model string) {
	c.Header("Content-Type", "application/json")
	c.Header("Access-Control-Allow-Origin", "*")

	scanner := bufio.NewScanner(strings.NewReader(resp.String()))

	var content, reasoningContent string
	usage := models.Usage{}
	finishReason := "stop"

	clientDone := c.Request.Context().Done()
	for scanner.Scan() {
		select {
		case <-clientDone:
			return
		default:
			//do nothing
		}
		line := scanner.Text()
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "0:") {
			// 内容部分
			content += processContent(line[2:])
		} else if strings.HasPrefix(line, "g:") {
			// 推理内容
			reasoningContent += processContent(line[2:])
		} else if strings.HasPrefix(line, "e:") {
			// 完成信息
			var finishData map[string]interface{}
			if err := json.Unmarshal([]byte(line[2:]), &finishData); err == nil {
				if reason, ok := finishData["finishReason"].(string); ok {
					finishReason = reason
				}
			}
		} else if strings.HasPrefix(line, "d:") {
			// 用量信息
			var usageData map[string]interface{}
			if err := json.Unmarshal([]byte(line[2:]), &usageData); err == nil {
				if u, ok := usageData["usage"].(map[string]interface{}); ok {
					if pt, ok := u["promptTokens"].(float64); ok {
						usage.PromptTokens = int(pt)
					}
					if ct, ok := u["completionTokens"].(float64); ok {
						usage.CompletionTokens = int(ct)
					}
					usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
				}
			}
		}
	}

	// 构造OpenAI格式的响应
	id := fmt.Sprintf("chatcmpl-%s%s", time.Now().Format("20060102150405"), randString(10))

	choices := models.NewChoice(content, reasoningContent, finishReason)
	oaiResponse := models.NewOaiStreamResponse(id, time.Now().Unix(), model, choices)
	oaiResponse.Usage = usage

	c.JSON(http.StatusOK, oaiResponse)
}

// 辅助函数：处理内容，移除引号并处理转义
func processContent(s string) string {
	// 移除开头和结尾的引号
	s = strings.TrimPrefix(s, "\"")
	s = strings.TrimSuffix(s, "\"")

	// 处理转义的换行符
	s = strings.ReplaceAll(s, "\\n", "\n")

	return s
}

// 辅助函数：生成随机字符串
func randString(n int) string {
	const letterBytes = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
