package wyoming

// Info-Events für die Dienstbeschreibung (describe/info).

// Attribution beschreibt den Ersteller eines Modells.
type Attribution struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// AsrModel beschreibt ein Speech-to-Text-Modell.
type AsrModel struct {
	Name        string      `json:"name"`
	Languages   []string    `json:"languages"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Description string      `json:"description,omitempty"`
	Version     string      `json:"version,omitempty"`
}

// AsrProgram beschreibt einen STT-Dienst.
type AsrProgram struct {
	Name        string      `json:"name"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Description string      `json:"description,omitempty"`
	Version     string      `json:"version,omitempty"`
	Models      []AsrModel  `json:"models"`
}

// TtsVoiceSpeaker beschreibt einen einzelnen Sprecher.
type TtsVoiceSpeaker struct {
	Name string `json:"name"`
}

// TtsVoice beschreibt eine TTS-Stimme.
type TtsVoice struct {
	Name        string            `json:"name"`
	Languages   []string          `json:"languages"`
	Attribution Attribution       `json:"attribution"`
	Installed   bool              `json:"installed"`
	Description string            `json:"description,omitempty"`
	Version     string            `json:"version,omitempty"`
	Speakers    []TtsVoiceSpeaker `json:"speakers,omitempty"`
}

// TtsProgram beschreibt einen TTS-Dienst.
type TtsProgram struct {
	Name        string      `json:"name"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Description string      `json:"description,omitempty"`
	Version     string      `json:"version,omitempty"`
	Voices      []TtsVoice  `json:"voices"`
}

// WakeModel beschreibt ein Wake-Word-Modell.
type WakeModel struct {
	Name        string      `json:"name"`
	Languages   []string    `json:"languages"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Description string      `json:"description,omitempty"`
	Version     string      `json:"version,omitempty"`
}

// WakeProgram beschreibt einen Wake-Word-Dienst.
type WakeProgram struct {
	Name        string      `json:"name"`
	Attribution Attribution `json:"attribution"`
	Installed   bool        `json:"installed"`
	Description string      `json:"description,omitempty"`
	Version     string      `json:"version,omitempty"`
	Models      []WakeModel `json:"models"`
}

// BuildSTTInfoEvent erstellt das Info-Event für den STT-Server.
func BuildSTTInfoEvent() *Event {
	info := map[string]any{
		"asr": []map[string]any{
			{
				"name": "CLIProxy-Whisper",
				"attribution": map[string]any{
					"name": "CLIProxy",
					"url":  "https://github.com/router-for-me/CLIProxyAPI",
				},
				"installed":   true,
				"description": "Whisper STT über CLIProxy",
				"version":     "1.0.0",
				"models": []map[string]any{
					{
						"name":      "whisper-large-v3-turbo",
						"languages": []string{"de", "en", "fr", "es", "it", "nl", "pt", "pl", "ru", "ja", "zh"},
						"attribution": map[string]any{
							"name": "OpenAI",
							"url":  "https://github.com/openai/whisper",
						},
						"installed":   true,
						"description": "Faster Whisper Large v3 Turbo",
						"version":     "3.0",
					},
				},
			},
		},
		"tts":    []map[string]any{},
		"handle": []map[string]any{},
		"intent": []map[string]any{},
		"wake":   []map[string]any{},
	}

	return &Event{
		Type: "info",
		Data: info,
	}
}

// BuildTTSInfoEvent erstellt das Info-Event für den TTS-Server.
func BuildTTSInfoEvent() *Event {
	info := map[string]any{
		"asr": []map[string]any{},
		"tts": []map[string]any{
			{
				"name": "CLIProxy-Piper",
				"attribution": map[string]any{
					"name": "CLIProxy",
					"url":  "https://github.com/router-for-me/CLIProxyAPI",
				},
				"installed":   true,
				"description": "Piper TTS über CLIProxy (Thorsten)",
				"version":     "1.0.0",
				"voices": []map[string]any{
					{
						"name":      "thorsten",
						"languages": []string{"de"},
						"attribution": map[string]any{
							"name": "Thorsten Müller",
							"url":  "https://github.com/thorstenMueller/Thorsten-Voice",
						},
						"installed":   true,
						"description": "Thorsten - Deutsche Stimme",
						"version":     "1.0",
					},
				},
			},
		},
		"handle": []map[string]any{},
		"intent": []map[string]any{},
		"wake":   []map[string]any{},
	}

	return &Event{
		Type: "info",
		Data: info,
	}
}

// BuildWakeWordInfoEvent erstellt das Info-Event für den Wake-Word-Server.
func BuildWakeWordInfoEvent() *Event {
	info := map[string]any{
		"asr":    []map[string]any{},
		"tts":    []map[string]any{},
		"handle": []map[string]any{},
		"intent": []map[string]any{},
		"wake": []map[string]any{
			{
				"name": "CLIProxy-WakeWord",
				"attribution": map[string]any{
					"name": "CLIProxy",
					"url":  "https://github.com/router-for-me/CLIProxyAPI",
				},
				"installed":   true,
				"description": "openWakeWord-kompatibler Wake-Word-Dienst",
				"version":     "1.0.0",
				"models": []map[string]any{
					{
						"name":      "ok_nabu",
						"languages": []string{"en"},
						"attribution": map[string]any{
							"name": "openWakeWord",
							"url":  "https://github.com/dscripka/openWakeWord",
						},
						"installed":   true,
						"description": "OK Nabu Wake Word",
						"version":     "1.0",
					},
					{
						"name":      "hey_jarvis",
						"languages": []string{"en"},
						"attribution": map[string]any{
							"name": "openWakeWord",
							"url":  "https://github.com/dscripka/openWakeWord",
						},
						"installed":   true,
						"description": "Hey Jarvis Wake Word",
						"version":     "1.0",
					},
					{
						"name":      "alexa",
						"languages": []string{"en"},
						"attribution": map[string]any{
							"name": "openWakeWord",
							"url":  "https://github.com/dscripka/openWakeWord",
						},
						"installed":   true,
						"description": "Alexa Wake Word",
						"version":     "1.0",
					},
				},
			},
		},
	}

	return &Event{
		Type: "info",
		Data: info,
	}
}
