package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/codevideo/codevideo-cli/cli"
	"github.com/codevideo/codevideo-cli/cli/staticserver"
	"github.com/codevideo/codevideo-cli/constants"
	"github.com/codevideo/codevideo-cli/server"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

// const for version string
const version = "0.0.3"

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "codevideo",
	Short: "CodeVideo's CLI tool",
	Long:  `CodeVideo's CLI tool for processing video jobs.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Check for version flag first
		showVersion, _ := cmd.Flags().GetBool("version")
		if showVersion {
			fmt.Printf("/> CodeVideo CLI v%s\n\n✨Sufficiently advanced technology is indistinguishable from magic.✨", version)
			return
		}

		// Setup logging configuration
		setupLogging(cmd)

		// for either CLI or server mode, we need to start the required servers:
		// start static server for the built gatsby files (7001) and the manifest files it needs (7000)
		ctx := context.Background()
		srv, err := staticserver.Start(ctx)
		if err != nil {
			log.Fatalf("Error starting static server: %v", err)
		}
		defer srv.Stop()
		log.Printf("Manifest server started on port %d", constants.DEFAULT_MANIFEST_SERVER_PORT)
		log.Printf("Static server started on port %d", constants.DEFAULT_GATSBY_PORT)

		mode, _ := cmd.Flags().GetString("mode")
		if mode == "serve" {
			// Server functionality (API use case)
			server.WatchForManifestFiles()
		} else {
			// CLI functionality
			if err := cli.Execute(cmd); err != nil {
				log.Fatalf("CLI execution failed: %v", err)
			}
		}
	},
}

// Setup logging with rotatable file output by default
func setupLogging(cmd *cobra.Command) {
	verbose, _ := cmd.Flags().GetBool("verbose")

	// Set log level
	if verbose {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}

	// Get executable directory for log file location
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("Error getting executable path: %v", err)
	}
	execDir := filepath.Dir(execPath)
	logPath := filepath.Join(execDir, "codevideo.log")

	// Setup rotating log file
	logRotate := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    10, // MB
		MaxBackups: 5,
		MaxAge:     30, // days
		Compress:   true,
	}

	// Write to both file and stdout
	multiWriter := io.MultiWriter(os.Stdout, logRotate)
	log.SetOutput(multiWriter)

	if verbose {
		log.Printf("/> CodeVideo CLI v%s - verbose logging enabled, logs saved to: %s", version, logPath)
	} else {
		log.Printf("/> CodeVideo CLI v%s - logs saved to: %s", version, logPath)
	}
}

func init() {
	// --mode or -m flag for running in server mode
	rootCmd.Flags().StringP("mode", "m", "", "Run mode (use 'serve' for file watcher mode)")

	// --project or -p flag for specifying project JSON data
	rootCmd.Flags().StringP("project", "p", "", "Project data (Actions, Lesson, or Course) in JSON format")

	// --output or -o flag for specifying output file path
	rootCmd.Flags().StringP("output", "o", "", "Output file path")

	// --orientation or -n flag for specifying video orientation
	rootCmd.Flags().StringP("orientation", "n", "landscape", "Video orientation (landscape or portrait)")

	// --resolution or -r flag for specifying video resolution
	rootCmd.Flags().StringP("resolution", "r", "1080p", "Video resolution (1080p or 4K)")

	// --verbose or -v flag for verbose output
	rootCmd.Flags().BoolP("verbose", "v", false, "Verbose output")

	// --version or -V flag for displaying version
	rootCmd.Flags().BoolP("version", "V", false, "Display version information")

	// --open flag for opening the generated MP4 file
	rootCmd.Flags().Bool("open", false, "Open the generated MP4 file when complete")

	// --config or -c flag for specifying config JSON file path
	rootCmd.Flags().StringP("config", "c", "", "Path to config JSON file")
}

func main() {
	// Get the executable path
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("Error getting executable path: %v", err)
	}

	// Get the directory of the executable
	execDir := filepath.Dir(execPath)

	// Try to load .env from the executable directory
	if err := godotenv.Load(filepath.Join(execDir, ".env")); err != nil {
		// Fall back to .env.example if .env is not found
		if err := godotenv.Load(filepath.Join(execDir, ".env.example")); err != nil {
			log.Printf("Warning: No .env or .env.example file found in %s", execDir)
		}
	}

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
