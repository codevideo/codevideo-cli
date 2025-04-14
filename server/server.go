package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/user"
	"github.com/codevideo/codevideo-cli/cli/renderer"
	"github.com/codevideo/codevideo-cli/cloud"
	"github.com/codevideo/codevideo-cli/constants"
	"github.com/codevideo/codevideo-cli/files"
	"github.com/codevideo/codevideo-cli/mail"
	"github.com/codevideo/codevideo-cli/utils"
	"github.com/fsnotify/fsnotify"

	slack "github.com/codevideo/go-utils/slack"
)

var debounceMu sync.Mutex
var debounceMap = make(map[string]*time.Timer)

// WatchForManifestFiles is used within the codevideo-api to watch for new manifest files in the 'new' folder.
// When a new manifest file is detected, it is processed as a job.
func WatchForManifestFiles() {
	// Ensure required directories exist.
	for _, dir := range []string{constants.NEW_FOLDER, constants.ERROR_FOLDER, constants.SUCCESS_FOLDER, constants.VIDEO_FOLDER} {
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

	if err := watcher.Add(constants.NEW_FOLDER); err != nil {
		log.Fatal(err)
	}

	// semaphore channel to limit concurrency to maxConcurrentJobs jobs.
	semaphore := make(chan struct{}, constants.MAX_CONCURRENT_JOBS)

	// get absolute path of 'new' folder
	absPath, err := filepath.Abs(constants.NEW_FOLDER)
	if err != nil {
		log.Fatalf("Error getting absolute path of %s: %v", constants.NEW_FOLDER, err)
	}

	log.Println("Watching for new manifest files in", absPath)

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
							ProcessJob(filePath, "serve")
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
			log.Printf("Watcher error:", err)
		}
	}
}

