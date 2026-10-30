package renderer

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"

	"github.com/codevideo/codevideo-cli/cli/progress"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current output")

// scene drives a renderer by hand: one fake clock for the renderer and the
// progress model, and for terminal output an emulated screen behind it. Each
// step adds what the user would see to a transcript, which is compared with a
// golden file.
type scene struct {
	t     *testing.T
	start time.Time
	now   time.Time
	ui    *ui
	out   bytes.Buffer
	term  *vt.Emulator
	trace strings.Builder
}

func newScene(t *testing.T, width int, tty bool) *scene {
	t.Helper()
	s := &scene{t: t, start: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)}
	s.now = s.start
	clock := func() time.Time { return s.now }
	t.Cleanup(progress.SetClock(clock))
	t.Cleanup(func() { progress.SetCurrent(nil) })
	s.ui = &ui{out: &s.out, tty: tty, width: width, now: clock, last: map[string]milestoneState{}}
	if tty {
		s.term = vt.NewEmulator(width, 12)
		// A terminal answers some sequences; the renderer sends none, but drain
		// the replies anyway so the emulator can never block on them.
		go io.Copy(io.Discard, s.term)
		t.Cleanup(func() { s.term.Close() })
	}
	return s
}

func (s *scene) wait(d time.Duration) { s.now = s.now.Add(d) }

// show repaints the bars and records the screen.
func (s *scene) show(what string) {
	s.ui.mu.Lock()
	s.ui.tick()
	s.ui.mu.Unlock()
	s.record(what)
}

// log prints a line above the bars, as a log line or status line would.
func (s *scene) log(line string) {
	s.ui.mu.Lock()
	s.ui.println(line)
	s.ui.mu.Unlock()
}

// end draws the final state, as Stop does.
func (s *scene) end(what string) {
	s.ui.mu.Lock()
	s.ui.finish()
	s.ui.mu.Unlock()
	s.record(what)
}

// record adds the screen (or, off a terminal, the lines printed since the
// last step) under a heading with the time since the scene started.
func (s *scene) record(what string) {
	fmt.Fprintf(&s.trace, "── %s %s ──\n", Clock(s.now.Sub(s.start)), what)
	if s.term == nil {
		s.trace.Write(s.out.Bytes())
		s.out.Reset()
		return
	}
	// A terminal's line discipline turns \n into \r\n (ONLCR) on the way to
	// the screen; the emulator gets the bytes raw, so do that here.
	s.term.Write(bytes.ReplaceAll(s.out.Bytes(), []byte("\n"), []byte("\r\n")))
	s.out.Reset()
	s.trace.WriteString(screenText(s.term.String()))
}

// screenText trims trailing spaces and blank lines from an emulated screen.
func screenText(screen string) string {
	lines := strings.Split(screen, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// golden compares got with testdata/name.golden; -update rewrites the file.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (go test ./cli/renderer -update creates it)", err)
	}
	if got == string(want) {
		return
	}
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(string(want), "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Fatalf("%s differs at line %d (go test ./cli/renderer -update accepts the new output)\n got: %q\nwant: %q\n\nfull output:\n%s", path, i+1, g, w, got)
		}
	}
}

// A narrated video from the first clip to the saved file, on a terminal and
// off one (CI logs, pipes).
func TestGoldenSingleVideo(t *testing.T) {
	for _, mode := range []struct {
		name string
		tty  bool
	}{{"single-video-tty", true}, {"single-video-plain", false}} {
		t.Run(mode.name, func(t *testing.T) {
			s := newScene(t, 80, mode.tty)
			job := progress.NewJob("")
			job.PlanNarration(2)
			job.PlanRecording([]float64{3200, 1400, 2600})
			s.ui.job = job

			job.Begin(progress.Narration)
			s.show("narration starts")
			s.wait(2 * time.Second)
			job.ClipDone(0, 3000)
			s.show("first clip generated")
			s.wait(2 * time.Second)
			job.ClipDone(2, 2500)
			job.Begin(progress.Starting)
			s.show("browser starting")
			s.wait(9 * time.Second)
			job.Begin(progress.Recording)
			s.wait(time.Second)
			job.ActionStarted(0)
			s.wait(2 * time.Second)
			s.show("inside the first action")
			s.log("warning: log lines print above the bar")
			s.wait(2 * time.Second)
			job.ActionStarted(1)
			s.wait(time.Second)
			s.show("second action, a little slower than planned")
			s.wait(time.Second)
			job.ActionStarted(2)
			s.wait(3 * time.Second)
			job.ActionStarted(3)
			job.Begin(progress.Closing)
			s.wait(2 * time.Second)
			s.show("closing the browser")
			s.wait(3 * time.Second)
			job.Begin(progress.Encoding)
			s.wait(4 * time.Second)
			job.SetFraction(0.5, 5*time.Second)
			s.show("encoding halfway")
			s.wait(5 * time.Second)
			job.SetFraction(1, 0)
			job.Finish("out.mp4")
			s.end("saved")
			golden(t, mode.name, s.trace.String())
		})
	}
}

