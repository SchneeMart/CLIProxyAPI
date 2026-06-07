// model_routing.go - Dynamic Model Routing Endpoints für Claude Code Integration.
// Ermöglicht das Abfragen und Umschalten des aktiven Modells über die oauth-model-alias Config.
package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	log "github.com/sirupsen/logrus"
)

const (
	defaultRoutingChannel = "antigravity"
	defaultRoutingAlias   = "default"
)

// modelStatusHandler gibt den aktuellen Model-Routing-Status zurück.
// GET /v1/model-status
// Query-Parameter:
//   - channel: OAuth-Channel (default: "antigravity")
func (s *Server) modelStatusHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := s.cfg
		if cfg == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server-Konfiguration nicht verfügbar"})
			return
		}

		channel := strings.ToLower(strings.TrimSpace(c.DefaultQuery("channel", defaultRoutingChannel)))

		// Aliase und Fallbacks aus der Config lesen
		aliases := make(map[string]string)
		fallbacks := make(map[string][]string)
		var currentDefault string

		if channelAliases, ok := cfg.OAuthModelAlias[channel]; ok {
			for _, entry := range channelAliases {
				aliases[entry.Alias] = entry.Name
				if strings.EqualFold(entry.Alias, defaultRoutingAlias) {
					currentDefault = entry.Name
				}
				if len(entry.Fallback) > 0 {
					fallbacks[entry.Alias] = entry.Fallback
				}
			}
		}

		// Verfügbare Modelle aus der Registry
		availableModels := getAvailableModelIDs()

		dynamicRouting := gin.H{
			"enabled": len(aliases) > 0,
			"aliases": aliases,
			"channel": channel,
		}
		if len(fallbacks) > 0 {
			dynamicRouting["fallbacks"] = fallbacks
		}

		response := gin.H{
			"success":               true,
			"dynamic_routing":       dynamicRouting,
			"current_default_model": currentDefault,
			"available_models":      availableModels,
		}

		c.JSON(http.StatusOK, response)
	}
}

// switchModelGetHandler gibt das aktuelle Default-Modell zurück.
// GET /v1/switch-model
// Query-Parameter:
//   - alias: Alias-Name (default: "default")
//   - channel: OAuth-Channel (default: "antigravity")
func (s *Server) switchModelGetHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := s.cfg
		if cfg == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server-Konfiguration nicht verfügbar"})
			return
		}

		channel := strings.ToLower(strings.TrimSpace(c.DefaultQuery("channel", defaultRoutingChannel)))
		alias := strings.ToLower(strings.TrimSpace(c.DefaultQuery("alias", defaultRoutingAlias)))
		currentModel := findAliasTarget(cfg, channel, alias)

		c.JSON(http.StatusOK, gin.H{
			"current_model":    currentModel,
			"alias":            alias,
			"channel":          channel,
			"available_models": getAvailableModelIDs(),
		})
	}
}

