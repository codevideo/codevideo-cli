package server

import (
	"math"
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/codevideo/codevideo-cli/cli/progress"
	"github.com/codevideo/codevideo-cli/types"
)

func TestRunnerEventsDriveTheJob(t *testing.T) {
	job := progress.NewJob("")
	job.PlanRecording([]float64{1000, 1000})
	for _, line := range []string{
		`CODEVIDEO_PROGRESS {"stage":"recording"}`,
		`CODEVIDEO_PROGRESS {"stage":"recording","action":1}`,
	} {
		event, ok := parseRunnerEvent(line)
		if !ok {
			t.Fatalf("not parsed: %s", line)
		}
		event.apply(job)
	}
	if s := job.Snapshot(time.Now()); s.Stage != progress.Recording || s.Detail != "action 2/2" {
		t.Fatalf("snapshot = %+v", s)
	}
	for _, line := range []string{"Progress update: { currentAction: 1 }", "CODEVIDEO_PROGRESS not json"} {
		if _, ok := parseRunnerEvent(line); ok {
			t.Errorf("parsed %q", line)
		}
	}
}

func TestRunRunnerExplainsFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}
	job := progress.NewJob("")
	job.PlanDeterministic([]float64{1000}, 1)
	script := `echo 'CODEVIDEO_PROGRESS {"stage":"frames","frame":30,"frames":180}'; echo chatter; echo 'first problem' >&2; printf 'last problem' >&2; exit 3`
	err := runRunner(exec.Command("sh", "-c", script), "Test runner", job)
	if err == nil || !strings.HasSuffix(err.Error(), "exit status 3: first problem | last problem") {
		t.Fatalf("err = %v", err)
	}
	if s := job.Snapshot(time.Now()); s.Stage != progress.Frames || s.Detail != "frame 30/180" {
		t.Fatalf("snapshot = %+v", s)
	}

	// The runner's own error message wins over stderr noise such as Chrome's.
	script = `echo 'Chrome noise' >&2; echo 'CODEVIDEO_PROGRESS {"error":"Failed to launch the browser process"}'; exit 1`
	err = runRunner(exec.Command("sh", "-c", script), "Test runner", nil)
	if err == nil || !strings.HasSuffix(err.Error(), "exit status 1: Failed to launch the browser process") {
		t.Fatalf("err = %v", err)
	}
	if err := runRunner(exec.Command("sh", "-c", "echo fine"), "Test runner", nil); err != nil {
		t.Fatal(err)
	}
}

func TestLineWriterSplitsAcrossWrites(t *testing.T) {
	var lines []string
	w := &lineWriter{line: func(line string) { lines = append(lines, line) }}
	w.Write([]byte("one\r\ntw"))
	w.Write([]byte("o\nthree"))
	w.flush()
	if want := []string{"one", "two", "three"}; !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
}

func TestPlanProgressUsesMeasuredNarration(t *testing.T) {
	actions := []types.Action{
		{Name: "author-speak-before", Value: "one two three four five"},
		{Name: "editor-type", Value: "ab"},
	}
	plan := func(clipMs float64) float64 {
		job := progress.NewJob("")
		job.PlanNarration(1)
		job.Begin(progress.Narration)
		if clipMs > 0 {
			job.ClipDone(0, clipMs)
		}
		PlanProgress(job, actions, nil, types.RenderModeRealtime, 1)
		return job.RecordedSeconds()
	}
	// On the /v3 page each action waits 200ms and typing "ab" takes 80ms; the
	// recorder adds 2s around playback. Five words guess 2s of speech; the
	// clip measured 4s.
	if got := plan(0); math.Abs(got-4.48) > 1e-9 {
		t.Errorf("word-count estimate = %vs, want 4.48s", got)
	}
	if got := plan(4000); math.Abs(got-6.48) > 1e-9 {
		t.Errorf("measured estimate = %vs, want 6.48s", got)
	}
	PlanProgress(nil, actions, nil, types.RenderModeRealtime, 1) // serve mode: no job
}
