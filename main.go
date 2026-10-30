package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/codevideo/codevideo-cli/cli"
	"github.com/codevideo/codevideo-cli/cli/notify"
	"github.com/codevideo/codevideo-cli/cli/renderer"
	"github.com/codevideo/codevideo-cli/cli/staticserver"
	"github.com/codevideo/codevideo-cli/constants"
	"github.com/codevideo/codevideo-cli/server"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

// version is injected by release builds with -ldflags "-X main.version=...".
var version = "dev"

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

		mode, _ := cmd.Flags().GetString("mode")
		serve := mode == "serve"

		// Setup logging configuration
		logPath, err := setupLogging(cmd, serve)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		slackWebhookURL, _ := cmd.Flags().GetString("slack-webhook-url")
		notify.Configure(serve, slackWebhookURL)
		renderer.OnMilestone(func(m renderer.Milestone) { notify.Queue(m.Text()) })

		// for either CLI or server mode, we need to start the required servers:
		// start static server for the built gatsby files (7001) and the manifest files it needs (7000)
		ctx := context.Background()
		srv, err := staticserver.Start(ctx)
		if err != nil {
			log.Fatalf("Error starting static server: %v", err)
		}
		defer srv.Stop()
		if srv.ManifestServerStarted() {
			log.Debugf("Manifest server started on port %d", constants.DEFAULT_MANIFEST_SERVER_PORT)
		}
		log.Debugf("Static server started on port %d", constants.DEFAULT_GATSBY_PORT)

		if serve {
			// Server functionality (API use case)
			server.WatchForManifestFiles()
			return
		}
		// CLI functionality
		err = cli.Execute(cmd)
		notify.Flush(10 * time.Second)
		if err != nil {
			log.Errorf("%v", err)
			fmt.Fprintf(os.Stderr, "See the log for details: %s\n", logPath)
			srv.Stop()
			os.Exit(1)
		}
	},
}

// setupLogging writes every log line to a rotating log file. Serve mode also
// writes them to stdout for the worker's container logs. The CLI keeps the
// terminal for its progress bar: warnings and errors print above it, -v adds
// info lines, --log-level picks any level, and -q leaves errors only.
func setupLogging(cmd *cobra.Command, serve bool) (string, error) {
	level, err := consoleLevel(cmd, serve)
	if err != nil {
		return "", err
	}

	logDir := constants.LogFolder()
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return "", fmt.Errorf("creating log directory %s: %w", logDir, err)
	}
	logPath := filepath.Join(logDir, "codevideo.log")

	// Setup rotating log file
	logRotate := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    10, // MB
		MaxBackups: 5,
		MaxAge:     30, // days
		Compress:   true,
	}

	if serve {
		// Write to both file and stdout
		log.SetOutput(io.MultiWriter(os.Stdout, logRotate))
		log.SetLevel(level)
		log.Printf("/> CodeVideo CLI v%s - logs saved to: %s", version, logPath)
		return logPath, nil
	}

	quiet, _ := cmd.Flags().GetBool("quiet")
	renderer.Configure(os.Stderr, quiet)
	log.SetOutput(logRotate)
	log.SetLevel(max(level, log.DebugLevel)) // the file gets everything
	log.AddHook(&consoleHook{level: level})
	log.RegisterExitHandler(renderer.Stop)
	log.Debugf("/> CodeVideo CLI v%s", version)
	log.Infof("Logs saved to: %s", logPath)
	return logPath, nil
}

// consoleLevel is the most detailed log level shown in the terminal (serve
// mode: on stdout).
func consoleLevel(cmd *cobra.Command, serve bool) (log.Level, error) {
	if name, _ := cmd.Flags().GetString("log-level"); name != "" {
		level, err := log.ParseLevel(name)
		if err != nil {
			return 0, fmt.Errorf("invalid --log-level %q: use error, warn, info, debug or trace", name)
		}
		return level, nil
	}
	verbose, _ := cmd.Flags().GetBool("verbose")
	quiet, _ := cmd.Flags().GetBool("quiet")
	switch {
	case serve:
		// The worker's container logs keep runner and ffmpeg output.
		return log.DebugLevel, nil
	case quiet:
		return log.ErrorLevel, nil
	case verbose:
		return log.InfoLevel, nil
	}
	return log.WarnLevel, nil
}

