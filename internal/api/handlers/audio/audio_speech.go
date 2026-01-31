package audio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

// speechRequest represents the OpenAI-compatible speech generation request.
type speechRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice,omitempty"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
}

// Speech handles the /v1/audio/speech endpoint.
// It proxies JSON requests to the appropriate OpenAI-compatible TTS backend
// based on the model prefix (e.g., "thorsten/thorsten-tts" routes to the Thorsten-TTS provider).
func (h *AudioHandler) Speech(c *gin.Context) {
	var req speechRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Errorf("Failed to parse speech request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("Failed to parse request body: %v", err),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	if req.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "Missing required parameter: model",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	if req.Input == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "Missing required parameter: input",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// Find the matching provider based on model prefix
	provider, actualModel := h.findProvider(req.Model)
	if provider == nil {
		log.Errorf("No provider found for model: %s", req.Model)
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("No provider found for model: %s", req.Model),
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}

	// Build the target URL
	targetURL := strings.TrimSuffix(provider.BaseURL, "/") + "/audio/speech"

	log.Debugf("Audio speech: routing model %q to %s (actual model: %s)", req.Model, targetURL, actualModel)

	// Replace model with actual model name
	req.Model = actualModel

	// Marshal the request body
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		log.Errorf("Failed to marshal speech request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("Failed to build request: %v", err),
				"type":    "server_error",
			},
		})
		return
	}

	// Create the proxy request
	proxyReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		log.Errorf("Failed to create proxy request: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("Failed to create proxy request: %v", err),
				"type":    "server_error",
			},
		})
		return
	}

	proxyReq.Header.Set("Content-Type", "application/json")

	// Add API key if configured
	if len(provider.APIKeyEntries) > 0 && provider.APIKeyEntries[0].APIKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+provider.APIKeyEntries[0].APIKey)
	}

	// Add custom headers if configured
	for key, value := range provider.Headers {
		proxyReq.Header.Set(key, value)
	}

	// Execute the request
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Errorf("Backend request failed: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("Backend request failed: %v", err),
				"type":    "server_error",
			},
		})
		return
	}
	defer resp.Body.Close()

	log.Debugf("Backend response: status=%d content-type=%s", resp.StatusCode, resp.Header.Get("Content-Type"))

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			c.Header(key, value)
		}
	}

	// Copy response body (audio data)
	c.Status(resp.StatusCode)
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		log.Errorf("Failed to copy response body: %v", err)
	}
}
