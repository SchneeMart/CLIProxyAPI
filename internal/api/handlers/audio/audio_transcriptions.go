// Package audio provides HTTP handlers for OpenAI-compatible audio endpoints.
// It implements audio transcription functionality by proxying requests to
// configured OpenAI-compatible backends (e.g., Whisper servers).
package audio

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	log "github.com/sirupsen/logrus"
)

// AudioHandler handles audio-related API endpoints.
type AudioHandler struct {
	cfg *config.Config
}

// NewAudioHandler creates a new audio handler instance.
func NewAudioHandler(cfg *config.Config) *AudioHandler {
	return &AudioHandler{cfg: cfg}
}

// UpdateConfig updates the configuration reference.
func (h *AudioHandler) UpdateConfig(cfg *config.Config) {
	h.cfg = cfg
}

// Transcriptions handles the /v1/audio/transcriptions endpoint.
// It proxies the multipart request to the appropriate OpenAI-compatible backend
// based on the model prefix (e.g., "whisper/model-name" routes to the Whisper provider).
func (h *AudioHandler) Transcriptions(c *gin.Context) {
	// Parse multipart form to extract the model
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil { // 32MB max
		log.Errorf("Failed to parse multipart form: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("Failed to parse multipart form: %v", err),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	model := c.Request.FormValue("model")
	if model == "" {
		log.Error("Missing required parameter: model")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"message": "Missing required parameter: model",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// Find the matching OpenAI-compatibility provider based on model prefix
	provider, actualModel := h.findProvider(model)
	if provider == nil {
		log.Errorf("No provider found for model: %s", model)
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("No provider found for model: %s", model),
				"type":    "invalid_request_error",
				"code":    "model_not_found",
			},
		})
		return
	}

	// Build the target URL
	targetURL := strings.TrimSuffix(provider.BaseURL, "/") + "/audio/transcriptions"

	log.Debugf("Audio transcription: routing model %q to %s (actual model: %s)", model, targetURL, actualModel)

	// Build multipart body synchronously to avoid goroutine issues
	body, contentType, err := h.buildMultipartBody(c.Request.MultipartForm, actualModel)
	if err != nil {
		log.Errorf("Failed to build multipart body: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("Failed to build request: %v", err),
				"type":    "server_error",
			},
		})
		return
	}

	log.Debugf("Built multipart body: %d bytes, content-type: %s", body.Len(), contentType)

	// Create the proxy request
	proxyReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", targetURL, body)
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

	proxyReq.Header.Set("Content-Type", contentType)

	// Add API key if configured
	if len(provider.APIKeyEntries) > 0 && provider.APIKeyEntries[0].APIKey != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+provider.APIKeyEntries[0].APIKey)
	}

	// Add custom headers if configured
	for key, value := range provider.Headers {
		proxyReq.Header.Set(key, value)
	}

	log.Debugf("Sending request to backend: %s", targetURL)

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

	log.Debugf("Backend response: status=%d", resp.StatusCode)

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			c.Header(key, value)
		}
	}

	// Copy response body
	c.Status(resp.StatusCode)
	if _, err := io.Copy(c.Writer, resp.Body); err != nil {
		log.Errorf("Failed to copy response body: %v", err)
	}
}

// buildMultipartBody creates a new multipart body from the parsed form
func (h *AudioHandler) buildMultipartBody(form *multipart.Form, actualModel string) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Write all form fields, replacing the model field
	if form.Value != nil {
		for key, values := range form.Value {
			for _, value := range values {
				if key == "model" {
					// Replace with the actual model name (prefix stripped)
					if err := writer.WriteField(key, actualModel); err != nil {
						return nil, "", fmt.Errorf("failed to write model field: %w", err)
					}
				} else {
					if err := writer.WriteField(key, value); err != nil {
						return nil, "", fmt.Errorf("failed to write field %s: %w", key, err)
					}
				}
			}
		}
	}

	// Write all file fields
	if form.File != nil {
		for key, fileHeaders := range form.File {
			for _, fileHeader := range fileHeaders {
				// Open the uploaded file
				file, err := fileHeader.Open()
				if err != nil {
					return nil, "", fmt.Errorf("failed to open file %s: %w", fileHeader.Filename, err)
				}

				// Create a form file in the new multipart
				part, err := writer.CreateFormFile(key, fileHeader.Filename)
				if err != nil {
					file.Close()
					return nil, "", fmt.Errorf("failed to create form file: %w", err)
				}

				// Copy the file content
				if _, err := io.Copy(part, file); err != nil {
					file.Close()
					return nil, "", fmt.Errorf("failed to copy file content: %w", err)
				}

				file.Close()
			}
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return body, writer.FormDataContentType(), nil
}

// findProvider finds the OpenAI-compatibility provider for the given model.
// It returns the provider config and the actual model name (with prefix stripped).
func (h *AudioHandler) findProvider(model string) (*config.OpenAICompatibility, string) {
	if h.cfg == nil {
		log.Error("AudioHandler: config is nil")
		return nil, ""
	}

	log.Debugf("Finding provider for model: %s (have %d openai-compat providers)", model, len(h.cfg.OpenAICompatibility))

	// Check for prefix match (e.g., "whisper/model-name")
	for i := range h.cfg.OpenAICompatibility {
		provider := &h.cfg.OpenAICompatibility[i]
		prefix := strings.TrimSuffix(provider.Prefix, "/")
		if prefix == "" {
			continue
		}

		log.Debugf("Checking provider %s with prefix %q", provider.Name, prefix)

		// Check if model starts with prefix/
		if strings.HasPrefix(model, prefix+"/") {
			modelPart := strings.TrimPrefix(model, prefix+"/")

			// Check if modelPart is an alias and resolve to actual name
			actualModel := modelPart
			for _, m := range provider.Models {
				if m.Alias != "" && m.Alias == modelPart {
					actualModel = m.Name
					break
				}
				if m.Name == modelPart {
					actualModel = m.Name
					break
				}
			}

			log.Debugf("Found provider %s for model %s (actual: %s)", provider.Name, model, actualModel)
			return provider, actualModel
		}
	}

	// Check for exact model match in any provider
	for i := range h.cfg.OpenAICompatibility {
		provider := &h.cfg.OpenAICompatibility[i]
		for _, m := range provider.Models {
			// Check alias match
			if m.Alias != "" && m.Alias == model {
				log.Debugf("Found provider %s for model alias %s (actual: %s)", provider.Name, model, m.Name)
				return provider, m.Name
			}
			// Check name match
			if m.Name == model {
				log.Debugf("Found provider %s for model name %s", provider.Name, model)
				return provider, m.Name
			}
		}
	}

	return nil, ""
}
