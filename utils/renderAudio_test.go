package utils

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/codevideo/codevideo-cli/types"
)

func audioTools(t *testing.T) (string, context.Context) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg required for audio integration tests")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe required for audio integration tests")
	}
	t.Setenv("CODEVIDEO_FFMPEG_PATH", ffmpeg)
	t.Setenv("CODEVIDEO_FFPROBE_PATH", probe)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ffmpeg, ctx
}

func TestPrepareAndMuxRenderAudio(t *testing.T) {
	ffmpeg, ctx := audioTools(t)
	dir := t.TempDir()
	tone := filepath.Join(dir, "tone.wav")
	if output, err := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=1:sample_rate=48000", tone).CombinedOutput(); err != nil {
		t.Fatalf("tone: %v %s", err, output)
	}
	data, err := os.ReadFile(tone)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.Write(data) }))
	defer server.Close()
	actions := []types.Action{{Name: "author-speak-before", Value: "same"}, {Name: "author-speak-during", Value: "same"}, {Name: "author-speak-after", Value: "tail"}}
	index := 2
	manifest := &types.CodeVideoManifest{Actions: actions, AudioItems: []types.AudioItem{
		{Text: "same", Mp3Url: server.URL},
		{Text: "tail", ActionIndex: &index, Mp3Url: "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(data)},
	}}
	assets, err := PrepareRenderAudio(ctx, manifest, filepath.Join(dir, "assets"))
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || requests != 1 || len(manifest.AudioItems) != 3 {
		t.Fatalf("deduplication failed: %d assets, %d requests, %d cues", len(assets), requests, len(manifest.AudioItems))
	}
	for i, item := range manifest.AudioItems {
		if item.ActionIndex == nil || *item.ActionIndex != i || item.Mp3Url != "" || math.Abs(item.DurationMs-1000) > 0.01 {
			t.Fatalf("bad prepared item: %+v", item)
		}
	}
	silent := filepath.Join(dir, "silent.mp4")
	if output, err := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=black:s=64x64:r=60:d=4", "-c:v", "libx264", "-pix_fmt", "yuv420p", silent).CombinedOutput(); err != nil {
		t.Fatalf("video: %v %s", err, output)
	}
	trace := types.RenderTrace{Version: 1, RenderMode: types.RenderModeDeterministic, FPS: 60, FrameCount: 240, DurationMs: 4000}
	for i, start := range []float64{250, 750, 2000} {
		trace.SpeechCues = append(trace.SpeechCues, types.SpeechCue{ActionIndex: i, AssetID: manifest.AudioItems[i].AssetID, StartMs: start, DurationMs: 1000})
	}
	final := filepath.Join(dir, "final.mp4")
	if err := MuxRenderAudio(ctx, silent, final, trace, assets); err != nil {
		t.Fatal(err)
	}
	pcm, err := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-i", final, "-f", "f32le", "-ac", "1", "-ar", "48000", "pipe:1").Output()
	if err != nil {
		t.Fatal(err)
	}
	rms := func(start, end float64) float64 {
		sum := 0.0
		a, b := int(start*48000), int(end*48000)
		if len(pcm) < b*4 {
			t.Fatalf("audio tail truncated: %.3fs decoded, need %.3fs", float64(len(pcm))/4/48000, end)
		}
		for i := a; i < b; i++ {
			value := float64(math.Float32frombits(binary.LittleEndian.Uint32(pcm[i*4:])))
			sum += value * value
		}
		return math.Sqrt(sum / float64(b-a))
	}
	if rms(0, .20) > .001 || rms(.30, .60) < .03 || rms(.85, .95) < 1.6*rms(.30, .60) || rms(2.5, 2.9) < .03 || rms(3.2, 3.9) > .001 {
		t.Fatal("audio delay, overlapping mix or final tail was incorrect")
	}
	trace.SpeechCues[0].StartMs = 3500
	if err := MuxRenderAudio(ctx, silent, final, trace, assets); err == nil {
		t.Fatal("accepted truncated speech cue")
	}
}

func TestPrepareRenderAudioFailsMissingAndBadAssets(t *testing.T) {
	manifest := &types.CodeVideoManifest{Actions: []types.Action{{Name: "author-speak-before", Value: "missing"}}}
	if _, err := PrepareRenderAudio(context.Background(), manifest, t.TempDir()); err == nil {
		t.Fatal("missing audio accepted")
	}
	for _, source := range []string{"file:///private/audio.mp3", "data:audio/mp3,plain", "data:audio/mp3;base64,invalid!"} {
		if _, err := readRenderAudio(context.Background(), source); err == nil {
			t.Fatalf("accepted invalid source %q", source)
		}
	}
}
