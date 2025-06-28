package elevenlabs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var customSpeechTransforms = map[string]string{
	"C#":          "C sharp",
	"areEqual":    "are equal",
	".NET":        "dot net",
	"C++":         "C plus plus",
	"_":           " underscore ",
	".sh":         "dot S H",
	".js":         "dot J S",
	".ts":         "dot T S",
	".css":        "dot C S S",
	".html":       "dot H T M L",
	".yaml":       "dot yaml",
	".yml":        "dot yaml",
	".xml":        "dot X M L",
	".md":         "dot M D",
	".txt":        "dot T X T",
	".log":        "dot L O G",
	".csv":        "dot C S V",
	".go":         "dot go",
	".py":         "dot pi",
	"console.log": "console dot log",
	"codevideo":   "code video",
	"CodeVideo":   "code video",
	".json":       "dot jason",
	"JSON":        "jason",
	"json":        "jason",
	"mcp-cli":     "M C P C L I",
	"stdin":       "standard in",
	"stdout":      "standard out",
	"stderr":      "standard error",
}

// applyCustomTransforms applies all custom text transformations.
func applyCustomTransforms(text string) string {
	for key, value := range customSpeechTransforms {
		if strings.Contains(text, key) {
			text = strings.ReplaceAll(text, key, value)
		}
	}
	return text
}

// getAudioArrayBufferElevenLabs sends a POST request to ElevenLabs’ TTS API
// and returns the audio data as a byte slice.
func GetAudioArrayBufferElevenLabs(textToSpeak, ttsApiKey, ttsVoiceId string) ([]byte, error) {
	// Apply any custom transforms
	textToSpeak = applyCustomTransforms(textToSpeak)

	// model ID - if it is my voice (1RLeGxy9FHYB5ScpFkts) use "eleven_turbo_v2"
	modelId := "eleven_multilingual_v2"
	if ttsVoiceId == "1RLeGxy9FHYB5ScpFkts" {
		modelId = "eleven_turbo_v2"
	}

	// Prepare the request payload.
	// It must match the API’s expected JSON structure.
	payload := struct {
		Text          string `json:"text"`
		ModelID       string `json:"model_id"`
		VoiceSettings struct {
			Stability         float64 `json:"stability"`
			SimilarityBoost   float64 `json:"similarity_boost"`
			StyleExaggeration float64 `json:"style_exaggeration"`
		} `json:"voice_settings"`
	}{
		Text: textToSpeak,
		// ModelID: "eleven_turbo_v2",
		ModelID: modelId,
	}
	payload.VoiceSettings.Stability = 0.5
	payload.VoiceSettings.SimilarityBoost = 0.75
	payload.VoiceSettings.StyleExaggeration = 0.5

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("error marshalling payload: %w", err)
	}

	// If no voice id is provided, use the default.
	if ttsVoiceId == "" {
		ttsVoiceId = "iP95p4xoKVk53GoZ742B"
	}

	// Build the ElevenLabs API URL.
	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", ttsVoiceId)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("error creating request: %w", err)
	}

	// Set headers.
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", ttsApiKey)
	req.Header.Set("Accept", "audio/mpeg")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making POST request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP error! Status: %d, Body: %s", resp.StatusCode, string(bodyBytes))
	}

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	return audioData, nil
}
