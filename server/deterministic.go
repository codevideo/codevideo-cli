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
	"time"

	"github.com/codevideo/codevideo-cli/cli/config"
	"github.com/codevideo/codevideo-cli/cli/progress"
	"github.com/codevideo/codevideo-cli/constants"
	"github.com/codevideo/codevideo-cli/types"
	"github.com/codevideo/codevideo-cli/utils"
	"github.com/google/uuid"
)

func processDeterministicJob(manifestPath string, manifest *types.CodeVideoManifest, output string) error {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	manifest.RenderMode = types.RenderModeDeterministic
	strategy, strategyErr := types.ResolveRenderStrategy(manifest.RenderStrategy)
	if strategyErr != nil {
		return strategyErr
	}
	manifest.RenderStrategy = strategy
	if manifest.RenderSeed == "" {
		manifest.RenderSeed = uuid.NewString()
	}
	if manifest.RenderWorkers == 0 {
		manifest.RenderWorkers = 1
	}
	if manifest.RenderWorkers < 1 || manifest.RenderWorkers > 32 {
		return fmt.Errorf("renderWorkers must be between 1 and 32")
	}
	// Persist resolved options without dropping unknown producer fields. In-place
	// writes do not create a second job in the create-event filesystem queue.
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var original map[string]json.RawMessage
	if err := json.Unmarshal(raw, &original); err != nil {
		return err
	}
	original["renderStrategy"], _ = json.Marshal(manifest.RenderStrategy)
	original["renderMode"], _ = json.Marshal(manifest.RenderMode)
	original["renderSeed"], _ = json.Marshal(manifest.RenderSeed)
	original["renderWorkers"], _ = json.Marshal(manifest.RenderWorkers)
	raw, err = json.MarshalIndent(original, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifestPath, raw, 0600); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(constants.VideoFolder(), "deterministic-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	assets, err := utils.PrepareRenderAudio(ctx, manifest, filepath.Join(temp, "audio"))
	if err != nil {
		return err
	}
	preparedAt := time.Now()
	preparedPath := filepath.Join(temp, "manifest.json")
	// Preserve producer-specific IDE props and snapshot fields just as the
	// realtime runner does; only replace audio with prepared timing metadata.
	original["audioItems"], err = json.Marshal(manifest.AudioItems)
	if err != nil {
		return err
	}
	raw, err = json.Marshal(original)
	if err != nil {
		return err
	}
	if err := os.WriteFile(preparedPath, raw, 0600); err != nil {
		return err
	}
	stem := strings.TrimSuffix(output, filepath.Ext(output))
	tracePath, metricsPath := stem+".render-trace.json", stem+".render-metrics.json"
	silent := filepath.Join(temp, "silent.mp4")
	args := []string{constants.DeterministicRunnerPath(), "--manifest-path", preparedPath, "--output-mp4", silent, "--trace-path", tracePath, "--metrics-path", metricsPath, "--resolution", config.GlobalConfig.Resolution, "--orientation", jobOrientation(manifest), "--workers", strconv.Itoa(manifest.RenderWorkers), "--strategy", string(manifest.RenderStrategy), "--progress"}
	if config.GlobalConfig.Headed {
		args = append(args, "--debug")
	}
	command := exec.CommandContext(ctx, "node", args...)
	// Let the Node runner close browsers and encoders before forcing termination.
	command.Cancel = func() error {
		if err := command.Process.Signal(os.Interrupt); err != nil {
			return command.Process.Kill()
		}
		return nil
	}
	command.WaitDelay = 10 * time.Second
	job := progress.Current()
	if err := runRunner(command, "Deterministic renderer", job); err != nil {
		return fmt.Errorf("deterministic renderer failed: %w", err)
	}
	var trace types.RenderTrace
	raw, err = os.ReadFile(tracePath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &trace); err != nil {
		return err
	}
	muxStarted := time.Now()
	job.Begin(progress.Encoding)
	final := filepath.Join(temp, "final.mp4")
	if err := utils.MuxRenderAudio(ctx, silent, final, trace, assets); err != nil {
		return err
	}
	if err := utils.CopyFile(final, output); err != nil {
		return err
	}
	raw, err = os.ReadFile(metricsPath)
	if err != nil {
		return err
	}
	var metrics map[string]interface{}
	if err := json.Unmarshal(raw, &metrics); err != nil {
		return err
	}
	metrics["audioPreparationMs"] = float64(preparedAt.Sub(started).Microseconds()) / 1000
	metrics["muxMs"] = float64(time.Since(muxStarted).Microseconds()) / 1000
	metrics["jobMs"] = float64(time.Since(started).Microseconds()) / 1000
	raw, err = json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metricsPath, raw, 0644)
}
