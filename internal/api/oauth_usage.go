package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

const (
	anthropicUsageURL   = "https://api.anthropic.com/api/oauth/usage"
	anthropicBetaHeader = "oauth-2025-04-20"
	oauthUsageTimeout   = 10 * time.Second
)

type oauthUsageResponse struct {
	FiveHour       *usageBucket `json:"five_hour"`
	SevenDay       *usageBucket `json:"seven_day"`
	SevenDayOAuth  *usageBucket `json:"seven_day_oauth_apps"`
	SevenDayOpus   *usageBucket `json:"seven_day_opus"`
	SevenDaySonnet *usageBucket `json:"seven_day_sonnet"`
	SevenDayCowork *usageBucket `json:"seven_day_cowork"`
	IguanaNecktie  *usageBucket `json:"iguana_necktie"`
	ExtraUsage     *extraUsage  `json:"extra_usage"`
}

type usageBucket struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    *string `json:"resets_at"`
}

type extraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
}

type accountUsage struct {
	Email string              `json:"email"`
	Usage *oauthUsageResponse `json:"usage,omitempty"`
	Error string              `json:"error,omitempty"`
}

// oauthUsageHandler returns a Gin handler for GET /v1/oauth-usage.
// It reads Claude auth files, queries the Anthropic OAuth usage endpoint,
// and returns the usage data. Protected by normal API key auth.
func (s *Server) oauthUsageHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := s.cfg
		if cfg == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server config unavailable"})
			return
		}

		filterEmail := strings.TrimSpace(c.Query("email"))
		authDir := cfg.AuthDir
		if authDir == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "auth directory not configured"})
			return
		}

		entries, err := os.ReadDir(authDir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to read auth directory: %v", err)})
			return
		}

		var results []accountUsage

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasPrefix(name, "claude-") || !strings.HasSuffix(name, ".json") {
				continue
			}

			fullPath := filepath.Join(authDir, name)
			data, errRead := os.ReadFile(fullPath)
			if errRead != nil {
				log.Warnf("oauth-usage: failed to read %s: %v", name, errRead)
				continue
			}

			fileType := gjson.GetBytes(data, "type").String()
			if fileType != "claude" {
				continue
			}

			email := gjson.GetBytes(data, "email").String()
			accessToken := gjson.GetBytes(data, "access_token").String()

			if filterEmail != "" && !strings.EqualFold(email, filterEmail) {
				continue
			}

			disabled := gjson.GetBytes(data, "disabled").Bool()
			if disabled {
				continue
			}

			if accessToken == "" {
				results = append(results, accountUsage{
					Email: email,
					Error: "no access token available",
				})
				continue
			}

			usage, errFetch := fetchAnthropicOAuthUsage(accessToken)
			if errFetch != nil {
				log.Warnf("oauth-usage: failed to fetch usage for %s: %v", email, errFetch)
				results = append(results, accountUsage{
					Email: email,
					Error: errFetch.Error(),
				})
				continue
			}

			results = append(results, accountUsage{
				Email: email,
				Usage: usage,
			})
		}

		if len(results) == 0 {
			c.JSON(http.StatusOK, gin.H{
				"accounts": []any{},
				"message":  "no claude accounts found",
			})
			return
		}

		if len(results) == 1 && results[0].Usage != nil {
			c.JSON(http.StatusOK, gin.H{
				"accounts": results,
				"email":    results[0].Email,
				"usage":    results[0].Usage,
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"accounts": results,
		})
	}
}

func fetchAnthropicOAuthUsage(accessToken string) (*oauthUsageResponse, error) {
	client := &http.Client{Timeout: oauthUsageTimeout}

	req, err := http.NewRequest("GET", anthropicUsageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-code/2.0.32")
	req.Header.Set("anthropic-beta", anthropicBetaHeader)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var usage oauthUsageResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &usage, nil
}
