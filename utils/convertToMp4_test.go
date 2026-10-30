package utils

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolveFFmpegPathOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable fixture uses Unix permissions")
	}
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEVIDEO_FFMPEG_PATH", ffmpeg)
	got, err := ResolveFFmpegPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != ffmpeg {
		t.Fatalf("ResolveFFmpegPath() = %q", got)
	}
}

func TestResolveFFmpegPathRejectsRelativeOverride(t *testing.T) {
	t.Setenv("CODEVIDEO_FFMPEG_PATH", "relative/ffmpeg")
	if _, err := ResolveFFmpegPath(); err == nil {
		t.Fatal("expected relative override to fail")
	}
}

func TestResolveFFmpegPathRejectsNonExecutableOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not use Unix executable mode bits")
	}
	ffmpeg := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEVIDEO_FFMPEG_PATH", ffmpeg)

	_, err := ResolveFFmpegPath()
	if err == nil || !strings.Contains(err.Error(), "not an executable file") {
		t.Fatalf("expected executable error, got %v", err)
	}
}

func TestFFmpegProgressBlocks(t *testing.T) {
	var p ffmpegProgress
	// The first block can carry N/A values before encoding starts.
	for _, line := range []string{"frame=0", "out_time_us=N/A", "speed=N/A", "progress=continue"} {
		if done := p.read(line); done != (line == "progress=continue") {
			t.Fatalf("read(%q) = %v", line, done)
		}
	}
	if fraction, remaining := p.estimate(10); fraction != 0 || remaining != 0 {
		t.Fatalf("empty block estimate = %v, %v", fraction, remaining)
	}

	for _, line := range []string{"out_time_us=2500000", "out_time=00:00:02.500000", "speed=2.5x", "progress=continue"} {
		p.read(line)
	}
	fraction, remaining := p.estimate(10)
	if fraction != 0.25 || remaining != 3*time.Second {
		t.Fatalf("estimate = %v, %v; want 0.25, 3s", fraction, remaining)
	}

	// Older ffmpeg builds only print out_time_ms, which is also microseconds.
	p.read("out_time_ms=12000000")
	if fraction, remaining := p.estimate(10); fraction != 1 || remaining != 0 {
		t.Fatalf("past the end = %v, %v", fraction, remaining)
	}
	if fraction, _ := p.estimate(0); fraction != 0 {
		t.Fatalf("unknown duration fraction = %v", fraction)
	}
}

func TestLastLinesKeepsTheEndOfStderr(t *testing.T) {
	got := lastLines("Input #0\n\n  Stream #0:0\nError opening output\nConversion failed!\n", 2)
	if got != "Error opening output | Conversion failed!" {
		t.Fatalf("lastLines = %q", got)
	}
}

// Chrome's MediaRecorder webm has no duration in its header, like this
// ffmpeg-made file streamed without seeking back to write one.
func TestMediaSecondsReadsHeaderlessWebm(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg required")
	}
	if _, err := ResolveFFprobePath(); err != nil {
		t.Skip("ffprobe required")
	}
	webm := filepath.Join(t.TempDir(), "recording.webm")
	command := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=60:duration=2", "-c:v", "libvpx", "-f", "webm", "pipe:1")
	file, err := os.Create(webm)
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	command.Stdout, command.Stderr = file, &stderr
	if err := command.Run(); err != nil {
		t.Skipf("no libvpx encoder: %v %s", err, stderr.String())
	}
	file.Close()
	seconds, err := mediaSeconds(webm)
	if err != nil {
		t.Fatal(err)
	}
	if seconds < 1.9 || seconds > 2 {
		t.Fatalf("mediaSeconds = %v, want about 2", seconds)
	}
}
