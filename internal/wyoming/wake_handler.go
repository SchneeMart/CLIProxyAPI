package wyoming

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"sync"

	log "github.com/sirupsen/logrus"
)

// WakeWordHandler implementiert einen openWakeWord-kompatiblen Wake-Word-Dienst
// über das Wyoming-Protokoll.
//
// Da wir kein echtes Wake-Word-Modell lokal ausführen, arbeitet dieser Handler
// als Platzhalter/Stub, der die korrekte Wyoming-Protokoll-Schnittstelle bereitstellt.
// Er antwortet auf describe-Anfragen und verarbeitet detect-Anfragen.
// Die eigentliche Wake-Word-Erkennung wird simuliert durch Erkennung von
// Audio-Energie über einem konfigurierbaren Schwellwert.
type WakeWordHandler struct {
	// EnergyThreshold ist der Energie-Schwellwert für die Erkennung (0-1).
	// Standardwert ist 0.5 (ziemlich sensitiv).
	EnergyThreshold float64

	// mu schützt activeNames
	mu sync.Mutex
	// activeNames sind die zu erkennenden Wake-Words
	activeNames []string
}

func (h *WakeWordHandler) ServiceType() string {
	return "WakeWord"
}

func (h *WakeWordHandler) HandleConnection(ctx context.Context, reader *bufio.Reader, writer io.Writer) error {
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

		LogEvent("WakeWord", event)

		switch event.Type {
		case "describe":
			// Info-Antwort senden
			infoEvent := BuildWakeWordInfoEvent()
			if err := WriteEvent(writer, infoEvent); err != nil {
				return fmt.Errorf("Fehler beim Senden des Info-Events: %w", err)
			}

		case "detect":
			// Erkennungsanfrage
			names := GetStringSlice(event.Data, "names")
			h.mu.Lock()
			if len(names) > 0 {
				h.activeNames = names
			} else {
				h.activeNames = []string{"ok_nabu"}
			}
			h.mu.Unlock()

			log.Debugf("Wyoming WakeWord: Erkennung gestartet für: %v", h.activeNames)

			// Audio-Stream verarbeiten
			if err := h.handleDetection(ctx, reader, writer); err != nil {
				if err == io.EOF {
					return err
				}
				log.Errorf("Wyoming WakeWord: Fehler bei der Erkennung: %v", err)
			}

		default:
			log.Debugf("Wyoming WakeWord: Unbekannter Event-Typ: %s", event.Type)
		}
	}
}

// handleDetection verarbeitet den Audio-Stream und sucht nach Wake-Words.
func (h *WakeWordHandler) handleDetection(ctx context.Context, reader *bufio.Reader, writer io.Writer) error {
	threshold := h.EnergyThreshold
	if threshold <= 0 {
		threshold = 0.5
	}

	// Zähler für aufeinanderfolgende Frames mit hoher Energie
	var highEnergyFrames int
	const requiredFrames = 5 // Mindestens 5 Frames mit hoher Energie für Erkennung

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

		switch event.Type {
		case "audio-chunk":
			if len(event.Payload) == 0 {
				continue
			}

			// Energie des Audio-Chunks berechnen
			energy := calculateEnergy(event.Payload, GetIntField(event.Data, "width"))

			if energy > threshold {
				highEnergyFrames++
			} else {
				highEnergyFrames = 0
			}

			// Erkennung auslösen wenn genug Frames mit hoher Energie
			if highEnergyFrames >= requiredFrames {
				h.mu.Lock()
				name := "ok_nabu"
				if len(h.activeNames) > 0 {
					name = h.activeNames[0]
				}
				h.mu.Unlock()

				log.Infof("Wyoming WakeWord: Wake-Word erkannt: %s (Energie: %.4f)", name, energy)

				detectionEvent := &Event{
					Type: "detection",
					Data: map[string]any{
						"name":      name,
						"timestamp": GetIntField(event.Data, "timestamp"),
					},
				}
				if err := WriteEvent(writer, detectionEvent); err != nil {
					return fmt.Errorf("Fehler beim Senden der Erkennung: %w", err)
				}

				// Zähler zurücksetzen
				highEnergyFrames = 0
				return nil
			}

		case "audio-start":
			log.Debugf("Wyoming WakeWord: Audio-Start")
			highEnergyFrames = 0

		case "audio-stop":
			log.Debugf("Wyoming WakeWord: Audio-Stop ohne Erkennung")
			// not-detected senden
			notDetectedEvent := &Event{
				Type: "not-detected",
				Data: map[string]any{},
			}
			if err := WriteEvent(writer, notDetectedEvent); err != nil {
				return fmt.Errorf("Fehler beim Senden von not-detected: %w", err)
			}
			return nil

		default:
			log.Debugf("Wyoming WakeWord: Unerwarteter Event-Typ: %s", event.Type)
		}
	}
}

// calculateEnergy berechnet die normalisierte Energie eines PCM-Audio-Chunks.
func calculateEnergy(payload []byte, sampleWidth int) float64 {
	if len(payload) == 0 {
		return 0
	}

	if sampleWidth == 0 {
		sampleWidth = 2 // Standard: 16-bit
	}

	var sumSquares float64
	var count int

	switch sampleWidth {
	case 2: // 16-bit signed
		for i := 0; i+1 < len(payload); i += 2 {
			sample := int16(payload[i]) | int16(payload[i+1])<<8
			normalized := float64(sample) / 32768.0
			sumSquares += normalized * normalized
			count++
		}
	case 1: // 8-bit unsigned
		for i := 0; i < len(payload); i++ {
			sample := int(payload[i]) - 128
			normalized := float64(sample) / 128.0
			sumSquares += normalized * normalized
			count++
		}
	default: // Fallback: 16-bit behandeln
		for i := 0; i+1 < len(payload); i += 2 {
			sample := int16(payload[i]) | int16(payload[i+1])<<8
			normalized := float64(sample) / 32768.0
			sumSquares += normalized * normalized
			count++
		}
	}

	if count == 0 {
		return 0
	}

	rms := math.Sqrt(sumSquares / float64(count))
	return rms
}
