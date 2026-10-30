package renderer

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/codevideo/codevideo-cli/cli/progress"
)

// useFile points the renderer at a temporary file, which is not a terminal,
// and returns a function that reads what was written.
func useFile(t *testing.T, quiet bool) func() string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	out, tty, wasQuiet, width, onReport := screen.out, screen.tty, screen.quiet, screen.width, screen.onReport
	t.Cleanup(func() {
		Stop()
		f.Close()
		screen.out, screen.tty, screen.quiet, screen.width, screen.onReport = out, tty, wasQuiet, width, onReport
	})
	Configure(f, quiet)
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
	output := useFile(t, false)
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
	output := useFile(t, true)
	job := progress.NewJob("")
	TrackJob(job)
	Status("Detected project type: Actions")
	Println("error: something broke")
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
