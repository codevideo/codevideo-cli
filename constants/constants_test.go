package constants

import (
	"path/filepath"
	"testing"
)

func TestRuntimePathOverrides(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CODEVIDEO_WORK_DIR", filepath.Join(base, "work"))
	t.Setenv("CODEVIDEO_LOG_DIR", filepath.Join(base, "logs"))
	t.Setenv("CODEVIDEO_OUTPUT_DIR", filepath.Join(base, "output"))
	t.Setenv("CODEVIDEO_PUPPETEER_RUNNER_PATH", filepath.Join(base, "runner.js"))

	checks := map[string]string{
		NewFolder():           filepath.Join(base, "work", "new"),
		ErrorFolder():         filepath.Join(base, "work", "error"),
		SuccessFolder():       filepath.Join(base, "work", "success"),
		VideoFolder():         filepath.Join(base, "work", "video"),
		LogFolder():           filepath.Join(base, "logs"),
		OutputFolder():        filepath.Join(base, "output"),
		PuppeteerRunnerPath(): filepath.Join(base, "runner.js"),
		TTSCacheFolder():      filepath.Join(base, "work", "tts"),
	}
	for got, want := range checks {
		if got != want {
			t.Fatalf("runtime path = %q, want %q", got, want)
		}
	}
}

func TestTTSCacheFolderOverride(t *testing.T) {
	base := t.TempDir()
	t.Setenv("CODEVIDEO_WORK_DIR", filepath.Join(base, "work"))
	t.Setenv("CODEVIDEO_TTS_CACHE_DIR", filepath.Join(base, "clips"))
	if got, want := TTSCacheFolder(), filepath.Join(base, "clips"); got != want {
		t.Fatalf("TTSCacheFolder() = %q, want %q", got, want)
	}
}
