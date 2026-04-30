package auth

import (
	"strings"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
)

type modelAliasEntry interface {
	GetName() string
	GetAlias() string
}

type oauthModelAliasTable struct {
	// reverse maps channel -> alias (lower) -> original upstream model name.
	reverse map[string]map[string]string
	// fallbacks maps channel -> alias (lower) -> ordered list of fallback alias names.
	fallbacks map[string]map[string][]string
	// upstreamToProviders maps upstream model name (lower) -> list of channels (providers).
	upstreamToProviders map[string][]string
}

func compileOAuthModelAliasTable(aliases map[string][]internalconfig.OAuthModelAlias) *oauthModelAliasTable {
	if len(aliases) == 0 {
		return &oauthModelAliasTable{}
	}
	out := &oauthModelAliasTable{
		reverse:             make(map[string]map[string]string, len(aliases)),
		fallbacks:           make(map[string]map[string][]string),
		upstreamToProviders: make(map[string][]string),
	}
	for rawChannel, entries := range aliases {
		channel := strings.ToLower(strings.TrimSpace(rawChannel))
		if channel == "" || len(entries) == 0 {
			continue
		}
		rev := make(map[string]string, len(entries))
		fb := make(map[string][]string)
		for _, entry := range entries {
			name := strings.TrimSpace(entry.Name)
			alias := strings.TrimSpace(entry.Alias)
			if name == "" {
				continue
			}

			// Upstream-Name -> Provider (Channel) indexieren.
			// Erlaubt das Auflösen von Providern für Upstream-Modellnamen,
			// auch wenn sie nicht direkt in der Registry stehen.
			upstreamKey := strings.ToLower(name)
			if !sliceContains(out.upstreamToProviders[upstreamKey], channel) {
				out.upstreamToProviders[upstreamKey] = append(out.upstreamToProviders[upstreamKey], channel)
			}

			if alias == "" {
				continue
			}
			if strings.EqualFold(name, alias) {
				continue
			}
			aliasKey := strings.ToLower(alias)
			if _, exists := rev[aliasKey]; exists {
				continue
			}
			rev[aliasKey] = name
			if len(entry.Fallback) > 0 {
				cleaned := make([]string, 0, len(entry.Fallback))
				for _, f := range entry.Fallback {
					f = strings.TrimSpace(f)
					if f != "" {
						cleaned = append(cleaned, f)
					}
				}
				if len(cleaned) > 0 {
					fb[aliasKey] = cleaned
				}
			}
		}
		if len(rev) > 0 {
			out.reverse[channel] = rev
		}
		if len(fb) > 0 {
			out.fallbacks[channel] = fb
		}
	}
	if len(out.reverse) == 0 {
		out.reverse = nil
	}
	if len(out.fallbacks) == 0 {
		out.fallbacks = nil
	}
	if len(out.upstreamToProviders) == 0 {
		out.upstreamToProviders = nil
	}
	return out
}

// SetOAuthModelAlias updates the OAuth model name alias table used during execution.
// The alias is applied per-auth channel to resolve the upstream model name while keeping the
// client-visible model name unchanged for translation/response formatting.
func (m *Manager) SetOAuthModelAlias(aliases map[string][]internalconfig.OAuthModelAlias) {
	if m == nil {
		return
	}
	table := compileOAuthModelAliasTable(aliases)
	// atomic.Value requires non-nil store values.
	if table == nil {
		table = &oauthModelAliasTable{}
	}
	m.oauthModelAlias.Store(table)
}

// ResolveGlobalAlias löst einen Modell-Alias kanalübergreifend auf.
// Wird verwendet, um frei wählbare Aliase (z.B. "default", "smart") vor dem
// Provider-Routing aufzulösen, wenn das Modell nicht direkt in der Registry steht.
// Gibt den Upstream-Modellnamen und alle Channels zurück, in denen der Alias definiert ist.
func (m *Manager) ResolveGlobalAlias(requestedModel string) (upstreamModel string, channels []string) {
	if m == nil {
		return "", nil
	}
	raw := m.oauthModelAlias.Load()
	table, _ := raw.(*oauthModelAliasTable)
	if table == nil || table.reverse == nil {
		return "", nil
	}
	key := strings.ToLower(strings.TrimSpace(requestedModel))
	if key == "" {
		return "", nil
	}
	for channel, rev := range table.reverse {
		if original, ok := rev[key]; ok && original != "" {
			if upstreamModel == "" {
				upstreamModel = original
			}
			channels = append(channels, channel)
		}
	}
	return upstreamModel, channels
}

