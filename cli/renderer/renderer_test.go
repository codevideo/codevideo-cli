package renderer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/codevideo/codevideo-cli/cli/progress"
)

// useFile points the renderer at a temporary file, which is not a terminal,
// and returns a function that reads what was written.
func useFile(t *testing.T, options Options) func() string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	out, tty, json, quiet, logPath, width, onReport := screen.out, screen.tty, screen.json, screen.quiet, screen.logPath, screen.width, screen.onReport
	t.Cleanup(func() {
		Stop()
		f.Close()
		screen.out, screen.tty, screen.json, screen.quiet, screen.logPath, screen.width, screen.onReport = out, tty, json, quiet, logPath, width, onReport
	})
	Configure(f, options)
	return func() string {
		data, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
}

func TestClock(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                       "0:00",
		61 * time.Second:        "1:01",
		1500 * time.Millisecond: "0:02",
		3725 * time.Second:      "1:02:05",
	} {
		if got := Clock(d); got != want {
			t.Errorf("Clock(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestDescribe(t *testing.T) {
	recording := progress.Snapshot{Started: true, Stage: progress.Recording, Detail: "action 4/12", Elapsed: 42 * time.Second, Total: 125 * time.Second}
	for _, tt := range []struct {
		s        progress.Snapshot
		withTime bool
		want     string
	}{
		{progress.Snapshot{}, true, "Waiting"},
		{recording, true, "Recording · action 4/12 · 0:42 / 2:05"},
		{recording, false, "Recording · action 4/12"},
		{progress.Snapshot{Started: true, Stage: progress.Done, Detail: "action 12/12", Elapsed: 121 * time.Second}, true, "Done · 2:01"},
	} {
		if got := describe(tt.s, tt.withTime); got != tt.want {
			t.Errorf("describe(%+v) = %q, want %q", tt.s, got, tt.want)
		}
	}
}

// Off a terminal there's no animation: each milestone is one plain line,
// and the milestone callback gets the same text.
func TestPlainOutputPrintsMilestones(t *testing.T) {
	output := useFile(t, Options{})
	var sent []string
	OnMilestone(func(m Milestone) { sent = append(sent, m.Text()) })
	job := progress.NewJob("")
	job.PlanRecording([]float64{1000})
	TrackJob(job)
	Status("Detected project type: Actions")
	job.Begin(progress.Recording)
	time.Sleep(plainCheckGap + 200*time.Millisecond)
	job.Finish("video.mp4")
	Stop()

	lines := strings.Split(strings.TrimSpace(output()), "\n")
	recording := regexp.MustCompile(`^\[ *\d+%\] Recording · action 1/1 · 0:\d\d / 0:\d\d$`)
	done := regexp.MustCompile(`^\[100%\] Done · 0:0\d$`)
	if len(lines) != 3 || lines[0] != "Detected project type: Actions" || !recording.MatchString(lines[1]) || !done.MatchString(lines[2]) {
		t.Fatalf("output:\n%s", output())
	}
	if len(sent) != 2 || sent[0] != lines[1] || sent[1] != lines[2] {
		t.Fatalf("callback got %q", sent)
	}
}

func TestQuietHidesStatusAndBarsButNotLogLines(t *testing.T) {
	output := useFile(t, Options{Quiet: true})
	job := progress.NewJob("")
	TrackJob(job)
	Status("Detected project type: Actions")
	Log("error", "something broke")
	job.Begin(progress.Starting)
	job.Finish("video.mp4")
	Stop()
	if got := output(); got != "error: something broke\n" {
		t.Fatalf("output = %q", got)
	}
}

// On a terminal the block is erased line by line and redrawn; lines are cut
// to the width so they never wrap, which would break the cursor arithmetic.
func TestCourseBlockRepaintsInPlace(t *testing.T) {
	var out bytes.Buffer
	first, second := progress.NewJob("Introduction to closures"), progress.NewJob("Loops")
	u := &ui{out: &out, tty: true, width: 60, course: progress.NewCourse([]*progress.Job{first, second})}
	u.draw()
	drawn := strings.Split(out.String(), "\n")
	if u.drawn != 3 || len(drawn) != 3 {
		t.Fatalf("drew %d lines: %q", u.drawn, drawn)
	}
	if !strings.HasPrefix(drawn[1], "  1 Introduction…  [") || !strings.HasPrefix(drawn[2], "  2 Loops          [") {
		t.Errorf("lesson lines = %q", drawn[1:])
	}
	for _, line := range drawn {
		if n := len([]rune(line)); n > 59 {
			t.Errorf("line of %d runes wraps a 60 column terminal: %q", n, line)
		}
	}

	out.Reset()
	u.erase()
	if got, want := out.String(), "\r\033[2K\033[1A\033[2K\033[1A\033[2K"; got != want || u.drawn != 0 {
		t.Fatalf("erase wrote %q, want %q", got, want)
	}
}

func TestMilestonesFollowTheShownPercentage(t *testing.T) {
	u := &ui{out: &bytes.Buffer{}, tty: true, last: map[string]milestoneState{}}
	job := progress.NewJob("")
	job.PlanRecording([]float64{1000})
	job.Begin(progress.Recording)
	u.job = job
	first := u.report()
	if len(first) != 1 {
		t.Fatalf("first report = %v", first)
	}
	// Reporting again in the same stage and quarter is quiet.
	if again := u.report(); len(again) != 0 {
		t.Fatalf("repeated milestone: %q", again[0].Text())
	}
}

// Log lines keep their level as a prefix, except info lines.
func TestLogLines(t *testing.T) {
	output := useFile(t, Options{})
	Log("info", "Loaded config from: portrait.json\n")
	Log("warning", "Failed to move manifest")
	Log("error", "job failed")
	Log("debug", "ffmpeg: 1.0s")
	want := "Loaded config from: portrait.json\nwarning: Failed to move manifest\nerror: job failed\ndebug: ffmpeg: 1.0s\n"
	if got := output(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

// jsonLines decodes output that should be one JSON object per line.
func jsonLines(t *testing.T, output string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatalf("not a JSON object: %q", line)
		}
		lines = append(lines, fields)
	}
	return lines
}

// JSON progress comes at each milestone and at least once a second while a
// stage runs, however often the renderer checks; a finished video goes quiet.
func TestJSONHeartbeat(t *testing.T) {
	s := newScene(t, 80, false)
	s.ui.json = true
	job := progress.NewJob("")
	job.PlanRecording([]float64{60000})
	s.ui.job = job
	job.Begin(progress.Starting)
	check := func() {
		s.ui.mu.Lock()
		s.ui.tick()
		s.ui.mu.Unlock()
	}
	for tick := 0; tick <= 12; tick++ { // three seconds, checked every 250ms
		if tick == 6 {
			job.Begin(progress.Recording)
		}
		check()
		s.wait(250 * time.Millisecond)
	}
	job.Finish("video.mp4")
	check()
	s.wait(2 * time.Second)
	check()

	var got []string
	for _, line := range jsonLines(t, s.out.String()) {
		got = append(got, fmt.Sprintf("%s at %vs", line["stage"], line["elapsed"]))
		if _, ok := line["lesson"]; ok {
			t.Errorf("a single video's line has a lesson: %v", line)
		}
	}
	// Times round to a tenth of a second: 3.25s reads 3.3.
	want := []string{"Starting browser at 0s", "Starting browser at 1s", "Recording at 1.5s", "Recording at 2.5s", "Done at 3.3s"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Status lines are left out of JSON output; log lines are keyed by level,
// errors name the log, and saved videos have absolute paths.
func TestJSONLogResultAndErrorLines(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "codevideo.log")
	output := useFile(t, Options{Format: JSON, LogPath: logPath})
	video := filepath.Join(t.TempDir(), "video.mp4")
	t.Chdir(filepath.Dir(video))

	Status("Detected project type: Actions")
	Log("warning", "a <slow> disk & more\n")
	Log("info", "Logs saved")
	Log("error", "recording failed")
	Log("fatal", "static server port 7001 is unavailable")
	Result("video.mp4", 0)
	Result(video, 2)
	Failed()
	Cancelled()

	got := jsonLines(t, output())
	want := []map[string]any{
		{"warning": "a <slow> disk & more"},
		{"info": "Logs saved"},
		{"error": "recording failed", "log": logPath},
		{"error": "static server port 7001 is unavailable", "log": logPath},
		{"result": video},
		{"result": video, "lesson": float64(2)},
		{"error": "cancelled", "log": logPath},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %v\nwant %v", got, want)
	}
	if !strings.Contains(output(), "a <slow> disk & more") {
		t.Errorf("HTML characters were escaped:\n%s", output())
	}
}

// -q hides JSON progress but not the results or errors.
func TestQuietJSONKeepsResultsAndErrors(t *testing.T) {
	output := useFile(t, Options{Format: JSON, Quiet: true})
	job := progress.NewJob("")
	job.PlanRecording([]float64{1000})
	TrackJob(job)
	job.Begin(progress.Recording)
	time.Sleep(3 * repaintEvery)
	job.Finish("video.mp4")
	Stop()
	Log("error", "failed to open file")
	got := jsonLines(t, output())
	if len(got) != 1 || got[0]["error"] != "failed to open file" {
		t.Fatalf("lines = %v", got)
	}
}
