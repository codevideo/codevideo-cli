package constants

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	LEGACY_WORK_FOLDER           = "../tmp/v3"
	NODE_SCRIPT_NAME             = "puppeteer-runner/recordVideoV3.js"
	TOKEN_DECREMENT_AMOUNT       = 10
	MAX_CONCURRENT_JOBS          = 2 // default for an 8 GB server serving both staging and prod; override with CODEVIDEO_MAX_CONCURRENT_JOBS
	DEFAULT_MANIFEST_SERVER_PORT = 7000
	DEFAULT_GATSBY_PORT          = 7001
	DEFAULT_SERVER_TIMEOUT       = time.Second * 5
)

func executableDir() string {
	executable, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(executable)
}

func absoluteEnvPath(name string) string {
	value := os.Getenv(name)
	if value == "" {
		return ""
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return absolute
}

// WorkFolder returns the base directory for transient render state. The
// executable-relative location remains the default for standalone binaries;
// the npm launcher supplies CODEVIDEO_WORK_DIR so node_modules stays read-only.
func WorkFolder() string {
	if configured := absoluteEnvPath("CODEVIDEO_WORK_DIR"); configured != "" {
		return configured
	}
	return filepath.Clean(filepath.Join(executableDir(), LEGACY_WORK_FOLDER))
}

func NewFolder() string     { return filepath.Join(WorkFolder(), "new") }
func ErrorFolder() string   { return filepath.Join(WorkFolder(), "error") }
func SuccessFolder() string { return filepath.Join(WorkFolder(), "success") }
func VideoFolder() string   { return filepath.Join(WorkFolder(), "video") }

func PuppeteerRunnerPath() string {
	if configured := absoluteEnvPath("CODEVIDEO_PUPPETEER_RUNNER_PATH"); configured != "" {
		return configured
	}
	return filepath.Join(executableDir(), NODE_SCRIPT_NAME)
}

func LogFolder() string {
	if configured := absoluteEnvPath("CODEVIDEO_LOG_DIR"); configured != "" {
		return configured
	}
	return executableDir()
}

func OutputFolder() string {
	if configured := absoluteEnvPath("CODEVIDEO_OUTPUT_DIR"); configured != "" {
		return configured
	}
	return executableDir()
}

// MaxConcurrentJobs returns the worker concurrency limit. It is overridable at
// runtime via the CODEVIDEO_MAX_CONCURRENT_JOBS env var (a positive integer);
// otherwise it falls back to MAX_CONCURRENT_JOBS.
func MaxConcurrentJobs() int {
	if v := os.Getenv("CODEVIDEO_MAX_CONCURRENT_JOBS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return MAX_CONCURRENT_JOBS
}