// switchModelPostHandler wechselt das Modell hinter einem Alias.
// POST /v1/switch-model
// Body: {"model": "claude-opus-4-5", "alias": "default", "channel": "antigravity"}
// alias und channel sind optional (Defaults: "default", "antigravity")
func (s *Server) switchModelPostHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := s.cfg
		if cfg == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Server-Konfiguration nicht verfügbar"})
			return
		}

		var body struct {
			Model   string `json:"model"`
			Alias   string `json:"alias"`
			Channel string `json:"channel"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "Ungültiger Request-Body",
			})
			return
		}

		model := strings.TrimSpace(body.Model)
		if model == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"error":   "model ist erforderlich",
			})
			return
		}

		alias := strings.ToLower(strings.TrimSpace(body.Alias))
		if alias == "" {
			alias = defaultRoutingAlias
		}

		channel := strings.ToLower(strings.TrimSpace(body.Channel))
		if channel == "" {
			channel = defaultRoutingChannel
		}

		// Modell-Validierung: Registry-Modelle + Upstream-Namen aus bestehenden Aliase
		availableModels := collectKnownModels(cfg, channel)
		if len(availableModels) > 0 && !modelInList(model, availableModels) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success":          false,
				"error":            fmt.Sprintf("Unbekanntes Modell: %s", model),
				"available_models": availableModels,
			})
			return
		}

		// Vorheriges Modell merken
		previousModel := findAliasTarget(cfg, channel, alias)

		// Alias in der Config aktualisieren
		if cfg.OAuthModelAlias == nil {
			cfg.OAuthModelAlias = make(map[string][]config.OAuthModelAlias)
		}

		channelAliases := cfg.OAuthModelAlias[channel]
		found := false
		for i, entry := range channelAliases {
			if strings.EqualFold(entry.Alias, alias) {
				channelAliases[i].Name = model
				found = true
				break
			}
		}
		if !found {
			channelAliases = append(channelAliases, config.OAuthModelAlias{
				Name:  model,
				Alias: alias,
			})
		}
		cfg.OAuthModelAlias[channel] = channelAliases

		// Config auf Disk persistieren (File-Watcher triggert Hot-Reload)
		if s.mgmt == nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   "Management-Handler nicht verfügbar",
			})
			return
		}
		if err := s.mgmt.PersistConfig(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"error":   fmt.Sprintf("Config konnte nicht gespeichert werden: %v", err),
			})
			return
		}

		// Alias-Tabelle im AuthManager sofort aktualisieren (ohne auf File-Watcher zu warten)
		if s.handlers != nil && s.handlers.AuthManager != nil {
			s.handlers.AuthManager.SetOAuthModelAlias(cfg.OAuthModelAlias)
		}

		log.Infof("Modell gewechselt: %s -> %s (alias=%s, channel=%s)", previousModel, model, alias, channel)

		c.JSON(http.StatusOK, gin.H{
			"success":        true,
			"previous_model": previousModel,
			"current_model":  model,
			"alias":          alias,
			"channel":        channel,
			"message":        "Modell erfolgreich gewechselt",
		})
	}
}

// findAliasTarget findet das Modell hinter einem Alias in einem Channel.
func findAliasTarget(cfg *config.Config, channel, alias string) string {
	if cfg == nil || cfg.OAuthModelAlias == nil {
		return ""
	}
	channelAliases, ok := cfg.OAuthModelAlias[channel]
	if !ok {
		return ""
	}
	for _, entry := range channelAliases {
		if strings.EqualFold(entry.Alias, alias) {
			return entry.Name
		}
	}
	return ""
}

// getAvailableModelIDs gibt eine deduplizierte Liste der verfügbaren Modell-IDs zurück.
func getAvailableModelIDs() []string {
	reg := registry.GetGlobalRegistry()
	if reg == nil {
		return nil
	}

	// Alle Modelle aus allen Handler-Typen sammeln
	seen := make(map[string]struct{})
	models := make([]string, 0)

	for _, handlerType := range []string{"openai", "claude", "gemini"} {
		rawModels := reg.GetAvailableModels(handlerType)
		for _, m := range rawModels {
			id, _ := m["id"].(string)
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			models = append(models, id)
		}
	}
	return models
}

// collectKnownModels sammelt alle bekannten Modellnamen:
// 1. Modell-IDs aus der Registry (client-sichtbare Namen/Aliase)
// 2. Upstream-Modellnamen aus bestehenden oauth-model-alias Einträgen (name-Feld)
func collectKnownModels(cfg *config.Config, channel string) []string {
	seen := make(map[string]struct{})
	models := make([]string, 0)

	// Registry-Modelle (client-sichtbare IDs)
	for _, id := range getAvailableModelIDs() {
		lower := strings.ToLower(id)
		if _, exists := seen[lower]; !exists {
			seen[lower] = struct{}{}
			models = append(models, id)
		}
	}

	// Upstream-Namen aus allen Channels der oauth-model-alias Config
	if cfg != nil && cfg.OAuthModelAlias != nil {
		for _, channelAliases := range cfg.OAuthModelAlias {
			for _, entry := range channelAliases {
				name := strings.TrimSpace(entry.Name)
				if name == "" {
					continue
				}
				lower := strings.ToLower(name)
				if _, exists := seen[lower]; !exists {
					seen[lower] = struct{}{}
					models = append(models, name)
				}
			}
		}
	}

	return models
}

// modelInList prüft case-insensitive ob ein Modell in der Liste ist.
func modelInList(model string, list []string) bool {
	lower := strings.ToLower(model)
	for _, m := range list {
		if strings.ToLower(m) == lower {
			return true
		}
	}
	return false
}