// GetProvidersForUpstreamModel gibt die Provider (Channels) zurück, die ein
// bestimmtes Upstream-Modell in der oauth-model-alias Config konfiguriert haben.
// Damit können Provider für Modellnamen gefunden werden, die nicht direkt in
// der globalen Registry registriert sind (z.B. claude-opus-4-5 -> antigravity).
func (m *Manager) GetProvidersForUpstreamModel(model string) []string {
	if m == nil {
		return nil
	}
	raw := m.oauthModelAlias.Load()
	table, _ := raw.(*oauthModelAliasTable)
	if table == nil || table.upstreamToProviders == nil {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(model))
	if key == "" {
		return nil
	}
	if providers, ok := table.upstreamToProviders[key]; ok && len(providers) > 0 {
		out := make([]string, len(providers))
		copy(out, providers)
		return out
	}
	return nil
}

// GetModelFallbacks gibt die konfigurierten Fallback-Aliase für einen
// gegebenen Modell-Alias zurück. Sucht kanalübergreifend und gibt die
// erste passende Fallback-Chain zurück. Nil wenn keine Fallbacks konfiguriert sind.
func (m *Manager) GetModelFallbacks(model string) []string {
	if m == nil {
		return nil
	}
	raw := m.oauthModelAlias.Load()
	table, _ := raw.(*oauthModelAliasTable)
	if table == nil || table.fallbacks == nil {
		return nil
	}
	key := strings.ToLower(strings.TrimSpace(model))
	if key == "" {
		return nil
	}
	for _, channelFB := range table.fallbacks {
		if fb, ok := channelFB[key]; ok && len(fb) > 0 {
			return fb
		}
	}
	return nil
}

func sliceContains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// applyOAuthModelAlias resolves the upstream model from OAuth model alias.
// If an alias exists, the returned model is the upstream model.
func (m *Manager) applyOAuthModelAlias(auth *Auth, requestedModel string) string {
	upstreamModel := m.resolveOAuthUpstreamModel(auth, requestedModel)
	if upstreamModel == "" {
		return requestedModel
	}
	return upstreamModel
}

func modelAliasLookupCandidates(requestedModel string) (thinking.SuffixResult, []string) {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return thinking.SuffixResult{}, nil
	}
	requestResult := thinking.ParseSuffix(requestedModel)
	base := requestResult.ModelName
	if base == "" {
		base = requestedModel
	}
	candidates := []string{base}
	if base != requestedModel {
		candidates = append(candidates, requestedModel)
	}
	return requestResult, candidates
}

func preserveResolvedModelSuffix(resolved string, requestResult thinking.SuffixResult) string {
	resolved = strings.TrimSpace(resolved)
	if resolved == "" {
		return ""
	}
	if thinking.ParseSuffix(resolved).HasSuffix {
		return resolved
	}
	if requestResult.HasSuffix && requestResult.RawSuffix != "" {
		return resolved + "(" + requestResult.RawSuffix + ")"
	}
	return resolved
}

func resolveModelAliasPoolFromConfigModels(requestedModel string, models []modelAliasEntry) []string {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return nil
	}
	if len(models) == 0 {
		return nil
	}

	requestResult, candidates := modelAliasLookupCandidates(requestedModel)
	if len(candidates) == 0 {
		return nil
	}

	out := make([]string, 0)
	seen := make(map[string]struct{})
	for i := range models {
		name := strings.TrimSpace(models[i].GetName())
		alias := strings.TrimSpace(models[i].GetAlias())
		for _, candidate := range candidates {
			if candidate == "" || alias == "" || !strings.EqualFold(alias, candidate) {
				continue
			}
			resolved := candidate
			if name != "" {
				resolved = name
			}
			resolved = preserveResolvedModelSuffix(resolved, requestResult)
			key := strings.ToLower(strings.TrimSpace(resolved))
			if key == "" {
				break
			}
			if _, exists := seen[key]; exists {
				break
			}
			seen[key] = struct{}{}
			out = append(out, resolved)
			break
		}
	}
	if len(out) > 0 {
		return out
	}

	for i := range models {
		name := strings.TrimSpace(models[i].GetName())
		for _, candidate := range candidates {
			if candidate == "" || name == "" || !strings.EqualFold(name, candidate) {
				continue
			}
			return []string{preserveResolvedModelSuffix(name, requestResult)}
		}
	}
	return nil
}

