package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/codevideo/codevideo-cli/cli/config"
	"github.com/codevideo/codevideo-cli/cli/estimate"
	"github.com/codevideo/codevideo-cli/cli/notify"
	"github.com/codevideo/codevideo-cli/cli/progress"
	"github.com/codevideo/codevideo-cli/cloud"
	"github.com/codevideo/codevideo-cli/constants"
	"github.com/codevideo/codevideo-cli/files"
	"github.com/codevideo/codevideo-cli/mail"
	"github.com/codevideo/codevideo-cli/types"
	"github.com/codevideo/codevideo-cli/utils"
	"github.com/fsnotify/fsnotify"
)

var debounceMu sync.Mutex
var debounceMap = make(map[string]*time.Timer)

// WatchForManifestFiles is used within the codevideo-api to watch for new manifest files in the 'new' folder.
// When a new manifest file is detected, it is processed as a job.
func WatchForManifestFiles() {
	// Ensure required directories exist.
	for _, dir := range []string{constants.NewFolder(), constants.ErrorFolder(), constants.SuccessFolder(), constants.VideoFolder()} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("Error creating directory %s: %v", dir, err)
		}
	}

	// Set up a watcher on the newFolder.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	if err := watcher.Add(constants.NewFolder()); err != nil {
		log.Fatal(err)
	}

	// semaphore channel to limit concurrency (overridable via CODEVIDEO_MAX_CONCURRENT_JOBS).
	maxConcurrentJobs := constants.MaxConcurrentJobs()
	log.Printf("Worker concurrency limit: %d", maxConcurrentJobs)
	semaphore := make(chan struct{}, maxConcurrentJobs)

	log.Println("Watching for new manifest files in", constants.NewFolder())

	// Listen for filesystem events.
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			// Process only create events for new JSON files.
			if event.Op&fsnotify.Create == fsnotify.Create {
				if filepath.Ext(event.Name) == ".json" {
					debounceMu.Lock()
					// If a timer exists for this file, stop it to reset the debounce period.
					if timer, exists := debounceMap[event.Name]; exists {
						timer.Stop()
					}
					// Set a new timer with a 500ms debounce interval.
					debounceMap[event.Name] = time.AfterFunc(500*time.Millisecond, func() {
						log.Printf("Detected new file: %s", event.Name)
						// Limit concurrent processing.
						semaphore <- struct{}{}
						go func(filePath string) {
							defer func() { <-semaphore }()
							// Optional delay to ensure the file is fully written.
							time.Sleep(2 * time.Second)
							ProcessJob(filePath, "serve", "")
						}(event.Name)
						// Clean up the timer from the map.
						debounceMu.Lock()
						delete(debounceMap, event.Name)
						debounceMu.Unlock()
					})
					debounceMu.Unlock()
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

// ProcessJob reads the manifest file, calls the Puppeteer script, sends an email if successful,
// and moves the manifest to the error or success folder. In cli mode it returns the saved
// video's path, and reports progress to the CLI's current job.
func ProcessJob(manifestPath string, mode string, outputPath string) (string, error) {
	saved, err := processJob(manifestPath, mode, outputPath)
	// A failed serve job used to stay in new/ (the watcher only reacts to new files,
	// so it was never retried) unless the failure was the email step. Every failure
	// now records its error in the manifest and lands in error/. Its webm/mp4 stay in
	// the video folder for debugging; the API sweeps that folder after a day.
	if err != nil && mode == "serve" {
		moveManifestToErrorFolder(manifestPath)
	}
	return saved, err
}

func moveManifestToErrorFolder(manifestPath string) {
	target := filepath.Join(constants.ErrorFolder(), filepath.Base(manifestPath))
	if err := files.MoveFile(manifestPath, target); err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to move manifest to error folder: %v", err)
	}
}

func processJob(manifestPath string, mode string, outputPath string) (string, error) {
	job := progress.Current()
	base := filepath.Base(manifestPath)
	manifest, err := files.UnmarshalManifest(manifestPath)
	if err != nil {
		log.Printf("Failed to unmarshal manifest file: %v", err)
		utils.AddErrorToManifest(manifestPath, err.Error())
		return "", fmt.Errorf("reading the job manifest: %w", err)
	}
	environment := manifest.Environment
	uuid := manifest.UUID
	clerkUserId := manifest.UserID

	videoFolder := constants.VideoFolder()
	if err := os.MkdirAll(videoFolder, 0755); err != nil {
		log.Printf("Failed to create video folder: %v", err)
		utils.AddErrorToManifest(manifestPath, err.Error())
		return "", fmt.Errorf("creating the video folder: %w", err)
	}
	webmPath := filepath.Join(videoFolder, uuid+".webm")

	// Use the provided outputPath if it's not empty, otherwise use the default
	var mp4Path string
	if outputPath != "" {
		mp4Path = outputPath
		// Ensure the output directory exists
		outputDir := filepath.Dir(mp4Path)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			log.Printf("Failed to create output directory: %v", err)
			utils.AddErrorToManifest(manifestPath, err.Error())
			return "", fmt.Errorf("creating the output folder: %w", err)
		}
	} else {
		mp4Path = filepath.Join(videoFolder, uuid+".mp4")
	}

	renderMode, err := types.ResolveRenderMode(manifest.RenderMode, os.Getenv("CODEVIDEO_RENDER_MODE"))
	if err != nil {
		utils.AddErrorToManifest(manifestPath, err.Error())
		return "", err
	}
	// Narration is measured by now, so the estimate can use the real clips.
	PlanProgress(job, manifestActions(manifest), manifest.CodeVideoIDEProps, renderMode, manifest.RenderWorkers)
	job.Begin(progress.Starting)
	if renderMode == types.RenderModeDeterministic {
		if err := processDeterministicJob(manifestPath, manifest, mp4Path); err != nil {
			utils.AddErrorToManifest(manifestPath, err.Error())
			return "", err
		}
	} else {
		// Call the Puppeteer script using node with the uuid and explicit output path.
		if err := RunPuppeteerForUUID(uuid, manifestPath, webmPath, jobOrientation(manifest)); err != nil {
			log.Printf("Puppeteer recording failed for job %s: %v", uuid, err)
			utils.AddErrorToManifest(manifestPath, "Puppeteer recording failed")
			return "", fmt.Errorf("recording failed: %w", err)
		}

		// Puppeteer succeeded, now convert to mp4
		log.Debugf("Converting webm to mp4 for job %s", uuid)
		if err := utils.ConvertToMp4(webmPath, mp4Path); err != nil {
			log.Printf("Failed to convert webm to mp4 for job %s: %v", uuid, err)
			utils.AddErrorToManifest(manifestPath, fmt.Sprintf("Failed to convert video: %v", err))
			return "", fmt.Errorf("converting to mp4 failed: %w", err)
		}

		log.Debugf("Converted webm to mp4 for job %s", uuid)

	}

	// we only need to upload to S3 and update clerk data if we are in serve mode
	if mode == "serve" {
		// Read and upload the mp4 to S3.
		mp4Bytes, err := os.ReadFile(mp4Path)
		if err != nil {
			log.Printf("Failed to read mp4 file for job %s: %v", uuid, err)
			utils.AddErrorToManifest(manifestPath, err.Error())
			return "", fmt.Errorf("job %s failed; see manifest and logs", manifestPath)
		}

		log.Printf("Uploading mp4 for job %s", uuid)
		mp4Url, err := cloud.UploadFileToS3(context.Background(), mp4Bytes, "v3/video", uuid+".mp4")
		if err != nil {
			log.Printf("Failed to upload file for job %s: %v", uuid, err)
			utils.AddErrorToManifest(manifestPath, err.Error())
			return "", fmt.Errorf("job %s failed; see manifest and logs", manifestPath)
		}
		log.Printf("Uploaded mp4 for job %s to %s", uuid, mp4Url)

		// use the clerk userID to get the email address of the user
		// be sure to initialize the clerk client with the correct API key according to whether the environment of the job is staging or prod
		shouldReturn := updateClerkUserData(environment, clerkUserId, manifestPath, mp4Url, uuid, base)
		if shouldReturn {
			return "", fmt.Errorf("job %s failed; see manifest and logs", manifestPath)
		}
	}

	// serve or cli mode, move the manifest to the success folder.
	if err := files.MoveFile(manifestPath, filepath.Join(constants.SuccessFolder(), base)); err != nil {
		log.Warnf("Failed to move manifest to success folder: %v", err)
	} else {
		log.Debugf("Job %s processed successfully", uuid)
		if notify.Enabled() {
			ramUsage, err := utils.GetRAMUsage()
			if err != nil {
				log.Printf("Failed to get RAM usage: %v", err)
			}
			environment := os.Getenv("ENVIRONMENT")
			if environment == "" {
				environment = "unknown env"
			}
			notify.Send(fmt.Sprintf("%s: Job %s processed successfully (RAM usage is at %s)", strings.ToUpper(environment), uuid, ramUsage))
		}
	}

	saved := ""
	if mode == "cli" {
		saved = outputPath
		// If outputPath is provided, we've already written to that location
		// Otherwise, copy the mp4 file to the default location
		if outputPath == "" {
			// The existing code to copy the mp4 to the default location
			now := time.Now()
			formattedTime := now.Format("2006-01-02-15-04-05")
			finalizedFileName := "CodeVideo-" + formattedTime + ".mp4"
			finalizedFilePath := filepath.Join(constants.OutputFolder(), finalizedFileName)

			// Copy the file to the root directory, which CODEVIDEO_OUTPUT_DIR may
			// name before it exists
			if err := os.MkdirAll(constants.OutputFolder(), 0755); err != nil {
				return "", fmt.Errorf("creating the output folder: %w", err)
			}
			if err := utils.CopyFile(mp4Path, finalizedFilePath); err != nil {
				log.Printf("Failed to copy output file: %v", err)
				return "", err
			}
			if renderMode == types.RenderModeDeterministic {
				for _, suffix := range []string{".render-trace.json", ".render-metrics.json"} {
					source := strings.TrimSuffix(mp4Path, filepath.Ext(mp4Path)) + suffix
					target := strings.TrimSuffix(finalizedFilePath, filepath.Ext(finalizedFilePath)) + suffix
					if err := utils.CopyFile(source, target); err != nil {
						return "", err
					}
				}
			}
			saved = finalizedFilePath
		}
		// The CLI prints the saved path once the progress bars are gone.
	}

	// cleanup: remove the mp4 and webm files, decrement 10 tokens from the user

	if outputPath == "" {
		if err := os.Remove(mp4Path); err != nil {
			log.Printf("Failed to remove mp4 file for job %s: %v", uuid, err)
		}
	}

	if err := os.Remove(webmPath); err != nil && !os.IsNotExist(err) {
		log.Printf("Failed to remove webm file for job %s: %v", uuid, err)
	}
	job.Finish(saved)
	return saved, nil
}

