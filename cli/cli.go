package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/codevideo/codevideo-cli/cli/config"
	"github.com/codevideo/codevideo-cli/cli/detector"
	"github.com/codevideo/codevideo-cli/cli/generator"
	"github.com/codevideo/codevideo-cli/server"
	"github.com/codevideo/codevideo-cli/types"
	"github.com/codevideo/codevideo-cli/utils"
	"github.com/spf13/cobra"
)

// Execute runs the CLI workflow with the provided project data
func Execute(cmd *cobra.Command) error {
	// Load configuration from flags
	if err := config.LoadFromFlags(cmd); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Get project JSON data from flags
	projectJSON, _ := cmd.Flags().GetString("project")
	if projectJSON == "" {
		cmd.Help()
		return nil
	}

	// Store project JSON in config
	config.GlobalConfig.ProjectJSON = projectJSON

	// Load and validate config file if provided
	var ideProps *types.CodeVideoIDEProps
	if config.GlobalConfig.ConfigFilePath != "" {
		var err error
		ideProps, err = config.LoadConfigFile(config.GlobalConfig.ConfigFilePath)
		if err != nil {
			return fmt.Errorf("config validation failed: %w", err)
		}
		log.Printf("Loaded config from: %s", config.GlobalConfig.ConfigFilePath)
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
	if ideProps != nil {
		generator.IDEProps = ideProps
	}

	outputPath, _ := cmd.Flags().GetString("output")

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
			server.ProcessJob(manifestPath, "cli", outputPath)
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
		server.ProcessJob(manifestPath, "cli", outputPath)
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
		server.ProcessJob(manifestPath, "cli", outputPath)
	}

	// if the openFile (--open flag) was passed, open it!
	openFile, _ := cmd.Flags().GetBool("open")
	if openFile {
		if err := utils.OpenFile(outputPath); err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
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