// A course: an overall bar with a bar per lesson. Both lessons are narrated
// first, then rendered one after the other.
func TestGoldenCourse(t *testing.T) {
	for _, mode := range []struct {
		name string
		tty  bool
	}{{"course-tty", true}, {"course-plain", false}} {
		t.Run(mode.name, func(t *testing.T) {
			s := newScene(t, 80, mode.tty)
			lessons := []*progress.Job{progress.NewJob("Variables"), progress.NewJob("Printing values to the console")}
			for _, lesson := range lessons {
				lesson.PlanNarration(1)
				lesson.PlanRecording([]float64{2400, 1200})
			}
			s.ui.course = progress.NewCourse(lessons)

			for i, lesson := range lessons {
				progress.SetCurrent(lesson)
				lesson.Begin(progress.Narration)
				s.wait(2 * time.Second)
				lesson.ClipDone(0, 2000)
				s.show(fmt.Sprintf("lesson %d narrated", i+1))
			}
			for i, lesson := range lessons {
				progress.SetCurrent(lesson)
				lesson.Begin(progress.Starting)
				s.wait(10 * time.Second)
				lesson.Begin(progress.Recording)
				lesson.ActionStarted(0)
				s.wait(2 * time.Second)
				s.show(fmt.Sprintf("lesson %d recording", i+1))
				lesson.ActionStarted(1)
				s.wait(time.Second)
				lesson.ActionStarted(2)
				lesson.Begin(progress.Closing)
				s.wait(4 * time.Second)
				lesson.Begin(progress.Encoding)
				s.wait(3 * time.Second)
				lesson.SetFraction(0.6, 2*time.Second)
				s.show(fmt.Sprintf("lesson %d encoding", i+1))
				s.wait(2 * time.Second)
				lesson.Finish(fmt.Sprintf("lesson-%d.mp4", i+1))
			}
			s.end("both lessons saved")
			golden(t, mode.name, s.trace.String())
		})
	}
}

// A deterministic render's stages: planning the timeline, then capturing frames.
func TestGoldenDeterministic(t *testing.T) {
	s := newScene(t, 80, true)
	job := progress.NewJob("")
	job.PlanDeterministic([]float64{2000, 2000}, 2)
	s.ui.job = job

	job.Begin(progress.Starting)
	s.wait(8 * time.Second)
	job.Begin(progress.Planning)
	s.wait(time.Second)
	job.PlanningFrame(180)
	s.show("planning")
	s.wait(time.Second)
	job.Begin(progress.Frames)
	job.FramesDone(0, 360)
	s.wait(8 * time.Second)
	job.FramesDone(20, 360)
	s.wait(4 * time.Second)
	job.FramesDone(180, 360)
	s.show("capturing frames")
	s.wait(6 * time.Second)
	job.FramesDone(360, 360)
	job.Begin(progress.Encoding)
	s.wait(time.Second)
	job.Finish("out.mp4")
	s.end("saved")
	golden(t, "deterministic-tty", s.trace.String())
}

// Lines are cut to the terminal's width, never wrapped: a wrapped bar would
// break the cursor arithmetic of the next repaint.
func TestGoldenNarrowTerminal(t *testing.T) {
	s := newScene(t, 50, true)
	lessons := []*progress.Job{progress.NewJob("Variables"), progress.NewJob("Printing values to the console")}
	for _, lesson := range lessons {
		lesson.PlanRecording([]float64{2400, 1200})
	}
	s.ui.course = progress.NewCourse(lessons)
	progress.SetCurrent(lessons[0])
	lessons[0].Begin(progress.Starting)
	s.wait(10 * time.Second)
	lessons[0].Begin(progress.Recording)
	lessons[0].ActionStarted(0)
	s.wait(time.Second)
	s.show("50 columns")
	s.log("a log line longer than the terminal is wide wraps instead of being cut, since only the bars are repainted")
	s.wait(time.Second)
	s.show("after a long log line")
	golden(t, "narrow-tty", s.trace.String())
}
