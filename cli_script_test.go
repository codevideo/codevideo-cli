package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

// End-to-end tests of the CLI as a user runs it: the real binary, its flags,
// what it prints where, and its exit codes. The browser, ffmpeg and the
// network are replaced with fakes (fakes_test.go), so the scripts in
// testdata/script run in seconds on Linux, macOS and Windows.
//
//	go test -run TestScripts             # run them
//	go test -run TestScripts -update     # accept new golden screens

var updateScripts = flag.Bool("update", false, "rewrite the golden files that testdata/script/*.txtar compare against")

func TestMain(m *testing.M) {
	// testscript puts each command on the scripts' PATH under its name; the
	// fakes stand in for the programs the CLI runs.
	testscript.Main(m, map[string]func(){
		"codevideo": main,
		"node":      fakeNode,
		"ffmpeg":    fakeFFmpeg,
		"ffprobe":   fakeFFprobe,
	})
}

func TestScripts(t *testing.T) {
	params := testscript.Params{
		Dir:                 filepath.Join("testdata", "script"),
		Setup:               setupScript,
		RequireExplicitExec: true,
		UpdateScripts:       *updateScripts,
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"ttyrun":     ttyrun,
			"slackposts": slackPosts,
		},
	}
	if deadline, ok := t.Deadline(); ok {
		params.Deadline = deadline
	}
	testscript.RunT(serialT{t}, params)
}

// serialT runs the scripts one at a time. Every CLI run binds port 7001 for
// the embedded site (and tries 7000 for manifests); running them in parallel
// would need a port override in the CLI and the Node runners.
type serialT struct{ *testing.T }

func (serialT) Parallel() {}

func (t serialT) Run(name string, f func(testscript.T)) {
	t.T.Run(name, func(t *testing.T) { f(serialT{t}) })
}

func (serialT) Verbose() bool { return testing.Verbose() }

// scriptKeys is the Values key for the environment variable names a script
// starts with; ttyrun passes their current values on.
type scriptKeys struct{}

type slackKey struct{}

func setupScript(env *testscript.Env) error {
	work := env.WorkDir
	// The CLI checks the runner scripts exist; the fake node plays them.
	runner := filepath.Join(work, "runner")
	if err := os.MkdirAll(runner, 0755); err != nil {
		return err
	}
	for _, name := range []string{"recordVideoV3.js", "renderFramesV1.js"} {
		if err := os.WriteFile(filepath.Join(runner, name), []byte("// played by fakeNode in fakes_test.go\n"), 0644); err != nil {
			return err
		}
	}
	envFile := filepath.Join(work, "empty.env")
	if err := os.WriteFile(envFile, nil, 0644); err != nil {
		return err
	}

	tts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/audio/speech" {
			http.NotFound(w, r)
			return
		}
		// About as long as a real TTS request, so narration shows in the output.
		time.Sleep(600 * time.Millisecond)
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("fake mp3"))
	}))
	env.Defer(tts.Close)
	slack := newSlackRecorder()
	env.Defer(slack.Close)
	env.Values[slackKey{}] = slack

	for _, kv := range [][2]string{
		// An empty file instead of the .env next to the binary: no keys, no Slack.
		{"CODEVIDEO_ENV_FILE", envFile},
		{"CODEVIDEO_PUPPETEER_RUNNER_PATH", filepath.Join(runner, "recordVideoV3.js")},
		{"CODEVIDEO_WORK_DIR", filepath.Join(work, "work")},
		{"CODEVIDEO_LOG_DIR", filepath.Join(work, "logs")},
		{"CODEVIDEO_OUTPUT_DIR", filepath.Join(work, "out")},
		{"CODEVIDEO_TTS_PROVIDER", "kokoro"},
		{"TTS_SERVICE_URL", tts.URL},
		{"SLACK_URL", slack.URL},
		// Knobs for the fakes; scripts change them with env.
		{"FAKE_RUNNER", ""},
		{"FAKE_STEP_MS", "150"},
	} {
		env.Setenv(kv[0], kv[1])
	}
	if runtime.GOOS == "windows" {
		// Windows networking (the CLI's static server) fails without SYSTEMROOT.
		for _, name := range []string{"SYSTEMROOT", "WINDIR", "PATHEXT", "COMSPEC"} {
			if value := os.Getenv(name); value != "" {
				env.Setenv(name, value)
			}
		}
	}
	keys := make([]string, 0, len(env.Vars))
	for _, kv := range env.Vars {
		if name, _, ok := strings.Cut(kv, "="); ok && name != "" {
			keys = append(keys, name)
		}
	}
	env.Values[scriptKeys{}] = keys
	return nil
}

// slackRecorder is a Slack incoming webhook that keeps what it's sent.
type slackRecorder struct {
	*httptest.Server
	mu    sync.Mutex
	posts []string
}

func newSlackRecorder() *slackRecorder {
	r := &slackRecorder{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body struct{ Text string }
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		r.posts = append(r.posts, body.Text)
		r.mu.Unlock()
		fmt.Fprint(w, "ok")
	}))
	return r
}

// slackPosts prints what the script's webhook received, one post per line, so
// stdout can check it.
func slackPosts(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) > 0 {
		ts.Fatalf("usage: slackposts")
	}
	// Writing through ts.Stdout, even nothing, makes this the output that
	// stdout checks next.
	out := ts.Stdout()
	r := ts.Value(slackKey{}).(*slackRecorder)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, post := range r.posts {
		fmt.Fprintln(out, post)
	}
}

// scriptEnv is the script's environment as it stands, for commands the
// script doesn't start through exec.
func scriptEnv(ts *testscript.TestScript) []string {
	var env []string
	for _, name := range ts.Value(scriptKeys{}).([]string) {
		env = append(env, name+"="+ts.Getenv(name))
	}
	return env
}

// lookPath finds a command on the script's PATH.
func lookPath(ts *testscript.TestScript, name string) (string, error) {
	exts := []string{""}
	if runtime.GOOS == "windows" {
		exts = []string{".exe", ""}
	}
	for _, dir := range filepath.SplitList(ts.Getenv("PATH")) {
		for _, ext := range exts {
			path := filepath.Join(dir, name+ext)
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("%s not found on the script's PATH", name)
}
