package wyoming

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// STTHandler verarbeitet Speech-to-Text-Anfragen über das Wyoming-Protokoll.
// Er leitet Audio-Daten an den bestehenden /v1/audio/transcriptions Endpoint weiter.
type STTHandler struct {
	// ProxyBaseURL ist die Basis-URL des CLIProxy (z.B. "http://localhost:8317")
	ProxyBaseURL string
	// APIKey ist der API-Schlüssel für den CLIProxy
	APIKey string
	// Model ist das zu verwendende Whisper-Modell (z.B. "whisper/whisper-large-v3-turbo")
	Model string
	// Language ist die Sprache für die Transkription (z.B. "de")
	Language string
}

func (h *STTHandler) ServiceType() string {
	return "STT"
}

func (h *STTHandler) HandleConnection(ctx context.Context, reader *bufio.Reader, writer io.Writer) error {
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

		LogEvent("STT", event)

		switch event.Type {
		case "describe":
			// Info-Antwort senden
			infoEvent := BuildSTTInfoEvent()
			if err := WriteEvent(writer, infoEvent); err != nil {
				return fmt.Errorf("Fehler beim Senden des Info-Events: %w", err)
			}

		case "transcribe":
			// Transkriptionsanfrage -- jetzt Audio-Chunks sammeln
			language := GetStringField(event.Data, "language")
			if language == "" {
				language = h.Language
			}

			transcript, err := h.handleTranscription(ctx, reader, writer, language)
			if err != nil {
				log.Errorf("Wyoming STT: Fehler bei der Transkription: %v", err)
				// Leere Transkription senden bei Fehler
				transcriptEvent := &Event{
					Type: "transcript",
					Data: map[string]any{
						"text": "",
					},
				}
				if writeErr := WriteEvent(writer, transcriptEvent); writeErr != nil {
					return writeErr
				}
				continue
			}

			// Transkriptions-Ergebnis senden
			transcriptEvent := &Event{
				Type: "transcript",
				Data: map[string]any{
					"text": transcript,
				},
			}
			if err := WriteEvent(writer, transcriptEvent); err != nil {
				return fmt.Errorf("Fehler beim Senden der Transkription: %w", err)
			}
			log.Infof("Wyoming STT: Transkription erfolgreich: %q", transcript)

		default:
			log.Debugf("Wyoming STT: Unbekannter Event-Typ: %s", event.Type)
		}
	}
}

// handleTranscription sammelt Audio-Chunks und sendet sie an den Transkriptions-Endpoint.
func (h *STTHandler) handleTranscription(ctx context.Context, reader *bufio.Reader, writer io.Writer, language string) (string, error) {
	var audioBuffer bytes.Buffer
	var sampleRate, sampleWidth, channels int

	// Audio-Chunks sammeln bis audio-stop kommt
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		event, err := ReadEvent(reader)
		if err != nil {
			return "", fmt.Errorf("Fehler beim Lesen des Audio-Events: %w", err)
		}

		LogEvent("STT-Audio", event)

		switch event.Type {
		case "audio-start":
			sampleRate = GetIntField(event.Data, "rate")
			sampleWidth = GetIntField(event.Data, "width")
			channels = GetIntField(event.Data, "channels")
			log.Debugf("Wyoming STT: Audio-Start rate=%d, width=%d, channels=%d", sampleRate, sampleWidth, channels)

		case "audio-chunk":
			if len(event.Payload) > 0 {
				audioBuffer.Write(event.Payload)
			}
			// Rate/Width/Channels auch aus audio-chunk lesen falls nicht aus audio-start
			if sampleRate == 0 {
				sampleRate = GetIntField(event.Data, "rate")
			}
			if sampleWidth == 0 {
				sampleWidth = GetIntField(event.Data, "width")
			}
			if channels == 0 {
				channels = GetIntField(event.Data, "channels")
			}

		case "audio-stop":
			// Audio vollständig, jetzt transkribieren
			log.Debugf("Wyoming STT: Audio-Stop, %d Bytes gesammelt", audioBuffer.Len())
			if audioBuffer.Len() == 0 {
				return "", nil
			}

			// Standardwerte falls nicht gesetzt
			if sampleRate == 0 {
				sampleRate = 16000
			}
			if sampleWidth == 0 {
				sampleWidth = 2
			}
			if channels == 0 {
				channels = 1
			}

			// WAV-Header erstellen und PCM-Daten an den Transkriptions-Endpoint senden
			wavData := pcmToWav(audioBuffer.Bytes(), sampleRate, sampleWidth, channels)
			return h.sendToTranscriptionEndpoint(ctx, wavData, language)

		default:
			log.Debugf("Wyoming STT: Unerwarteter Event-Typ während Audio-Sammlung: %s", event.Type)
		}
	}
}

