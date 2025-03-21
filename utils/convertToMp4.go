package utils

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/codevideo/codevideo-cli/cli/renderer"
	log "github.com/sirupsen/logrus"
)

// ConvertToMp4 converts a given input file to an MP4 file with the specified output filename.
// It constructs the ffmpeg command with options to overwrite output (-y), use the input (-i),
// set the video codec, preset, quality level, frame rate, audio codec, and audio bitrate.
func ConvertToMp4(input, output string, mode string) error {
	renderer.RenderProgressToConsole(95, "Converting webm to mp4...")

	// Construct the ffmpeg command with the -progress flag.
	cmd := exec.Command("ffmpeg",
		"-y",        // Overwrite output if exists
		"-i", input, // Input file
		"-c:v", "libx264", // Video codec
		"-preset", "fast", // Encoding preset
		"-crf", "18", // Quality level
		"-r", "60", // Frame rate
		"-c:a", "aac", // Audio codec
		"-b:a", "384k", // Audio bitrate
		"-progress", "pipe:1", // Send progress info to stdout
		output, // Output file
	)

	// Get a pipe to read ffmpeg's stdout.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %v", err)
	}

	// Start the command.
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %v", err)
	}

	// Create a scanner to read stdout line by line.
	scanner := bufio.NewScanner(stdoutPipe)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			// Debug: Uncomment the next line to see raw progress output.
			log.Println("ffmpeg output:", line)

			// Check if the line contains a progress update.
			if strings.HasPrefix(line, "progress=") {
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					pStr := strings.TrimSpace(parts[1])
					// Parse the progress value.
					if ffmpegProgress, err := strconv.ParseFloat(pStr, 64); err == nil {
						// Normalize the ffmpeg progress (0-100) to overall progress (80-100).
						normalizedProgress := 80.0 + (ffmpegProgress/100.0)*20.0
						// Update the progress bar with the normalized progress.
						if mode == "cli" {
							renderer.RenderProgressToConsole(normalizedProgress, "Converting webm to mp4...")
						}
					}
				}
			}
		}
	}()

	// Wait for the command to finish.
	if err := cmd.Wait(); err != nil {
		log.Errorf("ffmpeg failed: %v", err)
		return err
	}

	renderer.RenderProgressToConsole(100, "")

	return nil
}
