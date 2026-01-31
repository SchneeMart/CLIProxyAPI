package wyoming

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// TTSHandler verarbeitet Text-to-Speech-Anfragen über das Wyoming-Protokoll.
// Er leitet Synthesize-Anfragen an den bestehenden /v1/audio/speech Endpoint weiter.
type TTSHandler struct {
	// ProxyBaseURL ist die Basis-URL des CLIProxy (z.B. "http://localhost:8317")
	ProxyBaseURL string
	// APIKey ist der API-Schlüssel für den CLIProxy
	APIKey string
	// Model ist das zu verwendende TTS-Modell (z.B. "thorsten/thorsten-tts")
	Model string
	// OutputSampleRate ist die gewünschte Sample-Rate für die Ausgabe (Standard: 22050)
	OutputSampleRate int
	// OutputSampleWidth ist die Byte-Breite pro Sample (Standard: 2 für 16-bit)
	OutputSampleWidth int
	// OutputChannels ist die Anzahl der Audio-Kanäle (Standard: 1 für Mono)
	OutputChannels int
}

func (h *TTSHandler) ServiceType() string {
	return "TTS"
}

func (h *TTSHandler) HandleConnection(ctx context.Context, reader *bufio.Reader, writer io.Writer) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		event, err := ReadEvent(reader)
		if err != nil {
			return err
		}

		LogEvent("TTS", event)

		switch event.Type {
		case "describe":
			// Info-Antwort senden
			infoEvent := BuildTTSInfoEvent()
			if err := WriteEvent(writer, infoEvent); err != nil {
				return fmt.Errorf("Fehler beim Senden des Info-Events: %w", err)
			}

		case "synthesize":
			// TTS-Anfrage verarbeiten
			text := GetStringField(event.Data, "text")
			if text == "" {
				log.Warn("Wyoming TTS: Leerer Text in Synthesize-Anfrage")
				// Leeres Audio senden
				if err := h.sendEmptyAudio(writer); err != nil {
					return err
				}
				continue
			}

			voice := ""
			if voiceData, ok := event.Data["voice"]; ok {
				if voiceMap, ok := voiceData.(map[string]any); ok {
					voice = GetStringField(voiceMap, "name")
				}
			}

			if err := h.handleSynthesize(ctx, writer, text, voice); err != nil {
				log.Errorf("Wyoming TTS: Fehler bei der Synthese: %v", err)
				// Leeres Audio senden bei Fehler
				if sendErr := h.sendEmptyAudio(writer); sendErr != nil {
					return sendErr
				}
			}

		default:
			log.Debugf("Wyoming TTS: Unbekannter Event-Typ: %s", event.Type)
		}
	}
}

// handleSynthesize sendet den Text an den TTS-Endpoint und streamt das Ergebnis als Audio-Chunks.
func (h *TTSHandler) handleSynthesize(ctx context.Context, writer io.Writer, text, voice string) error {
	log.Infof("Wyoming TTS: Synthesize-Anfrage: %q (voice=%s)", text, voice)

	// Standardwerte
	outputRate := h.OutputSampleRate
	if outputRate == 0 {
		outputRate = 22050
	}
	outputWidth := h.OutputSampleWidth
	if outputWidth == 0 {
		outputWidth = 2
	}
	outputChannels := h.OutputChannels
	if outputChannels == 0 {
		outputChannels = 1
	}

	// Audio vom TTS-Backend abrufen
	pcmData, actualRate, actualWidth, actualChannels, err := h.fetchAudioFromTTS(ctx, text, voice)
	if err != nil {
		return err
	}

	if actualRate > 0 {
		outputRate = actualRate
	}
	if actualWidth > 0 {
		outputWidth = actualWidth
	}
	if actualChannels > 0 {
		outputChannels = actualChannels
	}

	// audio-start senden
	audioStart := &Event{
		Type: "audio-start",
		Data: map[string]any{
			"rate":     outputRate,
			"width":    outputWidth,
			"channels": outputChannels,
		},
	}
	if err := WriteEvent(writer, audioStart); err != nil {
		return fmt.Errorf("Fehler beim Senden von audio-start: %w", err)
	}

	// Audio in Chunks senden (je 2048 Samples)
	chunkSamples := 2048
	chunkBytes := chunkSamples * outputWidth * outputChannels

	for offset := 0; offset < len(pcmData); offset += chunkBytes {
		end := offset + chunkBytes
		if end > len(pcmData) {
			end = len(pcmData)
		}

		audioChunk := &Event{
			Type: "audio-chunk",
			Data: map[string]any{
				"rate":     outputRate,
				"width":    outputWidth,
				"channels": outputChannels,
			},
			Payload: pcmData[offset:end],
		}
		if err := WriteEvent(writer, audioChunk); err != nil {
			return fmt.Errorf("Fehler beim Senden des Audio-Chunks: %w", err)
		}
	}

	// audio-stop senden
	audioStop := &Event{
		Type: "audio-stop",
		Data: map[string]any{},
	}
	if err := WriteEvent(writer, audioStop); err != nil {
		return fmt.Errorf("Fehler beim Senden von audio-stop: %w", err)
	}

	log.Infof("Wyoming TTS: Synthese erfolgreich, %d Bytes PCM-Daten gesendet", len(pcmData))
	return nil
}