// sendToTranscriptionEndpoint sendet die Audio-Daten an den CLIProxy Transkriptions-Endpoint.
func (h *STTHandler) sendToTranscriptionEndpoint(ctx context.Context, wavData []byte, language string) (string, error) {
	// Multipart-Request erstellen
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)

	// Audio-Datei hinzufügen
	part, err := mw.CreateFormFile("file", "audio.wav")
	if err != nil {
		return "", fmt.Errorf("Fehler beim Erstellen des Formular-Felds: %w", err)
	}
	if _, err := part.Write(wavData); err != nil {
		return "", fmt.Errorf("Fehler beim Schreiben der Audio-Daten: %w", err)
	}

	// Modell-Feld hinzufügen
	model := h.Model
	if model == "" {
		model = "whisper/whisper-large-v3-turbo"
	}
	if err := mw.WriteField("model", model); err != nil {
		return "", fmt.Errorf("Fehler beim Schreiben des Modell-Felds: %w", err)
	}

	// Sprache hinzufügen
	if language != "" {
		if err := mw.WriteField("language", language); err != nil {
			return "", fmt.Errorf("Fehler beim Schreiben des Sprach-Felds: %w", err)
		}
	}

	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("Fehler beim Schließen des Multipart-Writers: %w", err)
	}

	// HTTP-Request erstellen
	targetURL := strings.TrimSuffix(h.ProxyBaseURL, "/") + "/v1/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, body)
	if err != nil {
		return "", fmt.Errorf("Fehler beim Erstellen des HTTP-Requests: %w", err)
	}

	req.Header.Set("Content-Type", mw.FormDataContentType())
	if h.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+h.APIKey)
	}

	// Request senden
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP-Request fehlgeschlagen: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("Fehler beim Lesen der Antwort: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Backend-Fehler: Status %d, Body: %s", resp.StatusCode, string(respBody))
	}

	// JSON-Antwort parsen
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		// Falls kein JSON, direkt als Text verwenden
		return strings.TrimSpace(string(respBody)), nil
	}

	return result.Text, nil
}

// pcmToWav erstellt eine WAV-Datei aus rohen PCM-Daten.
func pcmToWav(pcmData []byte, sampleRate, sampleWidth, channels int) []byte {
	dataSize := len(pcmData)
	fileSize := 36 + dataSize

	wav := make([]byte, 44+dataSize)

	// RIFF-Header
	copy(wav[0:4], "RIFF")
	wav[4] = byte(fileSize)
	wav[5] = byte(fileSize >> 8)
	wav[6] = byte(fileSize >> 16)
	wav[7] = byte(fileSize >> 24)
	copy(wav[8:12], "WAVE")

	// fmt-Chunk
	copy(wav[12:16], "fmt ")
	wav[16] = 16 // Chunk-Größe
	wav[17] = 0
	wav[18] = 0
	wav[19] = 0
	wav[20] = 1 // PCM-Format
	wav[21] = 0
	wav[22] = byte(channels)
	wav[23] = byte(channels >> 8)
	wav[24] = byte(sampleRate)
	wav[25] = byte(sampleRate >> 8)
	wav[26] = byte(sampleRate >> 16)
	wav[27] = byte(sampleRate >> 24)
	byteRate := sampleRate * channels * sampleWidth
	wav[28] = byte(byteRate)
	wav[29] = byte(byteRate >> 8)
	wav[30] = byte(byteRate >> 16)
	wav[31] = byte(byteRate >> 24)
	blockAlign := channels * sampleWidth
	wav[32] = byte(blockAlign)
	wav[33] = byte(blockAlign >> 8)
	bitsPerSample := sampleWidth * 8
	wav[34] = byte(bitsPerSample)
	wav[35] = byte(bitsPerSample >> 8)

	// data-Chunk
	copy(wav[36:40], "data")
	wav[40] = byte(dataSize)
	wav[41] = byte(dataSize >> 8)
	wav[42] = byte(dataSize >> 16)
	wav[43] = byte(dataSize >> 24)

	// PCM-Daten kopieren
	copy(wav[44:], pcmData)

	return wav
}
