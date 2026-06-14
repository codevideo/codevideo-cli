package constants

import (
	"os"
	"strconv"
	"time"
)

const (
	NEW_FOLDER                   = "../tmp/v3/new"
	ERROR_FOLDER                 = "../tmp/v3/error"
	SUCCESS_FOLDER               = "../tmp/v3/success"
	VIDEO_FOLDER                 = "../tmp/v3/video"
	NODE_SCRIPT_NAME             = "puppeteer-runner/recordVideoV3.js"
	TOKEN_DECREMENT_AMOUNT       = 10
	MAX_CONCURRENT_JOBS          = 2 // default for an 8 GB server serving both staging and prod; override with CODEVIDEO_MAX_CONCURRENT_JOBS
	DEFAULT_MANIFEST_SERVER_PORT = 7000
	DEFAULT_GATSBY_PORT          = 7001
	DEFAULT_SERVER_TIMEOUT       = time.Second * 5
)

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
