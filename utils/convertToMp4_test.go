package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
