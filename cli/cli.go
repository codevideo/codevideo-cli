package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/codevideo/codevideo-cli/cli/detector"
	"github.com/codevideo/codevideo-cli/cli/generator"
	"github.com/codevideo/codevideo-cli/server"
	"github.com/spf13/cobra"
)

// Execute runs the CLI workflow with the provided project data
func Execute(cmd *cobra.Command) error {
	// Get project JSON data from flags
	projectJSON, _ := cmd.Flags().GetString("project")
	if projectJSON == "" {
		cmd.Help()
		return nil
	}

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	setupCancellation(ctx, cancel)

	log.Printf("Analyzing project JSON: %s", projectJSON)
	course, lesson, actions, err := detector.DetectProjectType(projectJSON)
	if err != nil {
		return fmt.Errorf("failed to detect project type: %w", err)
	}

	generator := generator.NewGenerator()

	if course != nil {
		log.Printf("Starting Course workflow processing")
		fmt.Println("Detected project type: Course")
		fmt.Println("/> CodeVideo generation in progress...")
		manifests := generator.GenerateFromCourse(*course)
		// for each manifest, get its absolute path and call server.ProcessJob
		for _, manifest := range manifests {
			manifestPath, err := generator.SaveManifest(manifest)
			if err != nil {
				return fmt.Errorf("failed to save manifest: %w", err)
			}
			server.ProcessJob(manifestPath, "cli")
		}
	}

	if lesson != nil {
		log.Printf("Starting Lesson workflow processing")
		fmt.Println("Detected project type: Lesson")
		fmt.Println("/> CodeVideo generation in progress...")
		manifest := generator.GenerateFromLesson(*lesson)
		manifestPath, err := generator.SaveManifest(manifest)
		if err != nil {
			return fmt.Errorf("failed to save manifest: %w", err)
		}
		server.ProcessJob(manifestPath, "cli")
	}

	if actions != nil {
		log.Printf("Starting Actions workflow processing")
		fmt.Println("Detected project type: Actions")
		fmt.Println("/> CodeVideo generation in progress...")
		manifest := generator.GenerateFromActions(*actions)
		manifestPath, err := generator.SaveManifest(manifest)
		if err != nil {
			return fmt.Errorf("failed to save manifest: %w", err)
		}
		server.ProcessJob(manifestPath, "cli")
	}

	return nil
}

// setupCancellation configures graceful shutdown on interrupt
func setupCancellation(ctx context.Context, cancel context.CancelFunc) {
	c := make(chan os.Signal, 1)
	go func() {
		<-c
		fmt.Println("\nCancelling operations...")
		cancel()
		// Give a little time for cleanup
		time.Sleep(1 * time.Second)
		os.Exit(0)
	}()
}