func updateClerkUserData(environment string, clerkUserId string, manifestPath string, mp4Url string, uuid string, base string) bool {
	apiKey := os.Getenv("CLERK_SECRET_KEY")
	if environment == "staging" {
		apiKey = os.Getenv("CLERK_SECRET_KEY_STAGING")
	}

	if apiKey == "" {
		log.Printf("CLERK_SECRET_KEY not set")
	}
	config := &clerk.ClientConfig{}
	config.Key = &apiKey
	client := user.NewClient(config)
	clerkUser, err := client.Get(context.Background(), clerkUserId)
	if err != nil {
		log.Printf("Failed to get user: %v", err)
		utils.AddErrorToManifest(manifestPath, err.Error())
		return true
	}
	userEmail := clerkUser.EmailAddresses[0].EmailAddress

	// Then send an email notification including the mp4 URL.
	if err := mail.SendEmail(userEmail, mp4Url); err != nil {
		log.Printf("Failed to send email for job %s: %v", uuid, err)

		// add an error key and value to the manifest file; ProcessJob moves it to error/.
		utils.AddErrorToManifest(manifestPath, err.Error())
		return true
	}

	log.Printf("Email sent to %s for job %s", userEmail, uuid)

	// also update the user metadata to decrement the number of tokens by TOKEN_DECREMENT_AMOUNT
	currentTokens := 0

	if clerkUser.PublicMetadata != nil {
		var meta map[string]interface{}
		if err := json.Unmarshal(clerkUser.PublicMetadata, &meta); err == nil {
			switch v := meta["tokens"].(type) {
			case float64:
				currentTokens = int(v)
			case string:
				currentTokens, _ = strconv.Atoi(v)
			}
		}
	}

	newTokens := currentTokens - constants.TOKEN_DECREMENT_AMOUNT
	metadata, _ := json.Marshal(map[string]interface{}{"tokens": newTokens})
	params := user.UpdateMetadataParams{
		PublicMetadata: (*json.RawMessage)(&metadata),
	}

	if _, err := client.UpdateMetadata(context.Background(), clerkUserId, &params); err != nil {
		log.Printf("Failed to update user metadata: %v", err)
		utils.AddErrorToManifest(manifestPath, err.Error())
	} else {
		log.Printf("Successfully decremented user tokens from %d to %d", currentTokens, newTokens)
	}
	return false
}

