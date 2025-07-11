package generator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/codevideo/codevideo-cli/cli/renderer"
	"github.com/codevideo/codevideo-cli/cloud"
	"github.com/codevideo/codevideo-cli/constants"
	"github.com/codevideo/codevideo-cli/elevenlabs"
	"github.com/codevideo/codevideo-cli/types"
	"github.com/codevideo/codevideo-cli/utils"
	"github.com/google/uuid"

	slack "github.com/codevideo/go-utils/slack"
)

// Generator handles the creation of CodeVideo manifests
type Generator struct {
	Environment string
	UserID      string
	IDEProps    *types.CodeVideoIDEProps
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
func (g *Generator) GenerateFromActions(actions []types.Action) *types.CodeVideoManifest {
	// Generate a unique UUID for this manifest
	uuid := uuid.New().String()

	// Slack log a nice "/> CodeVideo" logo
	logo := `


/>\ CodeVideo CLI


`
	log.Print(logo)
	slack.SendSlackMessage(logo)

	ramUsage, err := utils.GetRAMUsage()
	if err != nil {
		log.Printf("Failed to get RAM usage: %v", err)
	}
	environmentEnv := strings.ToUpper(os.Getenv("ENVIRONMENT"))
	message := fmt.Sprintf("%s: Processing video job: %s (Job has %d actions; RAM usage is at %s)", environmentEnv, uuid, len(actions), ramUsage)
	log.Print(message)
	slack.SendSlackMessage(message)

	audioItems, err := generateAudioItems(actions)
	if err != nil {
		log.Fatalf("Error generating audio items: %v", err)
	}

	return &types.CodeVideoManifest{
		Environment:       g.Environment,
		UserID:            g.UserID,
		UUID:              uuid,
		Actions:           actions,
		AudioItems:        audioItems,
		CodeVideoIDEProps: g.IDEProps,
	}
}

// GenerateFromLesson creates a manifest from a lesson
func (g *Generator) GenerateFromLesson(lesson types.Lesson) *types.CodeVideoManifest {
	audioItems, err := generateAudioItems(lesson.Actions)
	if err != nil {
		log.Fatalf("Error generating audio items: %v", err)
	}

	return &types.CodeVideoManifest{
		Environment:       g.Environment,
		UserID:            g.UserID,
		UUID:              uuid.New().String(),
		Lesson:            lesson,
		AudioItems:        audioItems,
		CodeVideoIDEProps: g.IDEProps,
	}
}

// GenerateFromCourse creates multiple manifests from a course, one for each lesson
func (g *Generator) GenerateFromCourse(course types.Course) []*types.CodeVideoManifest {
	var manifests []*types.CodeVideoManifest

	for _, lesson := range course.Lessons {
		manifests = append(manifests, g.GenerateFromLesson(lesson))
	}

	return manifests
}

// SaveManifest saves a manifest to a file in the specified directory
func (g *Generator) SaveManifest(manifest *types.CodeVideoManifest) (string, error) {
	// Marshal the manifest to JSON
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// Get executable directory
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}
	execDir := filepath.Dir(execPath)

	// Create absolute path to NEW_FOLDER directory relative to executable
	newFolderPath := filepath.Join(execDir, constants.NEW_FOLDER)

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
func generateAudioItems(actions []types.Action) ([]types.AudioItem, error) {
	progress := 0.0
	renderer.RenderProgressToConsole(progress, "Generating audio for speaking actions...")
	var audioManifest []types.AudioItem

	ttsApiKey := os.Getenv("ELEVEN_LABS_API_KEY")
	ttsVoiceId := os.Getenv("ELEVEN_LABS_VOICE_ID")

	for i, action := range actions {
		textToSpeak := action.Value
		textHash := utils.Sha256Hash(textToSpeak)
		if strings.HasPrefix(action.Name, "author-speak") {
			log.Printf("Converting text at step index %d to audio... (hash is %s)\n", i, textHash)
			audioData, err := elevenlabs.GetAudioArrayBufferElevenLabs(textToSpeak, ttsApiKey, ttsVoiceId)
			if err != nil {
				return nil, fmt.Errorf("error converting text to audio: %w", err)
			}
			ctx := context.Background()
			mp3Url, err := cloud.UploadFileToS3(ctx, audioData, "v3/audio", fmt.Sprintf("%s.mp3", textHash))
			if err != nil {
				return nil, fmt.Errorf("error uploading audio to S3: %w", err)
			}
			audioManifest = append(audioManifest, types.AudioItem{
				Text:   textToSpeak,
				Mp3Url: mp3Url,
			})
		}
		// since audio is only about 10% of the total time, we'll cap the max progress at 10%
		progress = (float64(i) / float64(len(actions)) * 10)
		renderer.RenderProgressToConsole(progress, "Generating audio for speaking actions...")
	}

	log.Printf("Done with audio conversion and upload\n")
	renderer.RenderProgressToConsole(10, "Done with audio generation")

	return audioManifest, nil
}
