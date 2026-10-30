package generator

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/codevideo/codevideo-cli/cli/notify"
	"github.com/codevideo/codevideo-cli/cli/progress"
	"github.com/codevideo/codevideo-cli/cloud"
	"github.com/codevideo/codevideo-cli/constants"
	"github.com/codevideo/codevideo-cli/elevenlabs"
	"github.com/codevideo/codevideo-cli/types"
	"github.com/codevideo/codevideo-cli/utils"
	"github.com/google/uuid"
)

// Generator handles the creation of CodeVideo manifests
type Generator struct {
	RenderMode     types.RenderMode
	RenderSeed     string
	RenderWorkers  int
	RenderStrategy types.RenderStrategy
	Environment    string
	UserID         string
	IDEProps       *types.CodeVideoIDEProps
	// BeforeLesson, if set, runs before each lesson of a course is narrated.
	BeforeLesson func(index int)
}

// NewGenerator creates a new manifest generator
func NewGenerator() *Generator {
	// For local use, we'll generate a random user ID
	// In the future, this could be tied to a local config file or login
	// userID := uuid.New().String()

	return &Generator{
		Environment: strings.ToUpper(os.Getenv("ENVIRONMENT")),
		UserID:      "local-cli-user",
		IDEProps:    nil, // Will be set when config is loaded
	}
}

// GenerateFromActions creates a manifest from a list of actions
func (g *Generator) GenerateFromActions(actions []types.Action) (*types.CodeVideoManifest, error) {
	// Generate a unique UUID for this manifest
	uuid := uuid.New().String()

	if notify.Enabled() {
		// Slack log a nice "/> CodeVideo" logo
		notify.Send(`


/>\ CodeVideo CLI


`)
		ramUsage, err := utils.GetRAMUsage()
		if err != nil {
			log.Debugf("Failed to get RAM usage: %v", err)
		}
		environmentEnv := strings.ToUpper(os.Getenv("ENVIRONMENT"))
		notify.Send(fmt.Sprintf("%s: Processing video job: %s (Job has %d actions; RAM usage is at %s)", environmentEnv, uuid, len(actions), ramUsage))
	}
	log.Debugf("Processing video job %s with %d actions", uuid, len(actions))

	audioItems, err := generateAudioItems(actions)
	if err != nil {
		return nil, fmt.Errorf("error generating audio items: %w", err)
	}

	return &types.CodeVideoManifest{
		RenderStrategy: g.RenderStrategy, RenderMode: g.RenderMode, RenderSeed: g.RenderSeed, RenderWorkers: g.RenderWorkers,
		Environment:       g.Environment,
		UserID:            g.UserID,
		UUID:              uuid,
		Actions:           actions,
		AudioItems:        audioItems,
		CodeVideoIDEProps: g.IDEProps,
	}, nil
}

// GenerateFromLesson creates a manifest from a lesson
func (g *Generator) GenerateFromLesson(lesson types.Lesson) (*types.CodeVideoManifest, error) {
	audioItems, err := generateAudioItems(lesson.Actions)
	if err != nil {
		return nil, fmt.Errorf("error generating audio items: %w", err)
	}

	return &types.CodeVideoManifest{
		RenderStrategy: g.RenderStrategy, RenderMode: g.RenderMode, RenderSeed: g.RenderSeed, RenderWorkers: g.RenderWorkers,
		Environment:       g.Environment,
		UserID:            g.UserID,
		UUID:              uuid.New().String(),
		Lesson:            lesson,
		AudioItems:        audioItems,
		CodeVideoIDEProps: g.IDEProps,
	}, nil
}

// GenerateFromCourse creates multiple manifests from a course, one for each lesson
func (g *Generator) GenerateFromCourse(course types.Course) ([]*types.CodeVideoManifest, error) {
	var manifests []*types.CodeVideoManifest

	for i, lesson := range course.Lessons {
		if g.BeforeLesson != nil {
			g.BeforeLesson(i)
		}
		manifest, err := g.GenerateFromLesson(lesson)
		if err != nil {
			return nil, fmt.Errorf("lesson %d: %w", i+1, err)
		}
		if g.RenderSeed != "" {
			manifest.RenderSeed = fmt.Sprintf("%s:lesson:%d:%s", g.RenderSeed, i, lesson.ID)
		}
		manifests = append(manifests, manifest)
	}

	return manifests, nil
}

// SaveManifest saves a manifest to a file in the specified directory
func (g *Generator) SaveManifest(manifest *types.CodeVideoManifest) (string, error) {
	// Marshal the manifest to JSON
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal manifest: %w", err)
	}

	newFolderPath := constants.NewFolder()

	// Ensure directory exists
	if err := os.MkdirAll(newFolderPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", newFolderPath, err)
	}

	// Generate the file path
	filePath := filepath.Join(newFolderPath, manifest.UUID+".json")

	// Write the file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write manifest file: %w", err)
	}

	return filePath, nil
}

