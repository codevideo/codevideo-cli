package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

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

// ErrCancelled is returned when Ctrl-C or SIGTERM stopped the render.
var ErrCancelled = errors.New("cancelled")

// Execute runs the CLI workflow with the provided project data. Status lines
// and progress bars go to stderr; the saved videos are reported on stdout.
func Execute(cmd *cobra.Command) error {
	// Load configuration from flags
	if err := config.LoadFromFlags(cmd); err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	projectJSON, err := readProject(cmd)
	if err != nil {
		return err
	}
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

	var takes map[string]generator.AudioTake
	if takesPath, _ := cmd.Flags().GetString("audio-items"); takesPath != "" {
		takes, err = generator.LoadAudioTakes(takesPath)
		if err != nil {
			return err
		}
		log.Infof("Loaded %d narration takes from: %s", len(takes), takesPath)
	}

	ctx, stop := cancelOnSignal()
	defer stop()

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
	generator.AudioTakes = takes
	if ideProps != nil {
		generator.IDEProps = ideProps
	}

	outputPath, _ := cmd.Flags().GetString("output")

	var saved []string
	switch {
	case course != nil:
		saved, err = renderCourse(ctx, generator, *course, outputPath)
	case lesson != nil:
		saved, err = renderVideo(ctx, "Lesson", generator, lesson.Actions, func() (*types.CodeVideoManifest, error) {
			return generator.GenerateFromLesson(ctx, *lesson)
		}, outputPath)
	case actions != nil:
		saved, err = renderVideo(ctx, "Actions", generator, *actions, func() (*types.CodeVideoManifest, error) {
			return generator.GenerateFromActions(ctx, *actions)
		}, outputPath)
	}
	renderer.Stop()
	for i, path := range saved {
		fmt.Println("✅ CodeVideo successfully generated and saved to " + displayPath(path))
		number := 0 // a course's results name their lesson
		if course != nil {
			number = i + 1
		}
		renderer.Result(path, number)
	}
	if err != nil && ctx.Err() != nil {
		// The runner, ffmpeg or a TTS request failed because the signal stopped it.
		return ErrCancelled
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
func renderVideo(ctx context.Context, kind string, g *generator.Generator, actions []types.Action, generate func() (*types.CodeVideoManifest, error), outputPath string) ([]string, error) {
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
	saved, err := processManifest(ctx, g, manifest, outputPath)
	if err != nil {
		return nil, err
	}
	return []string{saved}, nil
}

// renderCourse shows an overall bar with one bar per lesson below it. Every
// lesson is narrated first, then each one is rendered in turn.
func renderCourse(ctx context.Context, g *generator.Generator, course types.Course, outputPath string) ([]string, error) {
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
	manifests, err := g.GenerateFromCourse(ctx, course)
	if err != nil {
		return nil, err
	}
	var saved []string
	for i, manifest := range manifests {
		progress.SetCurrent(jobs[i])
		path, err := processManifest(ctx, g, manifest, outputPath)
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

func processManifest(ctx context.Context, g *generator.Generator, manifest *types.CodeVideoManifest, outputPath string) (string, error) {
	manifestPath, err := g.SaveManifest(manifest)
	if err != nil {
		return "", fmt.Errorf("failed to save manifest: %w", err)
	}
	return server.ProcessJob(ctx, manifestPath, "cli", outputPath)
}

// readProject returns the project JSON from -p, or from the file that
// --project-file names. If -p refers to an existing file, it is read directly.
// A file has no size limit; one argument does (128 KiB on Linux, and about 32K
// characters for a whole Windows command line).
func readProject(cmd *cobra.Command) (string, error) {
	path, _ := cmd.Flags().GetString("project-file")
	if path == "" {
		project, _ := cmd.Flags().GetString("project")
		if project == "" {
			return "", nil
		}
		// If -p is a path to an existing file, read the file contents directly!
		if info, err := os.Stat(project); err == nil && !info.IsDir() {
			data, err := os.ReadFile(project)
			if err != nil {
				return "", fmt.Errorf("reading project file %s: %w", project, err)
			}
			if strings.TrimSpace(string(data)) == "" {
				return "", fmt.Errorf("project file %s is empty", project)
			}
			return string(data), nil
		}
		return project, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading --project-file: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf("--project-file %s is empty", path)
	}
	return string(data), nil
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

// cancelGrace is how long a cancelled render gets to wind down before the
// CLI exits anyway. It outlasts utils.StopDelay, after which a runner or
// ffmpeg that ignored the interrupt has been killed.
const cancelGrace = utils.StopDelay + 5*time.Second

// cancelOnSignal returns a context that Ctrl-C or SIGTERM cancels, and a
// function that stops listening. The render's child processes run under the
// context, so the signal reaches them even when it was sent to the CLI alone:
// the Node runners close Chrome, and ffmpeg finishes its file. Execute then
// returns ErrCancelled. A second signal, or a render still running after
// cancelGrace, exits at once.
func cancelOnSignal() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-signals:
		case <-done:
			return
		}
		log.Debug("Cancelling the render")
		cancel()
		select {
		case <-signals:
		case <-time.After(cancelGrace):
		case <-done:
			return
		}
		renderer.Stop()
		renderer.Cancelled()
		os.Exit(130)
	}()
	return ctx, func() {
		signal.Stop(signals)
		close(done)
		cancel()
	}
}
