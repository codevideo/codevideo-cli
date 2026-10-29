package utils

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/codevideo/codevideo-cli/types"
)

func MuxRenderAudio(ctx context.Context, silent, output string, trace types.RenderTrace, assets map[string]RenderAudioAsset) error {
	if trace.Version != 1 || trace.RenderMode != types.RenderModeDeterministic || trace.FPS != 60 || trace.FrameCount <= 0 || math.IsNaN(trace.DurationMs) || math.IsInf(trace.DurationMs, 0) || math.Abs(trace.DurationMs-float64(trace.FrameCount)*1000/60) > 0.01 {
		return fmt.Errorf("invalid deterministic render trace")
	}
	if len(trace.SpeechCues) == 0 {
		return CopyFile(silent, output)
	}
	ffmpeg, err := ResolveFFmpegPath()
	if err != nil {
		return err
	}
	// A single filter thread makes delayed/overlapping streams stable and avoids
	// excessive filter scheduling on machines with many cores.
	args := []string{"-y", "-nostdin", "-v", "error", "-filter_complex_threads", "1", "-i", silent}
	duration := strconv.FormatFloat(trace.DurationMs/1000, 'f', 6, 64)
	// A finite silence bed defines the audio timeline even when the final cue
	// ends early. This avoids relying on EOF-driven infinite padding.
	filters := []string{fmt.Sprintf("anullsrc=r=48000:cl=stereo:d=%s[silence]", duration)}
	labels := "[silence]"
	for i, cue := range trace.SpeechCues {
		asset, ok := assets[cue.AssetID]
		if !ok || cue.StartMs < 0 || math.IsNaN(cue.StartMs) || math.IsInf(cue.StartMs, 0) || cue.DurationMs <= 0 || math.IsNaN(cue.DurationMs) || math.IsInf(cue.DurationMs, 0) || math.Abs(asset.DurationMs-cue.DurationMs) > 0.01 || cue.StartMs+cue.DurationMs > trace.DurationMs+0.01 {
			return fmt.Errorf("invalid speech cue at action %d", cue.ActionIndex)
		}
		args = append(args, "-i", asset.Path)
		samples := int64(math.Round(cue.StartMs * 48))
		filters = append(filters, fmt.Sprintf("[%d:a]aresample=48000,aformat=channel_layouts=stereo,adelay=%dS:all=1[a%d]", i+1, samples, i))
		labels += fmt.Sprintf("[a%d]", i)
	}
	filters = append(filters, fmt.Sprintf("%samix=inputs=%d:duration=first:normalize=0,alimiter=limit=0.95:level=0:latency=1,atrim=duration=%s[audio]", labels, len(trace.SpeechCues)+1, duration))
	args = append(args, "-filter_complex", strings.Join(filters, ";"), "-map", "0:v:0", "-map", "[audio]", "-c:v", "copy", "-c:a", "aac", "-b:a", "384k", "-movflags", "+faststart", output)
	if stderr, err := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("audio mux failed: %w: %s", err, stderr)
	}
	return nil
}