// generateAudioItems processes the given actions. For each action whose name starts with
// "author-speak", it converts the text to audio via ElevenLabs and uploads the audio file to S3.
// Each clip's measured length goes to the CLI's job, which sharpens the recording estimate.
func generateAudioItems(actions []types.Action) ([]types.AudioItem, error) {
	clips := CountSpeech(actions)
	if clips == 0 {
		return nil, nil
	}
	job := progress.Current()
	job.PlanNarration(clips)
	job.Begin(progress.Narration)
	var audioManifest []types.AudioItem

	// Provider switch: "elevenlabs" (default, cloud + S3) or "kokoro" (self-hosted
	// codevideo-tts service, embedded as a data URI — no S3/cloud account needed).
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("CODEVIDEO_TTS_PROVIDER")))
	if provider == "" {
		provider = "elevenlabs"
	}
	ttsApiKey := os.Getenv("ELEVEN_LABS_API_KEY")
	ttsVoiceId := resolveTTSVoiceID()
	if provider == "elevenlabs" {
		if ttsVoiceId == "" {
			log.Info("No explicit ElevenLabs voice ID found in env; ElevenLabs client default voice will be used")
		} else {
			log.Debug("Using ElevenLabs voice ID from environment")
		}
	} else {
		log.Infof("Using self-hosted TTS provider %q (audio embedded as data URI, no S3)", provider)
	}

	for i, action := range actions {
		textToSpeak := action.Value
		// Include voice ID in the hash so changing voices always generates new audio objects.
		textHash := utils.Sha256Hash(fmt.Sprintf("%s::%s", ttsVoiceId, textToSpeak))
		if strings.HasPrefix(action.Name, "author-speak") {
			log.Debugf("Converting text at step index %d to audio... (hash is %s)", i, textHash)
			var mp3Url string
			var audioData []byte
			var err error
			if provider == "kokoro" {
				audioData, err = getAudioFromKokoro(textToSpeak)
				if err != nil {
					return nil, fmt.Errorf("error converting text to audio via kokoro: %w", err)
				}
				mp3Url = "data:audio/mpeg;base64," + base64.StdEncoding.EncodeToString(audioData)
			} else {
				audioData, err = elevenlabs.GetAudioArrayBufferElevenLabs(textToSpeak, ttsApiKey, ttsVoiceId)
				if err != nil {
					return nil, fmt.Errorf("error converting text to audio: %w", err)
				}
				ctx := context.Background()
				mp3Url, err = cloud.UploadFileToS3(ctx, audioData, "v3/audio", fmt.Sprintf("%s.mp3", textHash))
				if err != nil {
					return nil, fmt.Errorf("error uploading audio to S3: %w", err)
				}
			}
			audioManifest = append(audioManifest, types.AudioItem{
				Text: textToSpeak, Mp3Url: mp3Url, ActionIndex: &i,
			})
			job.ClipDone(i, clipDurationMs(job, audioData))
		}
	}

	log.Debug("Done with audio conversion")

	return audioManifest, nil
}

// CountSpeech returns how many actions are narrated.
func CountSpeech(actions []types.Action) int {
	count := 0
	for _, action := range actions {
		if strings.HasPrefix(action.Name, "author-speak") {
			count++
		}
	}
	return count
}

// clipDurationMs measures a narration clip for the progress estimate, or
// returns 0 when there's no CLI job or ffprobe can't read it.
func clipDurationMs(job *progress.Job, audio []byte) float64 {
	if job == nil {
		return 0
	}
	ms, err := utils.AudioDurationMs(context.Background(), audio)
	if err != nil {
		log.Debugf("Could not measure narration clip: %v", err)
		return 0
	}
	return ms
}

// getAudioFromKokoro synthesizes speech via the self-hosted codevideo-tts
// service (OpenAI-compatible POST /v1/audio/speech) and returns mp3 bytes.
// TTS_SERVICE_URL points at the service (default http://localhost:3000); an
// optional TTS_API_KEY is sent as a Bearer token.
func getAudioFromKokoro(text string) ([]byte, error) {
	base := strings.TrimRight(os.Getenv("TTS_SERVICE_URL"), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	payload, err := json.Marshal(map[string]interface{}{
		"input":           text,
		"response_format": "mp3",
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, base+"/v1/audio/speech", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key := os.Getenv("TTS_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach TTS service at %s: %w", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TTS service returned %d: %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}

func resolveTTSVoiceID() string {
	voiceID := strings.TrimSpace(os.Getenv("ELEVEN_LABS_VOICE_ID"))
	if voiceID != "" {
		return voiceID
	}

	// If the primary variable is empty, prefer the explicitly named Chris voice when available.
	chrisVoiceID := strings.TrimSpace(os.Getenv("ELEVEN_LABS_VOICE_ID_CHRIS"))
	if chrisVoiceID != "" {
		return chrisVoiceID
	}

	return ""
}
