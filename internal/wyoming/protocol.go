// Package wyoming implementiert das Wyoming-Protokoll für Home Assistant Voice.
// Es stellt TCP-Server für STT, TTS und Wake Word Detection bereit.
package wyoming

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"

	log "github.com/sirupsen/logrus"
)

// Event repräsentiert ein Wyoming-Protokoll-Event.
type Event struct {
	Type    string         `json:"type"`
	Data    map[string]any `json:"data,omitempty"`
	Payload []byte         `json:"-"`
}

// eventHeader ist das JSON-Header-Format auf der Leitung.
type eventHeader struct {
	Type          string         `json:"type"`
	Data          map[string]any `json:"data,omitempty"`
	DataLength    int            `json:"data_length,omitempty"`
	PayloadLength int            `json:"payload_length,omitempty"`
	Version       string         `json:"version,omitempty"`
}

const protocolVersion = "1.5.2"

// ReadEvent liest ein einzelnes Event aus einem bufio.Reader.
func ReadEvent(reader *bufio.Reader) (*Event, error) {
	// Lese die JSON-Header-Zeile
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}

	var header eventHeader
	if err := json.Unmarshal(line, &header); err != nil {
		return nil, fmt.Errorf("ungültiger Event-Header: %w", err)
	}

	data := header.Data
	if data == nil {
		data = make(map[string]any)
	}

	// Zusätzliche Daten lesen (falls vorhanden)
	if header.DataLength > 0 {
		dataBytes := make([]byte, header.DataLength)
		if _, err := io.ReadFull(reader, dataBytes); err != nil {
			return nil, fmt.Errorf("Fehler beim Lesen der zusätzlichen Daten: %w", err)
		}
		var extraData map[string]any
		if err := json.Unmarshal(dataBytes, &extraData); err != nil {
			return nil, fmt.Errorf("ungültige zusätzliche Daten: %w", err)
		}
		// Zusammenführen
		for k, v := range extraData {
			data[k] = v
		}
	}

	// Payload lesen (typischerweise PCM-Audio)
	var payload []byte
	if header.PayloadLength > 0 {
		payload = make([]byte, header.PayloadLength)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, fmt.Errorf("Fehler beim Lesen des Payloads: %w", err)
		}
	}

	return &Event{
		Type:    header.Type,
		Data:    data,
		Payload: payload,
	}, nil
}

// WriteEvent schreibt ein Event auf einen Writer.
func WriteEvent(writer io.Writer, event *Event) error {
	header := eventHeader{
		Type:    event.Type,
		Version: protocolVersion,
	}

	// Daten als separate Bytes kodieren
	var dataBytes []byte
	if len(event.Data) > 0 {
		var err error
		dataBytes, err = json.Marshal(event.Data)
		if err != nil {
			return fmt.Errorf("Fehler beim Kodieren der Event-Daten: %w", err)
		}
		header.DataLength = len(dataBytes)
	}

	if len(event.Payload) > 0 {
		header.PayloadLength = len(event.Payload)
	}

	// Header-Zeile schreiben
	headerBytes, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("Fehler beim Kodieren des Event-Headers: %w", err)
	}

	if _, err := writer.Write(headerBytes); err != nil {
		return err
	}
	if _, err := writer.Write([]byte("\n")); err != nil {
		return err
	}

	// Zusätzliche Daten schreiben
	if len(dataBytes) > 0 {
		if _, err := writer.Write(dataBytes); err != nil {
			return err
		}
	}

	// Payload schreiben
	if len(event.Payload) > 0 {
		if _, err := writer.Write(event.Payload); err != nil {
			return err
		}
	}

	return nil
}

// WriteEventToConn schreibt ein Event auf eine TCP-Verbindung und flusht den Writer.
func WriteEventToConn(conn net.Conn, event *Event) error {
	return WriteEvent(conn, event)
}

// Hilfsfunktionen für Event-Datenfelder

// GetStringField liest ein String-Feld aus den Event-Daten.
func GetStringField(data map[string]any, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetIntField liest ein Integer-Feld aus den Event-Daten.
func GetIntField(data map[string]any, key string) int {
	if v, ok := data[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return 0
}

// GetStringSlice liest ein String-Slice aus den Event-Daten.
func GetStringSlice(data map[string]any, key string) []string {
	if v, ok := data[key]; ok {
		if arr, ok := v.([]any); ok {
			result := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return nil
}

// LogEvent gibt ein Event zum Debugging aus.
func LogEvent(prefix string, event *Event) {
	payloadLen := len(event.Payload)
	if payloadLen > 0 {
		log.Debugf("[Wyoming %s] Event: type=%s, data=%v, payload=%d bytes", prefix, event.Type, event.Data, payloadLen)
	} else {
		log.Debugf("[Wyoming %s] Event: type=%s, data=%v", prefix, event.Type, event.Data)
	}
}
