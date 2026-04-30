// Package chat_completions enthält den MiniMax Tool Call Post-Processor.
// MiniMax gibt Tool Calls als XML-Text aus statt als echte Anthropic tool_use Content Blocks.
// Dieser Post-Processor erkennt das XML-Format und konvertiert es in OpenAI tool_calls.
package chat_completions

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// MiniMaxToolCallState hält den Zustand für die MiniMax XML Tool Call Erkennung im Streaming
type MiniMaxToolCallState struct {
	Buffer         strings.Builder
	Pending        bool // true wenn wir im XML-Puffer-Modus sind
	NextIndex      int  // nächster Tool Call Index
	NextToolCallID int  // Counter für Tool Call IDs
	HasToolCalls   bool // true wenn mindestens ein Tool Call erkannt wurde
}

var (
	miniMaxToolCallStart = "minimax:tool_call"
	miniMaxToolCallEnd   = "</minimax:tool_call>"
	invokeNameRegex      = regexp.MustCompile(`<invoke\s+name="([^"]+)">`)
	parameterRegex       = regexp.MustCompile(`(?s)<parameter\s+name="([^"]+)">(.*?)</parameter>`)
)

// MiniMaxToolCall repräsentiert einen geparsten MiniMax Tool Call
type MiniMaxToolCall struct {
	Name       string
	Parameters map[string]interface{}
}

// ParseMiniMaxToolCalls parst minimax:tool_call XML-Blöcke und extrahiert Tool Calls
func ParseMiniMaxToolCalls(block string) []MiniMaxToolCall {
	var calls []MiniMaxToolCall

	invokeMatches := invokeNameRegex.FindAllStringSubmatchIndex(block, -1)
	for _, match := range invokeMatches {
		name := block[match[2]:match[3]]

		// Finde das Ende des invoke-Blocks
		invokeEnd := strings.Index(block[match[0]:], "</invoke>")
		if invokeEnd == -1 {
			continue
		}
		invokeBlock := block[match[0] : match[0]+invokeEnd]

		params := make(map[string]interface{})
		paramMatches := parameterRegex.FindAllStringSubmatch(invokeBlock, -1)
		for _, pm := range paramMatches {
			if len(pm) >= 3 {
				value := strings.TrimSpace(pm[2])
				// Versuche JSON zu parsen (für verschachtelte Objekte/Arrays)
				var jsonVal interface{}
				if err := json.Unmarshal([]byte(value), &jsonVal); err == nil {
					params[pm[1]] = jsonVal
				} else {
					params[pm[1]] = value
				}
			}
		}

		calls = append(calls, MiniMaxToolCall{
			Name:       name,
			Parameters: params,
		})
	}

	return calls
}

// findToolCallStart sucht nach dem Beginn eines MiniMax Tool Call Blocks.
// Erkennt sowohl "<minimax:tool_call" als auch "minimax:tool_call" (ohne <).
// Gibt den Index zurück ab dem der Block beginnt (inkl. führendem <), oder -1.
func findToolCallStart(text string) int {
	idx := strings.Index(text, miniMaxToolCallStart)
	if idx == -1 {
		return -1
	}
	// Wenn ein < direkt davor steht, dieses mit einschließen
	if idx > 0 && text[idx-1] == '<' {
		return idx - 1
	}
	return idx
}

// ProcessText prüft Text auf minimax:tool_call XML und puffert bei Bedarf.
// Gibt zurück: (normalerText, toolCalls, istPending)
func (s *MiniMaxToolCallState) ProcessText(text string) (string, []MiniMaxToolCall, bool) {
	if s.Pending {
		s.Buffer.WriteString(text)
		bufStr := s.Buffer.String()

		if strings.Contains(bufStr, miniMaxToolCallEnd) {
			// Vollständiger Block - parsen und zurückgeben
			calls := ParseMiniMaxToolCalls(bufStr)
			if len(calls) > 0 {
				s.HasToolCalls = true
			}
			s.Buffer.Reset()
			s.Pending = false
			return "", calls, false
		}
		// Noch unvollständig - weiter puffern
		return "", nil, true
	}

	// Prüfe ob der Text den Beginn eines Tool Calls enthält
	if idx := findToolCallStart(text); idx != -1 {
		s.Pending = true
		normalText := strings.TrimRight(text[:idx], "\n\r ")
		s.Buffer.Reset()
		s.Buffer.WriteString(text[idx:])

		// Prüfe ob der Block bereits vollständig ist (selten bei Streaming)
		bufStr := s.Buffer.String()
		if strings.Contains(bufStr, miniMaxToolCallEnd) {
			calls := ParseMiniMaxToolCalls(bufStr)
			if len(calls) > 0 {
				s.HasToolCalls = true
			}
			s.Buffer.Reset()
			s.Pending = false
			return normalText, calls, false
		}

		return normalText, nil, true
	}

	return text, nil, false
}

// GenerateToolCallID generiert eine eindeutige Tool Call ID
func (s *MiniMaxToolCallState) GenerateToolCallID() string {
	s.NextToolCallID++
	return fmt.Sprintf("toolu_mm_%06d", s.NextToolCallID)
}

// ToolCallsToJSON konvertiert die Parameter eines Tool Calls in einen JSON-String
func ToolCallsToJSON(params map[string]interface{}) string {
	data, err := json.Marshal(params)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// ParseMiniMaxToolCallsFromText extrahiert alle minimax:tool_call Blöcke aus einem Text (für Non-Streaming)
func ParseMiniMaxToolCallsFromText(text string) (cleanText string, calls []MiniMaxToolCall) {
	remaining := text
	for {
		startIdx := findToolCallStart(remaining)
		if startIdx == -1 {
			cleanText += remaining
			break
		}

		// Text vor dem Block übernehmen
		cleanText += strings.TrimRight(remaining[:startIdx], "\n\r ")

		endIdx := strings.Index(remaining[startIdx:], miniMaxToolCallEnd)
		if endIdx == -1 {
			// Unvollständiger Block - als Text belassen
			cleanText += remaining[startIdx:]
			break
		}

		block := remaining[startIdx : startIdx+endIdx+len(miniMaxToolCallEnd)]
		parsed := ParseMiniMaxToolCalls(block)
		calls = append(calls, parsed...)

		remaining = remaining[startIdx+endIdx+len(miniMaxToolCallEnd):]
	}

	cleanText = strings.TrimSpace(cleanText)
	return
}