// jobOrientation prefers the job's own orientation, which the API writes as
// videoOptions, over the worker-wide default, so one worker renders both
// landscape and portrait jobs. Resolution stays worker-wide: a 4K job would also
// need the page's resolution prop, which manifests don't carry yet.
func jobOrientation(manifest *types.CodeVideoManifest) string {
	if manifest != nil && manifest.VideoOptions != nil {
		switch orientation := manifest.VideoOptions.Orientation; orientation {
		case "landscape", "portrait":
			return orientation
		case "":
		default:
			log.Printf("Ignoring unknown orientation %q for job %s", orientation, manifest.UUID)
		}
	}
	return config.GlobalConfig.Orientation
}

// PlanProgress sets the CLI job's expected stages for rendering actions.
// Narration measured so far replaces word-count guesses for those actions.
func PlanProgress(job *progress.Job, actions []types.Action, props *types.CodeVideoIDEProps, mode types.RenderMode, workers int) {
	if job == nil {
		return
	}
	options := estimate.OptionsFromProps(props)
	options.SpeechMs = job.SpeechMs()
	durations := estimate.ActionDurationsMs(actions, options)
	if mode == types.RenderModeDeterministic {
		job.PlanDeterministic(durations, workers)
		return
	}
	job.PlanRecording(durations)
}