// ProcessJob reads the manifest file, calls the Puppeteer script, sends an email if successful,
// and moves the manifest to the error or success folder.
func ProcessJob(manifestPath string, mode string) {
	// still at 10 from the audio generation step
	if mode == "cli" {
		renderer.RenderProgressToConsole(10, "Starting up video recording...")
	}
	base := filepath.Base(manifestPath)
	manifest, err := files.UnmarshalManifest(manifestPath)
	if err != nil {
		log.Printf("Failed to unmarshal manifest file: %v", err)
		utils.AddErrorToManifest(manifestPath, err.Error())
		return
	}
	environment := manifest.Environment
	uuid := manifest.UUID
	clerkUserId := manifest.UserID

	ramUsage, err := utils.GetRAMUsage()
	if err != nil {
		log.Printf("Failed to get RAM usage: %v", err)
	}
	message := fmt.Sprintf("%s: Processing video job: %s (Job has %d actions; RAM usage is at %s)", os.Getenv("ENVIRONMENT"), uuid, len(manifest.Actions), ramUsage)
	log.Print(message)
	slack.SendSlackMessage(message)

	operatingSystem := os.Getenv("OPERATING_SYSTEM")

	// Call the Puppeteer script using node with the uuid and operating system as arguments.
	shouldReturn := RunPuppeteerForUUID(uuid, operatingSystem, mode)
	if shouldReturn {
		return
	}

	// If the Puppeteer script succeeded, convert the webm file to mp4
	webmPath := filepath.Join(constants.VIDEO_FOLDER, uuid+".webm")
	mp4Path := filepath.Join(constants.VIDEO_FOLDER, uuid+".mp4")
	log.Printf("Converting webm to mp4 for job %s", uuid)
	if err := utils.ConvertToMp4(webmPath, mp4Path, mode); err != nil {
		log.Printf("Failed to convert webm to mp4 for job %s: %v", uuid, err)
		utils.AddErrorToManifest(manifestPath, err.Error())
		return
	}
	log.Printf("Converted webm to mp4 for job %s", uuid)

	// we only need to upload to S3 and update clerk data if we are in serve mode
	if mode == "serve" {
		// Read and upload the mp4 to S3.
		mp4Bytes, err := os.ReadFile(mp4Path)
		if err != nil {
			log.Printf("Failed to read mp4 file for job %s: %v", uuid, err)
			utils.AddErrorToManifest(manifestPath, err.Error())
			return
		}

		log.Printf("Uploading mp4 for job %s", uuid)
		mp4Url, err := cloud.UploadFileToS3(context.Background(), mp4Bytes, "v3/video", uuid+".mp4")
		if err != nil {
			log.Printf("Failed to upload file for job %s: %v", uuid, err)
			utils.AddErrorToManifest(manifestPath, err.Error())
			return
		}
		log.Printf("Uploaded mp4 for job %s to %s", uuid, mp4Url)

		// use the clerk userID to get the email address of the user
		// be sure to initialize the clerk client with the correct API key according to whether the environment of the job is staging or prod
		shouldReturn = updateClerkUserData(environment, clerkUserId, manifestPath, mp4Url, uuid, base)
		if shouldReturn {
			return
		}
	}

	// serve or cli mode, move the manifest to the success folder.
	if err := files.MoveFile(manifestPath, filepath.Join(constants.SUCCESS_FOLDER, base)); err != nil {
		log.Printf("Failed to move manifest to success folder: %v", err)
	} else {
		log.Printf("Job %s processed successfully", uuid)
		ramUsage, err = utils.GetRAMUsage()
		if err != nil {
			log.Printf("Failed to get RAM usage: %v", err)
		}
		environment := os.Getenv("ENVIRONMENT")
		if environment == "" {
			environment = "unknown env"
		}
		slack.SendSlackMessage(fmt.Sprintf("%s: Job %s processed successfully (RAM usage is at %s)", strings.ToUpper(environment), uuid, ramUsage))
	}

	if mode == "cli" {
		// copy the mp4 file to the root of this project
		now := time.Now()
		formattedTime := now.Format("2006-01-02-15-04-05")
		finalizedFileName := "CodeVideo-" + formattedTime + ".mp4"
		if err := utils.CopyFile(mp4Path, filepath.Join(".", finalizedFileName)); err != nil {
			log.Printf("Failed to copy mp4 file for job %s: %v", uuid, err)
		}
		fmt.Println()
		fmt.Println("✅ CodeVideo successfully generated and saved to " + finalizedFileName)
		fmt.Println()
	}

	// cleanup: remove the mp4 and webm files, decrement 10 tokens from the user

	if err := os.Remove(mp4Path); err != nil {
		log.Printf("Failed to remove mp4 file for job %s: %v", uuid, err)
	}

	if err := os.Remove(webmPath); err != nil {
		log.Printf("Failed to remove webm file for job %s: %v", uuid, err)
	}
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

		// add an error key and value to the manifest file.
		utils.AddErrorToManifest(manifestPath, err.Error())
		log.Printf("Failed to add error to manifest file: %v", err)

		if err := files.MoveFile(manifestPath, filepath.Join(constants.ERROR_FOLDER, base)); err != nil {
			log.Printf("Failed to move manifest to error folder: %v", err)
		}
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

func RunPuppeteerForUUID(uuid string, operatingSystem string, mode string) bool {
	cmd := exec.Command("node", constants.NODE_SCRIPT_NAME, uuid, operatingSystem)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Error obtaining stdout pipe: %v", err)
		return true
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("Error obtaining stderr pipe: %v", err)
		return true
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Job %s failed to start: %v", uuid, err)
		return true
	}

	// Stream stdout concurrently.
	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			text := scanner.Text()
			log.Printf("[Puppeteer stdout]: %s", text)
			if strings.Contains(text, "progress") {
				// regex everything between ' ' characters
				progress, err := utils.ExtractProgress(text)
				if err != nil {
					log.Printf("Failed to extract progress: %v", err)
				}
				log.Printf("Extracted progress: %f", progress)

				// now, since we always are already at 10, we want to show progress even if we are less than 0,
				// so we scale it in from 10 to 90
				// (we also have conversion to mp4 still to do)
				progress = 10 + (progress * 0.8)
				if mode == "cli" {
					renderer.RenderProgressToConsole(progress, "Rendering video...")
				}
			}
		}
		if err := scanner.Err(); err != nil {
			log.Printf("Error reading stdout: %v", err)
		}
	}()

	// Stream stderr concurrently.
	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			log.Printf("[Puppeteer stderr]: %s", scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			log.Printf("Error reading stderr: %v", err)
		}
	}()

	// Wait for the command to finish.
	if err := cmd.Wait(); err != nil {
		log.Printf("Job %s failed: %v", uuid, err)
		// Optionally capture additional output if needed.
	}
	return false
}
