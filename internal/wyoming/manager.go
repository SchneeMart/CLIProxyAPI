package wyoming

import (
	"context"
	"fmt"
	"sync"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	log "github.com/sirupsen/logrus"
)

// Manager verwaltet alle Wyoming-Server (STT, TTS, Wake Word).
type Manager struct {
	mu      sync.Mutex
	servers []*Server
	running bool
}

// NewManager erstellt einen neuen Wyoming-Manager.
func NewManager() *Manager {
	return &Manager{}
}

// Start startet alle konfigurierten Wyoming-Server.
func (m *Manager) Start(ctx context.Context, cfg *config.WyomingConfig, proxyBaseURL, apiKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("Wyoming-Manager läuft bereits")
	}

	if cfg == nil || !cfg.Enabled {
		log.Info("Wyoming-Server sind deaktiviert")
		return nil
	}

	log.Info("Starte Wyoming-Server...")

	// STT-Server
	if IsSTTEnabled(cfg) {
		model := cfg.STT.Model
		if model == "" {
			model = "whisper/whisper-large-v3-turbo"
		}
		language := cfg.STT.Language
		if language == "" {
			language = "de"
		}

		sttHandler := &STTHandler{
			ProxyBaseURL: proxyBaseURL,
			APIKey:       apiKey,
			Model:        model,
			Language:     language,
		}

		addr := fmt.Sprintf(":%d", GetSTTPort(cfg))
		server := NewServer(addr, sttHandler)
		if err := server.Start(ctx); err != nil {
			return fmt.Errorf("Fehler beim Starten des STT-Servers: %w", err)
		}
		m.servers = append(m.servers, server)
	}

	// TTS-Server
	if IsTTSEnabled(cfg) {
		model := cfg.TTS.Model
		if model == "" {
			model = "thorsten/thorsten-tts"
		}

		ttsHandler := &TTSHandler{
			ProxyBaseURL: proxyBaseURL,
			APIKey:       apiKey,
			Model:        model,
		}

		addr := fmt.Sprintf(":%d", GetTTSPort(cfg))
		server := NewServer(addr, ttsHandler)
		if err := server.Start(ctx); err != nil {
			// Bereits gestartete Server stoppen
			m.stopServersLocked()
			return fmt.Errorf("Fehler beim Starten des TTS-Servers: %w", err)
		}
		m.servers = append(m.servers, server)
	}

	// Wake-Word-Server
	if IsWakeWordEnabled(cfg) {
		threshold := cfg.WakeWord.EnergyThreshold
		if threshold <= 0 {
			threshold = 0.5
		}

		wakeHandler := &WakeWordHandler{
			EnergyThreshold: threshold,
		}

		addr := fmt.Sprintf(":%d", GetWakeWordPort(cfg))
		server := NewServer(addr, wakeHandler)
		if err := server.Start(ctx); err != nil {
			m.stopServersLocked()
			return fmt.Errorf("Fehler beim Starten des Wake-Word-Servers: %w", err)
		}
		m.servers = append(m.servers, server)
	}

	m.running = true
	log.Infof("Wyoming-Server gestartet (%d Server)", len(m.servers))
	return nil
}

// Stop stoppt alle Wyoming-Server.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopServersLocked()
	m.running = false
}

func (m *Manager) stopServersLocked() {
	for _, server := range m.servers {
		if err := server.Stop(); err != nil {
			log.Errorf("Fehler beim Stoppen eines Wyoming-Servers: %v", err)
		}
	}
	m.servers = nil
}

// IsRunning gibt zurück, ob der Manager läuft.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}