// fetchAudioFromTTS ruft Audio vom TTS-Backend ab.
// Gibt PCM-Daten, Sample-Rate, Sample-Breite und Kanalanzahl zurück.
func (h *TTSHandler) fetchAudioFromTTS(ctx context.Context, text, voice string) ([]byte, int, int, int, error) {
	model := h.Model
	if model == "" {
		model = "thorsten/thorsten-tts"
	}

	reqBody := map[string]any{
		"model":           model,
		"input":           text,
		"response_format": "wav",
	}
	if voice != "" {
		reqBody["voice"] = voice
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("Fehler beim Erstellen des Request-Body: %w", err)
	}

	targetURL := strings.TrimSuffix(h.ProxyBaseURL, "/") + "/v1/audio/speech"
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("Fehler beim Erstellen des HTTP-Requests: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if h.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.APIKey)
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("HTTP-Request fehlgeschlagen: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, 0, 0, 0, fmt.Errorf("Backend-Fehler: Status %d, Body: %s", resp.StatusCode, string(respBody))
	}

	// Audio-Antwort lesen
	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("Fehler beim Lesen der Audio-Antwort: %w", err)
	}

	// WAV-Header parsen und PCM-Daten extrahieren
	pcmData, rate, width, channels, parseErr := parseWav(audioData)
	if parseErr != nil {
		// Falls kein WAV, versuchen wir es als rohes PCM zu behandeln
		log.Debugf("Wyoming TTS: Konnte WAV nicht parsen (%v), verwende rohe Daten", parseErr)
		return audioData, 0, 0, 0, nil
	}

	return pcmData, rate, width, channels, nil
}

// parseWav extrahiert PCM-Daten und Audio-Parameter aus einer WAV-Datei.
func parseWav(data []byte) (pcmData []byte, sampleRate, sampleWidth, channels int, err error) {
	if len(data) < 44 {
		return nil, 0, 0, 0, fmt.Errorf("Daten zu kurz für WAV-Header")
	}

	// RIFF-Header prüfen
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, 0, 0, fmt.Errorf("Kein gültiges WAV-Format")
	}

	// fmt-Chunk suchen
	offset := 12
	for offset+8 <= len(data) {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))

		if chunkID == "fmt " {
			if offset+8+chunkSize > len(data) || chunkSize < 16 {
				return nil, 0, 0, 0, fmt.Errorf("Ungültiger fmt-Chunk")
			}
			fmtData := data[offset+8 : offset+8+chunkSize]
			channels = int(binary.LittleEndian.Uint16(fmtData[2:4]))
			sampleRate = int(binary.LittleEndian.Uint32(fmtData[4:8]))
			bitsPerSample := int(binary.LittleEndian.Uint16(fmtData[14:16]))
			sampleWidth = bitsPerSample / 8
		} else if chunkID == "data" {
			dataStart := offset + 8
			dataEnd := dataStart + chunkSize
			if dataEnd > len(data) {
				dataEnd = len(data)
			}
			return data[dataStart:dataEnd], sampleRate, sampleWidth, channels, nil
		}

		offset += 8 + chunkSize
		// Padding auf gerade Adresse
		if chunkSize%2 != 0 {
			offset++
		}
	}

	return nil, 0, 0, 0, fmt.Errorf("Kein data-Chunk gefunden")
}

// sendEmptyAudio sendet eine leere Audio-Sequenz.
func (h *TTSHandler) sendEmptyAudio(writer io.Writer) error {
	rate := h.OutputSampleRate
	if rate == 0 {
		rate = 22050
	}
	width := h.OutputSampleWidth
	if width == 0 {
		width = 2
	}
	channels := h.OutputChannels
	if channels == 0 {
		channels = 1
	}

	audioStart := &Event{
		Type: "audio-start",
		Data: map[string]any{
			"rate":     rate,
			"width":    width,
			"channels": channels,
		},
	}
	if err := WriteEvent(writer, audioStart); err != nil {
		return err
	}

	audioStop := &Event{
		Type: "audio-stop",
		Data: map[string]any{},
	}
	return WriteEvent(writer, audioStop)
}
