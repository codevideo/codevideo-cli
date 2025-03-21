package main

import (
	"context"

	log "github.com/sirupsen/logrus"

	"github.com/codevideo/codevideo-cli/cli"
	"github.com/codevideo/codevideo-cli/cli/staticserver"
	"github.com/codevideo/codevideo-cli/constants"
	"github.com/codevideo/codevideo-cli/server"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "codevideo",
	Short: "CodeVideo's CLI tool",
	Long:  `CodeVideo's CLI tool for processing video jobs.`,
	Run: func(cmd *cobra.Command, args []string) {
		// apply logging level before anything
		verbose, _ := cmd.Flags().GetBool("verbose")
		if verbose {
			log.SetLevel(log.DebugLevel)
			log.Printf("CodeVideo CLI v0.0.1 - verbose logging enabled")
		} else {
			log.SetLevel(log.ErrorLevel)
		}

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

func init() {
	// --mode or -m flag for running in server mode
	rootCmd.Flags().StringP("mode", "m", "", "Run mode (use 'serve' for file watcher mode)")

	// --project or -p flag for specifying project JSON data
	rootCmd.Flags().StringP("project", "p", "", "Project data (Actions, Lesson, or Course) in JSON format")

	// --output or -o flag for specifying output file path
	rootCmd.Flags().StringP("output", "o", "", "Output file path")

	// --resolution or -r flag for specifying video resolution
	rootCmd.Flags().StringP("resolution", "r", "1080p", "Video resolution (1080p or 4K)")

	// --verbose or -v flag for verbose output
	rootCmd.Flags().BoolP("verbose", "v", false, "Verbose output")
}

func main() {
	// Load environment variables from .env file.
	if err := godotenv.Load(); err != nil {
		log.Fatal("Error loading .env file")
	}

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
