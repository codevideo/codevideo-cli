package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// An unknown render mode fails before any browser, S3 or Clerk work, so it stands
// in for every failure path without network access.
const failingManifest = `{"environment":"staging","userId":"user_1","uuid":"job-1","renderMode":"bogus","audioItems":[],"actions":[]}`

func queueManifest(t *testing.T, workDir string) string {
	t.Helper()
	path := filepath.Join(workDir, "new", "job-1.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(failingManifest), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServeFailureMovesManifestToErrorFolder(t *testing.T) {
	work := t.TempDir()
	t.Setenv("CODEVIDEO_WORK_DIR", work)
	manifestPath := queueManifest(t, work)

	if err := ProcessJob(manifestPath, "serve", ""); err == nil {
		t.Fatal("expected the job to fail")
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
		t.Fatalf("failed manifest is still in new/: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(work, "error", "job-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if message, _ := manifest["error"].(string); message == "" {
		t.Fatalf("error not recorded in the moved manifest: %s", data)
	}
}

func TestCLIFailureLeavesManifestInPlace(t *testing.T) {
	work := t.TempDir()
	t.Setenv("CODEVIDEO_WORK_DIR", work)
	manifestPath := queueManifest(t, work)

	if err := ProcessJob(manifestPath, "cli", ""); err == nil {
		t.Fatal("expected the job to fail")
	}
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("a cli-mode manifest should stay where it is: %v", err)
	}
}
