package wyoming

import (
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// Hilfsfunktionen für die Wyoming-Konfiguration.

// IsSTTEnabled prüft ob der STT-Server aktiviert ist.
func IsSTTEnabled(cfg *config.WyomingConfig) bool {
	if !cfg.Enabled {
		return false
	}
	if cfg.STT.Enabled != nil {
		return *cfg.STT.Enabled
	}
	return true
}

// IsTTSEnabled prüft ob der TTS-Server aktiviert ist.
func IsTTSEnabled(cfg *config.WyomingConfig) bool {
	if !cfg.Enabled {
		return false
	}
	if cfg.TTS.Enabled != nil {
		return *cfg.TTS.Enabled
	}
	return true
}

// IsWakeWordEnabled prüft ob der Wake-Word-Server aktiviert ist.
func IsWakeWordEnabled(cfg *config.WyomingConfig) bool {
	if !cfg.Enabled {
		return false
	}
	if cfg.WakeWord.Enabled != nil {
		return *cfg.WakeWord.Enabled
	}
	return true
}

// GetSTTPort gibt den STT-Port zurück (Standard: 10300).
func GetSTTPort(cfg *config.WyomingConfig) int {
	if cfg.STT.Port > 0 {
		return cfg.STT.Port
	}
	return 10300
}

// GetTTSPort gibt den TTS-Port zurück (Standard: 10200).
func GetTTSPort(cfg *config.WyomingConfig) int {
	if cfg.TTS.Port > 0 {
		return cfg.TTS.Port
	}
	return 10200
}

// GetWakeWordPort gibt den Wake-Word-Port zurück (Standard: 10400).
func GetWakeWordPort(cfg *config.WyomingConfig) int {
	if cfg.WakeWord.Port > 0 {
		return cfg.WakeWord.Port
	}
	return 10400
}
