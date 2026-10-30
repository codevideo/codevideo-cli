package progress

import (
	"math"
	"testing"
	"time"
)

// fakeClock lets a test move time by hand.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func useFakeClock(t *testing.T) *fakeClock {
	c := &fakeClock{t: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	t.Cleanup(SetClock(c.now))
	return c
}

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.001 {
		t.Errorf("%s = %.4f, want %.4f", name, got, want)
	}
}

func TestRecordingInterpolatesInsideTheCurrentAction(t *testing.T) {
	c := useFakeClock(t)
	j := NewJob("")
	j.PlanRecording([]float64{1000, 3000})
	j.Begin(Recording)
	j.ActionStarted(0)

	c.advance(500 * time.Millisecond)
	j.Snapshot(c.now())
	near(t, "half of action 1", j.frac[Recording], 0.125)

	// Action 1 overruns its 1s estimate: the bar waits at its end.
	c.advance(1500 * time.Millisecond)
	j.Snapshot(c.now())
	near(t, "overrunning action 1", j.frac[Recording], 0.25)

	// It took 2s for 1s of plan; with 1s of evidence the pace leans a quarter
	// of the way to 2x: 1.25. Action 2 then fills at 1/1.25 of real time.
	j.ActionStarted(1)
	near(t, "pace", j.recordingPace(), 1.25)
	c.advance(1500 * time.Millisecond)
	s := j.Snapshot(c.now())
	near(t, "inside action 2", j.frac[Recording], (1000+1500/1.25)/4000)
	if s.Detail != "action 2/2" {
		t.Errorf("detail = %q", s.Detail)
	}

	j.ActionStarted(2) // the page reports the end of playback
	j.Snapshot(c.now())
	near(t, "after the last action", j.frac[Recording], 1)
}

// A machine that plays everything twice as slowly as planned: the estimate of
// what's left grows toward twice the remaining plan as evidence builds up.
func TestRecordingLearnsThePace(t *testing.T) {
	c := useFakeClock(t)
	j := NewJob("")
	plan := []float64{2000, 2000, 2000, 2000, 2000, 2000}
	j.PlanRecording(plan)
	j.Begin(Recording)
	c.advance(time.Second) // the recorder's lead-in
	for i := range 4 {
		j.ActionStarted(i)
		c.advance(4 * time.Second)
	}
	j.ActionStarted(4)
	j.Snapshot(c.now())
	// 8s of plan took 16s: weight 8/11 toward a measured 2x.
	pace := 3.0/11 + 8.0/11*2
	near(t, "pace", j.recordingPace(), pace)
	near(t, "time left", j.remainingInStage(c.now()), 4*pace+1)
	// Encoding scales with the recording now expected, not the plan.
	near(t, "encoding estimate", j.expect[Encoding], (17+4*pace+1)*defaultEncodeFactor)
}

func TestFractionIsElapsedOverTotalAndNeverGoesBackwards(t *testing.T) {
	c := useFakeClock(t)
	j := NewJob("")
	j.PlanNarration(2)
	j.PlanRecording([]float64{2000, 2000})
	j.Begin(Narration)

	c.advance(2 * time.Second)
	first := j.Snapshot(c.now())
	near(t, "fraction", first.Fraction, first.Elapsed.Seconds()/first.Total.Seconds())

	// A slow first clip doubles the narration estimate; the bar holds still.
	c.advance(6 * time.Second)
	j.ClipDone(0, 2500)
	second := j.Snapshot(c.now())
	if second.Fraction < first.Fraction {
		t.Fatalf("fraction went backwards: %.3f -> %.3f", first.Fraction, second.Fraction)
	}
	if got := j.SpeechMs()[0]; got != 2500 {
		t.Errorf("measured clip length = %v", got)
	}

	j.Finish("out.mp4")
	done := j.Snapshot(c.now())
	if done.Fraction != 1 || done.Total != done.Elapsed || done.Result != "out.mp4" {
		t.Fatalf("finished snapshot = %+v", done)
	}
}

func TestEncodingUsesTheRecordedLength(t *testing.T) {
	c := useFakeClock(t)
	j := NewJob("")
	j.PlanRecording([]float64{1000})
	j.Begin(Recording)
	c.advance(10 * time.Second)
	j.Begin(Encoding)
	near(t, "recorded seconds", j.RecordedSeconds(), 10)
	near(t, "encoding estimate", j.expect[Encoding], 10*defaultEncodeFactor)

	c.advance(1 * time.Second)
	j.SetFraction(0.5, 2*time.Second)
	near(t, "encoding fraction", j.frac[Encoding], 0.5)
	// Halfway through, the prior expectation and ffmpeg's measurement count
	// equally; near the end the measurement takes over.
	prior := 10*defaultEncodeFactor - 1
	near(t, "time left halfway", j.remainingInStage(c.now()), 0.5*prior+0.5*2)
	c.advance(4 * time.Second)
	j.SetFraction(0.95, time.Second)
	near(t, "time left near the end", j.remainingInStage(c.now()), 0.05*(10*defaultEncodeFactor-5)+0.95*1)
}

