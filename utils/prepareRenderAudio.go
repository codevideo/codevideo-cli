package utils

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/codevideo/codevideo-cli/types"
)

type RenderAudioAsset struct {
	Path       string
	DurationMs float64
}

func ResolveFFprobePath() (string, error) {
	if p := os.Getenv("CODEVIDEO_FFPROBE_PATH"); p != "" {
		info, err := os.Stat(p)
		if !filepath.IsAbs(p) || err != nil || info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0) {
			return "", fmt.Errorf("CODEVIDEO_FFPROBE_PATH is not an absolute executable file: %s", p)
		}
		return p, nil
	}
	if ffmpeg := os.Getenv("CODEVIDEO_FFMPEG_PATH"); ffmpeg != "" {
		name := "ffprobe"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		candidate := filepath.Join(filepath.Dir(ffmpeg), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	p, err := exec.LookPath("ffprobe")
	if err != nil {
		return "", fmt.Errorf("ffprobe is required for deterministic speech; install it or set CODEVIDEO_FFPROBE_PATH")
	}
	return p, nil
}

// ProbeDurationMs measures a media file's duration with ffprobe.
func ProbeDurationMs(ctx context.Context, path string) (float64, error) {
	probe, err := ResolveFFprobePath()
	if err != nil {
		return 0, err
	}
	output, err := exec.CommandContext(ctx, probe, "-v", "error", "-show_entries", "format=duration", "-of", "json", path).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}
	var result struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return 0, err
	}
	seconds, err := strconv.ParseFloat(result.Format.Duration, 64)
	if err != nil || seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, fmt.Errorf("invalid duration %q", result.Format.Duration)
	}
	return seconds * 1000, nil
}

// AudioDurationMs measures encoded audio held in memory.
func AudioDurationMs(ctx context.Context, data []byte) (float64, error) {
	file, err := os.CreateTemp("", "codevideo-audio-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return 0, err
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	return ProbeDurationMs(ctx, file.Name())
}

func readRenderAudio(ctx context.Context, source string) ([]byte, error) {
	const limit = 64 * 1024 * 1024
	if strings.HasPrefix(source, "data:") {
		parts := strings.SplitN(source, ",", 2)
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") || len(parts[1]) > limit*4/3+4 {
			return nil, fmt.Errorf("invalid or oversized audio data URI")
		}
		return base64.StdEncoding.DecodeString(parts[1])
	}
	parsed, err := url.Parse(source)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, fmt.Errorf("audio must be an HTTP(S) URL or base64 data URI")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", source, nil)
	if err != nil {
		return nil, err
	}
	response, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("audio download failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("audio download returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if len(data) > limit {
		return nil, fmt.Errorf("audio exceeds 64 MiB")
	}
	return data, err
}
func PrepareRenderAudio(ctx context.Context, manifest *types.CodeVideoManifest, directory string) (map[string]RenderAudioAsset, error) {
	actions := manifest.Actions
	if len(actions) == 0 {
		actions = manifest.Lesson.Actions
	}
	assets := map[string]RenderAudioAsset{}
	items := []types.AudioItem{}
	sourceIDs := map[string]string{}
	for index, action := range actions {
		if !strings.HasPrefix(action.Name, "author-speak-") {
			continue
		}
		var selected *types.AudioItem
		for i := range manifest.AudioItems {
			item := &manifest.AudioItems[i]
			if item.ActionIndex != nil && *item.ActionIndex == index {
				selected = item
				break
			}
		}
		if selected == nil {
			for i := range manifest.AudioItems {
				item := &manifest.AudioItems[i]
				if item.ActionIndex == nil && item.Text == action.Value {
					selected = item
					break
				}
			}
		}
		if selected == nil {
			return nil, fmt.Errorf("missing speech audio for action %d", index)
		}
		assetID := sourceIDs[selected.Mp3Url]
		if assetID == "" {
			data, err := readRenderAudio(ctx, selected.Mp3Url)
			if err != nil {
				return nil, fmt.Errorf("speech action %d: %w", index, err)
			}
			digest := sha256.Sum256(data)
			assetID = hex.EncodeToString(digest[:])
			if _, exists := assets[assetID]; !exists {
				if err := os.MkdirAll(directory, 0755); err != nil {
					return nil, err
				}
				audioPath := filepath.Join(directory, assetID+".audio")
				if err := os.WriteFile(audioPath, data, 0600); err != nil {
					return nil, err
				}
				durationMs, err := ProbeDurationMs(ctx, audioPath)
				if err != nil {
					return nil, fmt.Errorf("speech action %d: %w", index, err)
				}
				assets[assetID] = RenderAudioAsset{Path: audioPath, DurationMs: durationMs}
			}
			sourceIDs[selected.Mp3Url] = assetID
		}
		// Browser needs timing and identity only. Never fetch or play the asset there.
		items = append(items, types.AudioItem{Text: action.Value, Mp3Url: "", ActionIndex: &index, AssetID: assetID, DurationMs: assets[assetID].DurationMs})
	}
	manifest.AudioItems = items
	return assets, nil
}