// consoleHook prints log lines at or above a level above the progress bar.
// Info lines print as they are; other levels are prefixed ("warning: ...").
type consoleHook struct{ level log.Level }

func (h *consoleHook) Levels() []log.Level { return log.AllLevels[:h.level+1] }

func (h *consoleHook) Fire(entry *log.Entry) error {
	renderer.Println(consoleLine(entry.Level, entry.Message))
	return nil
}

func consoleLine(level log.Level, message string) string {
	message = strings.TrimRight(message, "\n")
	if level == log.InfoLevel {
		return message
	}
	return level.String() + ": " + message
}

func init() {
	addFlags(rootCmd)
}

func addFlags(cmd *cobra.Command) {
	cmd.Flags().String("render-mode", "realtime", "Render mode (realtime or deterministic)")
	cmd.Flags().String("render-seed", "", "Stable seed for reproducible deterministic renders")
	cmd.Flags().String("render-strategy", "sequential-seek", "Deterministic strategy (sequential-seek or ab-initio)")
	cmd.Flags().Int("render-workers", 1, "Deterministic browser workers (1-32)")
	// --mode or -m flag for running in server mode
	cmd.Flags().StringP("mode", "m", "", "Run mode (use 'serve' for file watcher mode)")

	// --project or -p flag for specifying project JSON data
	cmd.Flags().StringP("project", "p", "", "Project data (Actions, Lesson, or Course) in JSON format")

	// --output or -o flag for specifying output file path
	cmd.Flags().StringP("output", "o", "", "Output file path")

	// --orientation or -n flag for specifying video orientation
	cmd.Flags().StringP("orientation", "n", "landscape", "Video orientation (landscape or portrait)")

	// --resolution or -r flag for specifying video resolution
	cmd.Flags().StringP("resolution", "r", "1080p", "Video resolution (1080p or 4K)")

	// Output: by default a progress bar, warnings and errors on stderr, and the saved video on stdout
	cmd.Flags().BoolP("verbose", "v", false, "Also show info log lines above the progress bar")
	cmd.Flags().BoolP("quiet", "q", false, "Hide the progress bar and status lines; show errors only")
	cmd.MarkFlagsMutuallyExclusive("verbose", "quiet")
	cmd.Flags().String("log-level", "", "Most detailed log level to show: error, warn, info, debug or trace (the log file always gets debug)")
	cmd.Flags().String("slack-webhook-url", "", "Post progress to this Slack incoming webhook at each stage and every 25%")

	// --version or -V flag for displaying version
	cmd.Flags().BoolP("version", "V", false, "Display version information")

	// --open flag for opening the generated MP4 file
	cmd.Flags().Bool("open", false, "Open the generated MP4 file when complete")

	// --config or -c flag for specifying config JSON file path
	cmd.Flags().StringP("config", "c", "", "Path to config JSON file")

	// --headed shows the browser window instead of running it headless
	cmd.Flags().Bool("headed", false, "Show the browser window instead of running it headless")
	cmd.Flags().BoolP("debug", "d", false, "Show the browser window instead of running it headless")
	cmd.Flags().MarkDeprecated("debug", "use --headed instead")
}

func main() {
	// Get the executable path
	execPath, err := os.Executable()
	if err != nil {
		log.Fatalf("Error getting executable path: %v", err)
	}

	// Get the directory of the executable
	execDir := filepath.Dir(execPath)

	if configured := os.Getenv("CODEVIDEO_ENV_FILE"); configured != "" {
		if err := godotenv.Load(configured); err != nil {
			log.Warnf("failed to load CODEVIDEO_ENV_FILE %s: %v", configured, err)
		}
	} else if err := godotenv.Load(filepath.Join(execDir, ".env")); err != nil {
		// Preserve the source-install fallback without warning for npm installs.
		_ = godotenv.Load(filepath.Join(execDir, ".env.example"))
	}

	// Execute the root command; cobra prints its own errors (unknown flags)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
