package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"scira2api/log"
	"scira2api/models"
	"scira2api/pkg/constants"
	// "time" // Removed as it's not used in this file

	"github.com/gin-gonic/gin"
)

// sendErrorFinishSSE sends a final SSE message with a specified error reason and [DONE].
// This is used when the stream terminates due to an error detected after the main scanning loop,
// and we want to inform the client about the error before closing the stream with [DONE].
func (h *ChatHandler) sendErrorFinishSSE(writer gin.ResponseWriter, flusher http.Flusher, responseID string, created int64, model string, errorMsgContent string, counter *TokenCounter) {
	log.Warn("Sending error finish SSE to client. Error: %s", errorMsgContent)

	// 获取当前的token统计
	currentUsage := counter.GetUsage()

	errorResponse := models.OpenAIChatCompletionsStreamResponse{
		ID:      responseID,
		Object:  constants.ObjectChatCompletionChunk,
		Created: created,
		Model:   model, // Use the external model name passed to the function
		Choices: []models.Choice{
			{
				BaseChoice: models.BaseChoice{
					Index:        0,
					FinishReason: "error", // Clearly indicate an error finish reason
				},
				Delta: models.Delta{
					// Provide the error message in the content
					Content: fmt.Sprintf("\n\n[Stream Error: %s]", errorMsgContent),
				},
			},
		},
		Usage: currentUsage, // Include current token usage
	}

	errorJSON, jsonErr := json.Marshal(errorResponse)
	if jsonErr != nil {
		log.Error("Failed to marshal error finish SSE response: %v. Sending plain text fallback.", jsonErr)
		// Fallback to plain text if JSON marshalling fails
		if _, writeErr := fmt.Fprintf(writer, "event: error\ndata: {\"error\": \"Stream processing error\", \"details\": \"%s\"}\n\n", errorMsgContent); writeErr != nil {
			log.Error("Failed to write plain text error finish SSE: %v", writeErr)
		}
	} else {
		if _, writeErr := fmt.Fprintf(writer, "data: %s\n\n", errorJSON); writeErr != nil {
			log.Error("Failed to write JSON error finish SSE: %v", writeErr)
		}
	}

	// Always send [DONE] after an error message to properly close the stream from client's perspective
	if _, writeErr := fmt.Fprint(writer, "data: [DONE]\n\n"); writeErr != nil {
		log.Error("Failed to write [DONE] after error finish SSE: %v", writeErr)
	}

	flusher.Flush()
	log.Info("Error finish SSE and [DONE] message sent to client.")
}