func resolveModelAliasFromConfigModels(requestedModel string, models []modelAliasEntry) string {
	resolved := resolveModelAliasPoolFromConfigModels(requestedModel, models)
	if len(resolved) > 0 {
		return resolved[0]
	}
	return ""
}

// resolveOAuthUpstreamModel resolves the upstream model name from OAuth model alias.
// If an alias exists, returns the original (upstream) model name that corresponds
// to the requested alias.
//
// If the requested model contains a thinking suffix (e.g., "gemini-2.5-pro(8192)"),
// the suffix is preserved in the returned model name. However, if the alias's
// original name already contains a suffix, the config suffix takes priority.
func (m *Manager) resolveOAuthUpstreamModel(auth *Auth, requestedModel string) string {
	return resolveUpstreamModelFromAliasTable(m, auth, requestedModel, modelAliasChannel(auth))
}

func resolveUpstreamModelFromAliasTable(m *Manager, auth *Auth, requestedModel, channel string) string {
	if m == nil || auth == nil {
		return ""
	}
	if channel == "" {
		return ""
	}

	// Extract thinking suffix from requested model using ParseSuffix
	requestResult := thinking.ParseSuffix(requestedModel)
	baseModel := requestResult.ModelName

	// Candidate keys to match: base model and raw input (handles suffix-parsing edge cases).
	candidates := []string{baseModel}
	if baseModel != requestedModel {
		candidates = append(candidates, requestedModel)
	}

	raw := m.oauthModelAlias.Load()
	table, _ := raw.(*oauthModelAliasTable)
	if table == nil || table.reverse == nil {
		return ""
	}
	rev := table.reverse[channel]
	if rev == nil {
		return ""
	}

	for _, candidate := range candidates {
		key := strings.ToLower(strings.TrimSpace(candidate))
		if key == "" {
			continue
		}
		original := strings.TrimSpace(rev[key])
		if original == "" {
			continue
		}
		if strings.EqualFold(original, baseModel) {
			return ""
		}

		// If config already has suffix, it takes priority.
		if thinking.ParseSuffix(original).HasSuffix {
			return original
		}
		// Preserve user's thinking suffix on the resolved model.
		if requestResult.HasSuffix && requestResult.RawSuffix != "" {
			return original + "(" + requestResult.RawSuffix + ")"
		}
		return original
	}

	return ""
}

// modelAliasChannel extracts the OAuth model alias channel from an Auth object.
// It determines the provider and auth kind from the Auth's attributes and delegates
// to OAuthModelAliasChannel for the actual channel resolution.
func modelAliasChannel(auth *Auth) string {
	if auth == nil {
		return ""
	}
	provider := strings.ToLower(strings.TrimSpace(auth.Provider))
	authKind := ""
	if auth.Attributes != nil {
		authKind = strings.ToLower(strings.TrimSpace(auth.Attributes["auth_kind"]))
	}
	if authKind == "" {
		if kind, _ := auth.AccountInfo(); strings.EqualFold(kind, "api_key") {
			authKind = "apikey"
		}
	}
	return OAuthModelAliasChannel(provider, authKind)
}

// OAuthModelAliasChannel returns the OAuth model alias channel name for a given provider
// and auth kind. Returns empty string if the provider/authKind combination doesn't support
// OAuth model alias (e.g., API key authentication).
//
// Supported channels: gemini-cli, vertex, aistudio, antigravity, claude, codex, kimi.
func OAuthModelAliasChannel(provider, authKind string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	authKind = strings.ToLower(strings.TrimSpace(authKind))
	switch provider {
	case "gemini":
		// gemini provider uses gemini-api-key config, not oauth-model-alias.
		// OAuth-based gemini auth is converted to "gemini-cli" by the synthesizer.
		return ""
	case "vertex":
		if authKind == "apikey" {
			return ""
		}
		return "vertex"
	case "claude":
		if authKind == "apikey" {
			return ""
		}
		return "claude"
	case "codex":
		if authKind == "apikey" {
			return ""
		}
		return "codex"
	case "gemini-cli", "aistudio", "antigravity", "kimi":
		return provider
	default:
		return ""
	}
}
