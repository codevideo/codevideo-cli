package utils

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/codevideo/codevideo-cli/cli/progress"
	log "github.com/sirupsen/logrus"
)

// ConvertToMp4 converts a given input file to an MP4 file with the specified output filename,
// starting start into the input (0 keeps all of it). It constructs the ffmpeg command with
// options to overwrite output (-y), use the input (-i), set the video codec, preset, quality
// level, frame rate, audio codec, and audio bitrate.
// Progress goes to the CLI's current job, if there is one. Cancelling ctx
// interrupts ffmpeg.
func ConvertToMp4(ctx context.Context, input, output string, start time.Duration) error {
	job := progress.Current()
	job.Begin(progress.Encoding)

	// Convert input and output to absolute paths if they aren't already
	inputAbs, err := filepath.Abs(input)
	if err != nil {
		return fmt.Errorf("failed to get absolute input path: %v", err)
	}

	// Check if input file exists
	if _, err := os.Stat(inputAbs); os.IsNotExist(err) {
		return fmt.Errorf("input file %s does not exist", inputAbs)
	}

	outputAbs, err := filepath.Abs(output)
	if err != nil {
		return fmt.Errorf("failed to get absolute output path: %v", err)
	}

	// Ensure the output directory exists
	outputDir := filepath.Dir(outputAbs)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %v", outputDir, err)
	}

	log.Debugf("Converting from %s to %s", inputAbs, outputAbs)

	ffmpegPath, err := ResolveFFmpegPath()
	if err != nil {
		return err
	}

	// The recording's length. Chrome's webm files have no duration in their
	// header, so read the last packet timestamp, or else use how long the
	// recording ran. The progress bar needs it, and it bounds start.
	duration := job.RecordedSeconds()
	if job != nil || start > 0 {
		if seconds, err := mediaSeconds(ctx, inputAbs); err == nil {
			duration = seconds
		} else {
			log.Debugf("Could not read the recording's length: %v", err)
		}
	}
	if start > 0 && duration > 0 && start.Seconds() >= duration {
		log.Debugf("Keeping the whole recording: it is %.1fs long, shorter than the %v lead-in", duration, start)
		start = 0
	}
	if start > 0 {
		duration -= start.Seconds()
	}

	// Construct the ffmpeg command with the -progress flag.
	cmd := CommandContext(ctx, ffmpegPath, mp4Args(inputAbs, outputAbs, start)...)

	log.Debugf("Executing command: %s", cmd.String())

	// Get a pipe to read ffmpeg's stdout.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %v", err)
	}
	// Without -nostats and the banner, stderr only holds stream info and errors.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	// Start the command.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %v", err)
	}

	var block ffmpegProgress
	scanner := bufio.NewScanner(stdoutPipe)
	for scanner.Scan() {
		if !block.read(scanner.Text()) {
			continue
		}
		fraction, remaining := block.estimate(duration)
		log.Debugf("ffmpeg: %.1fs of %.1fs encoded at %.2fx", block.outSeconds, duration, block.speed)
		job.SetFraction(fraction, remaining)
	}

	// Wait for the command to finish (after stdout is drained).
	if err := cmd.Wait(); err != nil {
		log.Debugf("ffmpeg stderr:\n%s", stderr.String())
		return fmt.Errorf("ffmpeg failed: %v: %s", err, lastLines(stderr.String(), 5))
	}
	if stderr.Len() > 0 {
		log.Debugf("ffmpeg stderr:\n%s", stderr.String())
	}
	return nil
}

// mp4Args is the ffmpeg command line that encodes input to output, starting
// start into the input.
func mp4Args(input, output string, start time.Duration) []string {
	args := []string{
		"-y",           // Overwrite output if exists
		"-hide_banner", // No build information on stderr
		"-nostats",     // Progress comes from -progress, not the stderr stats line
	}
	if start > 0 {
		// Seeking before -i is fast, and exact here because the video is re-encoded.
		args = append(args, "-ss", strconv.FormatFloat(start.Seconds(), 'f', 3, 64))
	}
	return append(args,
		"-i", input, // Input file (absolute path)
		"-c:v", "libx264", // Video codec
		"-preset", "fast", // Encoding preset
		"-crf", "18", // Quality level
		"-r", "60", // Frame rate
		"-c:a", "aac", // Audio codec
		"-b:a", "384k", // Audio bitrate
		"-progress", "pipe:1", // Send progress info to stdout
		output, // Output file (absolute path)
	)
}

// mediaSeconds returns the last video timestamp in a file by listing packet
// timestamps, which reads the file without decoding it.
func mediaSeconds(ctx context.Context, path string) (float64, error) {
	probe, err := ResolveFFprobePath()
	if err != nil {
		return 0, err
	}
	output, err := exec.CommandContext(ctx, probe, "-v", "error", "-select_streams", "v:0", "-show_entries", "packet=pts_time", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}
	last := 0.0
	for _, field := range strings.Fields(string(output)) {
		if seconds, err := strconv.ParseFloat(strings.TrimSuffix(field, ","), 64); err == nil && seconds > last {
			last = seconds
		}
	}
	if last <= 0 {
		return 0, fmt.Errorf("no video timestamps in %s", path)
	}
	return last, nil
}

// ffmpegProgress accumulates `-progress` output. ffmpeg writes key=value lines
// in blocks, each ending with progress=continue (or progress=end).
type ffmpegProgress struct {
	outSeconds float64 // position encoded so far
	speed      float64 // encoding speed as a multiple of real time
}

// read applies one line and reports whether it closed a block.
func (p *ffmpegProgress) read(line string) bool {
	key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
	if !ok {
		return false
	}
	switch key {
	case "out_time_us", "out_time_ms": // both are microseconds (ffmpeg's naming)
		if us, err := strconv.ParseFloat(value, 64); err == nil && us >= 0 {
			p.outSeconds = us / 1e6
		}
	case "speed":
		if x, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(value), "x"), 64); err == nil && x > 0 {
			p.speed = x
		}
	case "progress":
		return true
	}
	return false
}

// estimate returns the fraction encoded and the time left for an input of
// duration seconds; the time left is 0 (unknown) until ffmpeg reports a speed.
func (p ffmpegProgress) estimate(duration float64) (float64, time.Duration) {
	if duration <= 0 {
		return 0, 0
	}
	fraction := p.outSeconds / duration
	if fraction > 1 {
		fraction = 1
	}
	if p.speed <= 0 {
		return fraction, 0
	}
	left := (duration - p.outSeconds) / p.speed
	if left < 0 {
		left = 0
	}
	return fraction, time.Duration(left * float64(time.Second))
}

// lastLines returns the final n non-empty lines of s, joined by " | ".
func lastLines(s string, n int) string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

// ResolveFFmpegPath resolves the external FFmpeg executable without running it.
func ResolveFFmpegPath() (string, error) {
	ffmpegPath := os.Getenv("CODEVIDEO_FFMPEG_PATH")
	if ffmpegPath == "" {
		resolved, err := exec.LookPath("ffmpeg")
		if err != nil {
			return "", fmt.Errorf("ffmpeg is required: install it on PATH or set CODEVIDEO_FFMPEG_PATH")
		}
		return resolved, nil
	}
	if !filepath.IsAbs(ffmpegPath) {
		return "", fmt.Errorf("CODEVIDEO_FFMPEG_PATH must be absolute: %s", ffmpegPath)
	}
	info, err := os.Stat(ffmpegPath)
	if err != nil || info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0) {
		return "", fmt.Errorf("CODEVIDEO_FFMPEG_PATH is not an executable file: %s", ffmpegPath)
	}
	return ffmpegPath, nil
}
