package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	log "github.com/sirupsen/logrus"

	"github.com/codevideo/codevideo-cli/cli/config"
	"github.com/codevideo/codevideo-cli/cli/detector"
	"github.com/codevideo/codevideo-cli/cli/generator"
	"github.com/codevideo/codevideo-cli/cli/progress"
	"github.com/codevideo/codevideo-cli/cli/renderer"
	"github.com/codevideo/codevideo-cli/server"
	"github.com/codevideo/codevideo-cli/types"
	"github.com/codevideo/codevideo-cli/utils"
	"github.com/spf13/cobra"
)

// Execute runs the CLI workflow with the provided project data. Status lines
// and progress bars go to stderr; the saved videos are reported on stdout.
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
		log.Infof("Loaded config from: %s", config.GlobalConfig.ConfigFilePath)
	}

	// Create a context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	setupCancellation(ctx, cancel)

	log.Debugf("Analyzing project JSON: %s", projectJSON)
	course, lesson, actions, err := detector.DetectProjectType(projectJSON)
	if err != nil {
		return fmt.Errorf("failed to detect project type: %w", err)
	}

	generator := generator.NewGenerator()
	generator.RenderMode = config.GlobalConfig.RenderMode
	generator.RenderSeed = config.GlobalConfig.RenderSeed
	generator.RenderWorkers = config.GlobalConfig.RenderWorkers
	generator.RenderStrategy = config.GlobalConfig.RenderStrategy
	if ideProps != nil {
		generator.IDEProps = ideProps
	}

	outputPath, _ := cmd.Flags().GetString("output")

	var saved []string
	switch {
	case course != nil:
		saved, err = renderCourse(generator, *course, outputPath)
	case lesson != nil:
		saved, err = renderVideo("Lesson", generator, lesson.Actions, func() (*types.CodeVideoManifest, error) {
			return generator.GenerateFromLesson(*lesson)
		}, outputPath)
	case actions != nil:
		saved, err = renderVideo("Actions", generator, *actions, func() (*types.CodeVideoManifest, error) {
			return generator.GenerateFromActions(*actions)
		}, outputPath)
	}
	renderer.Stop()
	for _, path := range saved {
		fmt.Println("✅ CodeVideo successfully generated and saved to " + displayPath(path))
	}
	if err != nil {
		return err
	}

	// if the openFile (--open flag) was passed, open it!
	openFile, _ := cmd.Flags().GetBool("open")
	if openFile && len(saved) > 0 {
		if err := utils.OpenFile(saved[len(saved)-1]); err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
	}

	return nil
}

// renderVideo narrates and renders a lesson or a list of actions under one
// progress bar.
func renderVideo(kind string, g *generator.Generator, actions []types.Action, generate func() (*types.CodeVideoManifest, error), outputPath string) ([]string, error) {
	renderer.Status("Detected project type: " + kind)
	renderer.Status("/> CodeVideo generation in progress...")
	job := progress.NewJob("")
	planJob(job, g, actions)
	progress.SetCurrent(job)
	renderer.TrackJob(job)

	manifest, err := generate()
	if err != nil {
		return nil, err
	}
	saved, err := processManifest(g, manifest, outputPath)
	if err != nil {
		return nil, err
	}
	return []string{saved}, nil
}

// renderCourse shows an overall bar with one bar per lesson below it. Every
// lesson is narrated first, then each one is rendered in turn.
func renderCourse(g *generator.Generator, course types.Course, outputPath string) ([]string, error) {
	renderer.Status("Detected project type: Course")
	renderer.Status("/> CodeVideo generation in progress...")
	jobs := make([]*progress.Job, len(course.Lessons))
	for i, lesson := range course.Lessons {
		label := lesson.Name
		if label == "" {
			label = fmt.Sprintf("Lesson %d", i+1)
		}
		jobs[i] = progress.NewJob(label)
		planJob(jobs[i], g, lesson.Actions)
	}
	renderer.TrackCourse(progress.NewCourse(jobs))

	g.BeforeLesson = func(i int) { progress.SetCurrent(jobs[i]) }
	manifests, err := g.GenerateFromCourse(course)
	if err != nil {
		return nil, err
	}
	var saved []string
	for i, manifest := range manifests {
		progress.SetCurrent(jobs[i])
		path, err := processManifest(g, manifest, outputPath)
		if err != nil {
			return saved, fmt.Errorf("lesson %d: %w", i+1, err)
		}
		saved = append(saved, path)
	}
	return saved, nil
}

// planJob gives a job its first estimate, before any narration is measured.
func planJob(job *progress.Job, g *generator.Generator, actions []types.Action) {
	if clips := generator.CountSpeech(actions); clips > 0 {
		job.PlanNarration(clips)
	}
	server.PlanProgress(job, actions, g.IDEProps, g.RenderMode, g.RenderWorkers)
}

func processManifest(g *generator.Generator, manifest *types.CodeVideoManifest, outputPath string) (string, error) {
	manifestPath, err := g.SaveManifest(manifest)
	if err != nil {
		return "", fmt.Errorf("failed to save manifest: %w", err)
	}
	return server.ProcessJob(manifestPath, "cli", outputPath)
}

// displayPath shows a path relative to the working directory when it's inside it.
func displayPath(path string) string {
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return path
}

// setupCancellation clears the progress bars and exits on Ctrl-C. The Node
// runners and Chrome get the same signal from the terminal.
func setupCancellation(ctx context.Context, cancel context.CancelFunc) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		renderer.Stop()
		fmt.Fprintln(os.Stderr, "Cancelled")
		cancel()
		os.Exit(130)
	}()
}