func manifestActions(manifest *types.CodeVideoManifest) []types.Action {
	if len(manifest.Actions) > 0 {
		return manifest.Actions
	}
	return manifest.Lesson.Actions
}

// RunPuppeteerForUUID records the job's webm. The runner's progress lines go
// to the CLI's current job and the rest of its output to the debug log.
func RunPuppeteerForUUID(uuid string, manifestPath string, webmOutputPath string, orientation string) error {
	// Access the global configuration
	resolution := config.GlobalConfig.Resolution

	nodeScriptPath := constants.PuppeteerRunnerPath()

	// Check if the script exists
	if _, err := os.Stat(nodeScriptPath); err != nil {
		return fmt.Errorf("node script is unavailable at %s: %w", nodeScriptPath, err)
	}

	log.Debugf("Using node script at: %s", nodeScriptPath)

	cmd := exec.Command("node", nodeScriptPath,
		"--uuid", uuid,
		"--os", os.Getenv("OPERATING_SYSTEM"),
		"--resolution", resolution,
		"--orientation", orientation,
		"--manifest-path", manifestPath,
		"--output-webm", webmOutputPath)

	// Show the browser window when --headed is set
	if config.GlobalConfig.Headed {
		cmd.Args = append(cmd.Args, "--debug")
	}

	return runRunner(cmd, "Puppeteer", progress.Current())
}
