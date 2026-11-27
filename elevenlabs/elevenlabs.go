package elevenlabs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/codevideo/codevideo-cli/speech"
)

const (
	// defaultVoiceID speaks when no voice is set.
	defaultVoiceID = "iP95p4xoKVk53GoZ742B"
	// chrisVoiceID is Chris's clone, which uses eleven_turbo_v2 unless a model is set.
	chrisVoiceID = "1RLeGxy9FHYB5ScpFkts"
	// defaultModelID is the model for every other voice unless a model is set.
	defaultModelID = "eleven_multilingual_v2"
)

// Voice is who narrates, and with which ElevenLabs model. An empty ID means
// the default voice, and an empty ModelID the model picked for the voice.
type Voice struct {
	ID      string
	ModelID string
}

// Model is the ElevenLabs model ID the voice speaks with: ModelID when set
// (ELEVEN_LABS_MODEL_ID), otherwise eleven_turbo_v2 for Chris's clone and
// eleven_multilingual_v2 for everyone else, as before models could be chosen.
func (v Voice) Model() string {
	if v.ModelID != "" {
		return v.ModelID
	}
	if v.ID == chrisVoiceID {
		return "eleven_turbo_v2"
	}
	return defaultModelID
}

func (v Voice) id() string {
	if v.ID == "" {
		return defaultVoiceID
	}
	return v.ID
}

var modelGeneration = regexp.MustCompile(`^eleven_v(\d+)`)

// ReadsAudioTags reports whether a model acts out audio tags like [whispers]
// instead of reading them aloud: Eleven v3 and every later generation
// (eleven_v3, eleven_v3_conversational, eleven_v4, eleven_v4_turbo, ...).
func ReadsAudioTags(model string) bool {
	m := modelGeneration.FindStringSubmatch(model)
	if m == nil {
		return false
	}
	generation, err := strconv.Atoi(m[1])
	return err == nil && generation >= 3
}

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

// transformOrder applies the longest patterns first, so "console.log" wins over
// ".log" and ".json" over "json". Ranging over the map gave a different order,
// and so different speech, on every run.
var transformOrder = func() []string {
	keys := make([]string, 0, len(customSpeechTransforms))
	for key := range customSpeechTransforms {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	return keys
}()

// applyCustomTransforms applies all custom text transformations.
func applyCustomTransforms(text string) string {
	for _, key := range transformOrder {
		text = strings.ReplaceAll(text, key, customSpeechTransforms[key])
	}
	return text
}

// request is exactly what ElevenLabs is asked for. The same request gives the
// same clip, so its hash names the clip for reuse (ClipKey).
type request struct {
	voiceID string
	body    []byte
}

func newRequest(textToSpeak string, voice Voice) (request, error) {
	modelId := voice.Model()

	// Models before v3 would read audio tags aloud, so they never see them.
	// Text without tags is unchanged, so its request (and ClipKey) is too.
	if !ReadsAudioTags(modelId) {
		textToSpeak = speech.StripTags(textToSpeak)
	}

	// Apply any custom transforms
	textToSpeak = applyCustomTransforms(textToSpeak)

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
		return request{}, fmt.Errorf("error marshalling payload: %w", err)
	}

	return request{voiceID: voice.id(), body: reqBody}, nil
}

// ClipKey names the clip GetAudioArrayBufferElevenLabs would make: a hash of
// the voice, the model, the voice settings and the text after the speech
// transforms. A change to any of them gives a new key, so a reused clip always
// matches what ElevenLabs would return now.
func ClipKey(textToSpeak string, voice Voice) (string, error) {
	r, err := newRequest(textToSpeak, voice)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("elevenlabs-v1\n"+r.voiceID+"\n"), r.body...))
	return hex.EncodeToString(sum[:]), nil
}

// GetAudioArrayBufferElevenLabs sends a POST request to ElevenLabs’ TTS API
// and returns the audio data as a byte slice. Cancelling ctx abandons the request.
func GetAudioArrayBufferElevenLabs(ctx context.Context, textToSpeak, ttsApiKey string, voice Voice) ([]byte, error) {
	r, err := newRequest(textToSpeak, voice)
	if err != nil {
		return nil, err
	}

	// Build the ElevenLabs API URL.
	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", r.voiceID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(r.body))
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
