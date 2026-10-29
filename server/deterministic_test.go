package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/codevideo/codevideo-cli/cli/config"
	"github.com/codevideo/codevideo-cli/cli/staticserver"
	"github.com/codevideo/codevideo-cli/types"
)

// Opt-in because this launches Chrome and binds the embedded application's ports.
// CODEVIDEO_BROWSER_TESTS=1 go test ./server -run TestDeterministicSpeechRender -v
func TestDeterministicSpeechRender(t *testing.T) {
	if os.Getenv("CODEVIDEO_BROWSER_TESTS") != "1" {
		t.Skip("set CODEVIDEO_BROWSER_TESTS=1 for the full browser/audio integration")
	}
	dir := t.TempDir()
	runner, err := filepath.Abs("../puppeteer-runner/recordVideoV3.js")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEVIDEO_PUPPETEER_RUNNER_PATH", runner)
	t.Setenv("CODEVIDEO_WORK_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "video"), 0755); err != nil {
		t.Fatal(err)
	}
	previous := config.GlobalConfig
	config.GlobalConfig = config.DefaultConfig()
	t.Cleanup(func() { config.GlobalConfig = previous })
	srv, err := staticserver.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	items := []types.AudioItem{}
	for _, duration := range []string{"1", "3"} {
		file := filepath.Join(dir, duration+".wav")
		if output, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration="+duration, file).CombinedOutput(); err != nil {
			t.Fatalf("tone: %v %s", err, output)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, types.AudioItem{Text: duration, Mp3Url: "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(data)})
	}
	manifest := &types.CodeVideoManifest{UUID: "speech-fixture", RenderWorkers: 4, RenderStrategy: types.RenderStrategyAbInitio, AudioItems: items, Actions: []types.Action{
		{Name: "author-speak-before", Value: "1"},
		{Name: "author-speak-during", Value: "3"},
		{Name: "author-speak-after", Value: "1"},
		{Name: "author-speak-during", Value: "3"},
	}}
	manifestPath := filepath.Join(dir, "manifest.json")
	raw, _ := json.Marshal(manifest)
	if err := os.WriteFile(manifestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "speech.mp4")
	if err := processDeterministicJob(manifestPath, manifest, output); err != nil {
		t.Fatal(err)
	}
	var trace struct {
		types.RenderTrace
		Actions []struct {
			StartMs float64 `json:"startMs"`
			EndMs   float64 `json:"endMs"`
		} `json:"actions"`
	}
	raw, err = os.ReadFile(filepath.Join(dir, "speech.render-trace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &trace); err != nil {
		t.Fatal(err)
	}
	if len(trace.SpeechCues) != 4 || len(trace.Actions) != 4 || trace.RenderSeed == "" {
		t.Fatalf("incomplete trace: %s", raw)
	}
	for _, i := range []int{0, 2} {
		if math.Abs(trace.Actions[i].EndMs-trace.SpeechCues[i].StartMs-1000) > .01 {
			t.Fatalf("blocking cue %d did not wait", i)
		}
	}
	for _, i := range []int{1, 3} {
		if math.Abs(trace.Actions[i].EndMs-trace.SpeechCues[i].StartMs) > .01 {
			t.Fatalf("during cue %d blocked", i)
		}
	}
	last := trace.SpeechCues[3]
	if trace.DurationMs < last.StartMs+last.DurationMs+1000 || trace.DurationMs > last.StartMs+last.DurationMs+1017 {
		t.Fatal("final speech tail/hold missing")
	}
	var saved types.CodeVideoManifest
	raw, _ = os.ReadFile(manifestPath)
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.RenderSeed != trace.RenderSeed || saved.RenderMode != types.RenderModeDeterministic || saved.RenderStrategy != types.RenderStrategyAbInitio {
		t.Fatal("resolved mode/seed not persisted")
	}
	var metrics struct {
		Strategy        string `json:"strategy"`
		Workers         int    `json:"workers"`
		EvaluatedFrames int    `json:"evaluatedFrames"`
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "speech.render-metrics.json"))
	if err := json.Unmarshal(raw, &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics.Strategy != "ab-initio" || metrics.Workers < 2 || metrics.EvaluatedFrames != trace.FrameCount {
		t.Fatalf("speech did not use checkpoint segments: %s", raw)
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatal(err)
	}
	raw, err = exec.Command(probe, "-v", "error", "-show_entries", "stream=codec_type,codec_name,duration,nb_frames", "-of", "json", output).Output()
	if err != nil {
		t.Fatal(err)
	}
	var media struct {
		Streams []struct {
			Type     string `json:"codec_type"`
			Codec    string `json:"codec_name"`
			Duration string `json:"duration"`
			Frames   string `json:"nb_frames"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(raw, &media); err != nil {
		t.Fatal(err)
	}
	if len(media.Streams) != 2 || media.Streams[0].Codec != "h264" || media.Streams[1].Codec != "aac" {
		t.Fatalf("unexpected media: %s", raw)
	}
	frames, err := strconv.Atoi(media.Streams[0].Frames)
	if err != nil || frames != trace.FrameCount {
		t.Fatal("encoded frame count differs from trace")
	}
	audioSeconds, err := strconv.ParseFloat(media.Streams[1].Duration, 64)
	if err != nil || math.Abs(audioSeconds*1000-trace.DurationMs) > 25 {
		t.Fatal("muxed audio duration differs from trace")
	}
	t.Logf("validated speech cues, blocking/nonblocking timing, persisted seed and MP4 streams: %s", raw)
}