func TestCourseSumsLessonsInOrder(t *testing.T) {
	c := useFakeClock(t)
	t.Cleanup(func() { SetCurrent(nil) })
	first, second := NewJob("Intro"), NewJob("Loops")
	first.PlanRecording([]float64{1000})
	second.PlanRecording([]float64{1000})
	course := NewCourse([]*Job{first, second})

	SetCurrent(first)
	first.Begin(Recording)
	c.advance(3 * time.Second)
	first.Finish("intro.mp4")
	SetCurrent(second)
	second.Begin(Starting)
	overall, lessons := course.Snapshot(c.now())
	if overall.Detail != "lesson 2/2" || overall.Stage != Starting || lessons[0].Stage != Done || lessons[1].Stage != Starting {
		t.Fatalf("overall %+v, lessons %+v", overall, lessons)
	}
	if overall.Fraction <= 0 || overall.Fraction >= 1 {
		t.Fatalf("overall fraction = %.3f", overall.Fraction)
	}

	second.Finish("loops.mp4")
	overall, _ = course.Snapshot(c.now())
	if overall.Stage != Done || overall.Fraction != 1 {
		t.Fatalf("finished course = %+v", overall)
	}
}

// A course narrates every lesson before rendering any. A narrated lesson's bar
// holds still while it waits; only the overall bar moves with time.
func TestLessonBarsCountWorkNotWaiting(t *testing.T) {
	c := useFakeClock(t)
	t.Cleanup(func() { SetCurrent(nil) })
	first, second := NewJob("Intro"), NewJob("Loops")
	for _, j := range []*Job{first, second} {
		j.PlanNarration(1)
		j.PlanRecording([]float64{2000})
	}
	course := NewCourse([]*Job{first, second})

	SetCurrent(first)
	first.Begin(Narration)
	c.advance(2 * time.Second)
	first.ClipDone(0, 1500)
	SetCurrent(second)
	second.Begin(Narration)
	_, lessons := course.Snapshot(c.now())
	narrated := lessons[0].Fraction
	if lessons[0].Stage != Waiting || narrated <= 0 {
		t.Fatalf("narrated lesson = %+v", lessons[0])
	}

	c.advance(10 * time.Second)
	overallBefore, _ := course.Snapshot(c.now().Add(-time.Second))
	overall, lessons := course.Snapshot(c.now())
	near(t, "waiting lesson", lessons[0].Fraction, narrated)
	if overall.Fraction <= overallBefore.Fraction {
		t.Errorf("overall bar stood still: %.3f -> %.3f", overallBefore.Fraction, overall.Fraction)
	}
	if overall.Stage != Narration || overall.Detail != "lesson 2/2" {
		t.Errorf("overall = %+v", overall)
	}
}

func TestDeterministicStagesRefineFromFrameRates(t *testing.T) {
	c := useFakeClock(t)
	j := NewJob("")
	j.PlanDeterministic([]float64{1000}, 2)
	// One second of actions plus two of padding, at 60fps.
	near(t, "planning estimate", j.expect[Planning], 180*defaultPlanSecondsPerFrame)
	near(t, "capture estimate", j.expect[Frames], defaultStartupSeconds+180*defaultCaptureSecondsPerFrame/2)
	near(t, "mux estimate", j.expect[Encoding], 3*defaultMuxFactor)

	j.Begin(Starting)
	c.advance(8 * time.Second)
	j.Begin(Planning)
	c.advance(time.Second)
	j.PlanningFrame(90)
	near(t, "planning fraction", j.frac[Planning], 0.5)
	near(t, "planning at the measured rate", j.expect[Planning], 2)

	// The planning pass found a longer timeline. Capture workers start their
	// browsers about as fast as the first one did.
	j.Begin(Frames)
	j.FramesDone(0, 240)
	prior := 8 + 240*defaultCaptureSecondsPerFrame/2
	near(t, "capture prior", j.expect[Frames], prior)

	// Ten seconds of browser start before the first frame don't count
	// toward the capture rate: 50 more frames in 5s is 0.1s a frame.
	c.advance(10 * time.Second)
	j.FramesDone(10, 240)
	c.advance(5 * time.Second)
	j.FramesDone(60, 240)
	near(t, "capture fraction", j.frac[Frames], 0.25)
	measuredLeft, expectedLeft := 180*0.1, prior-15
	near(t, "capture estimate", j.expect[Frames], 15+0.75*expectedLeft+0.25*measuredLeft)
	if s := j.Snapshot(c.now()); s.Detail != "frame 60/240" || s.Stage != Frames {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestNilJobIsSafe(t *testing.T) {
	var j *Job // what Current returns in serve mode
	j.Begin(Recording)
	j.PlanNarration(1)
	j.ClipDone(0, 100)
	j.PlanRecording([]float64{1})
	j.ActionStarted(0)
	j.PlanDeterministic([]float64{1}, 2)
	j.PlanningFrame(5)
	j.FramesDone(5, 10)
	j.SetFraction(0.5, time.Second)
	j.Finish("x")
	if j.SpeechMs() != nil || j.RecordedSeconds() != 0 {
		t.Fatal("nil job returned data")
	}
}
