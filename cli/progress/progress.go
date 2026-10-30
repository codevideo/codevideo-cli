// Package progress turns render signals into a time-weighted percentage.
//
// A video goes through stages (narration, browser start, recording or frame
// rendering, encoding). Each stage carries an expected duration in seconds, so
// the bar moves with wall time: a 30 second narration moves it as much as it
// takes instead of one action's share. Expectations are refined as real
// numbers arrive (measured clips, ffmpeg speed, frame rate).
//
// All methods are safe on a nil *Job, which is what Current returns outside
// the CLI (serve mode), so callers don't need mode checks.
package progress

import (
	"fmt"
	"sync"
	"time"
)

// Stage is the step a video is in; it is also the label next to the bar.
type Stage string

const (
	Waiting   Stage = "Waiting"
	Narration Stage = "Narration"
	Starting  Stage = "Starting browser"
	Recording Stage = "Recording"
	Closing   Stage = "Closing browser"
	Planning  Stage = "Planning timeline"
	Frames    Stage = "Rendering frames"
	Encoding  Stage = "Encoding"
	Done      Stage = "Done"
)

const (
	// First guesses, replaced by measurements as soon as they exist. Startup,
	// closing and encoding were timed on an M-series Mac: Chrome, the page and
	// the recording extension take about 11s to start and 5s to close, and
	// ffmpeg encodes the 1080p60 webm at about half of real time.
	defaultSecondsPerClip = 2.0
	defaultStartupSeconds = 10.0
	defaultClosingSeconds = 4.0
	defaultEncodeFactor   = 1.5 // encoding time per second of video
	// The recorder starts a second before playback and stops a second after it.
	recorderPaddingSeconds = 2.0

	// Deterministic renders, from render-metrics.json files: the planning pass
	// evaluates every frame without capturing it; each capture worker starts
	// its own browser, then adds a screenshot and an encode per frame (0.015s
	// to 0.11s, depending on the machine and how far it seeks); the final mux
	// copies the video while mixing in the narration. The timeline also has a
	// second of lead-in and a second after the last action.
	framesPerSecond               = 60
	deterministicPaddingSeconds   = 2.0
	defaultPlanSecondsPerFrame    = 0.005
	defaultCaptureSecondsPerFrame = 0.06 // per worker
	defaultMuxFactor              = 0.05 // mux time per second of video
)

// Snapshot is what the renderer draws.
type Snapshot struct {
	Label    string
	Fraction float64
	Stage    Stage
	Detail   string
	Elapsed  time.Duration
	Total    time.Duration
	Result   string
	Started  bool

	remaining float64 // estimated seconds left, summed by Course
}

// Job tracks one video.
type Job struct {
	mu sync.Mutex

	label      string
	started    time.Time
	stage      Stage
	stageStart time.Time
	order      []Stage
	expect     map[Stage]float64 // expected seconds per stage
	prior      map[Stage]float64 // expectation when the stage began
	frac       map[Stage]float64 // completed fraction per stage
	result     string

	clipsTotal, clipsDone int
	speechMs              map[int]float64

	actionMs      []float64
	actionIndex   int
	actionStart   time.Time
	firstActionAt time.Time // when the page started the first action

	workers            int
	planned            int // frames evaluated by the deterministic planning pass
	frames, framesDone int
	firstFrames        int       // frames captured at the first capture report
	firstFrameAt       time.Time // when frames were first captured
	startup            float64   // seconds the browser took to start

	recorded float64 // seconds the recording stage actually ran
	shown    float64 // last fraction drawn, so the bar never moves backwards
}

// NewJob creates a job; label names it in a course ("Intro", "Variables").
func NewJob(label string) *Job {
	return &Job{label: label, stage: Waiting, expect: map[Stage]float64{}, prior: map[Stage]float64{}, frac: map[Stage]float64{}, speechMs: map[int]float64{}}
}

var (
	currentMu sync.Mutex
	current   *Job
	// clock is swapped in tests.
	clock = time.Now
)

// SetClock replaces the clock jobs read and returns a function that restores
// the previous one. Tests use it to move time by hand.
func SetClock(now func() time.Time) (restore func()) {
	previous := clock
	clock = now
	return func() { clock = previous }
}

// SetCurrent marks the job that the generator, recorder and encoder report to.
func SetCurrent(j *Job) {
	currentMu.Lock()
	defer currentMu.Unlock()
	current = j
}

// Current returns the job being worked on, or nil outside the CLI.
func Current() *Job {
	currentMu.Lock()
	defer currentMu.Unlock()
	return current
}

func (j *Job) plan(stage Stage, seconds float64) {
	if _, seen := j.expect[stage]; !seen {
		j.order = append(j.order, stage)
	}
	j.expect[stage] = seconds
}

// Begin moves the job into a stage; stages before it count as finished.
// Beginning the stage the job is already in does nothing.
func (j *Job) Begin(stage Stage) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.stage == stage {
		return
	}
	now := clock()
	if j.started.IsZero() {
		j.started = now
	}
	if _, seen := j.expect[stage]; !seen {
		j.plan(stage, j.defaultExpectation(stage))
	}
	for _, s := range j.order {
		if s == stage {
			break
		}
		j.frac[s] = 1
	}
	switch j.stage {
	case Starting:
		j.startup = j.stageSeconds(now)
	case Recording:
		j.recorded = j.stageSeconds(now)
	}
	if stage == Frames {
		j.expect[Frames] = j.framesPrior()
	}
	if stage == Encoding && j.recorded > 0 {
		// Encoding scales with the video that was actually recorded.
		j.expect[Encoding] = j.recorded * defaultEncodeFactor
	}
	j.prior[stage] = j.expect[stage]
	j.stage, j.stageStart = stage, now
}

func (j *Job) defaultExpectation(stage Stage) float64 {
	switch stage {
	case Starting:
		return defaultStartupSeconds
	case Closing:
		return defaultClosingSeconds
	case Encoding:
		return j.expect[Recording] * defaultEncodeFactor
	}
	return 0
}

func (j *Job) stageSeconds(now time.Time) float64 {
	if j.stageStart.IsZero() {
		return 0
	}
	return now.Sub(j.stageStart).Seconds()
}

// PlanNarration announces how many narration clips will be generated.
func (j *Job) PlanNarration(clips int) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.clipsTotal = clips
	j.plan(Narration, float64(clips)*defaultSecondsPerClip)
}

// ClipDone records a finished narration clip and its measured length (0 if
// unknown). The average time per clip so far refines the narration estimate.
func (j *Job) ClipDone(actionIndex int, durationMs float64) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.clipsDone++
	if durationMs > 0 {
		j.speechMs[actionIndex] = durationMs
	}
	if j.clipsTotal > 0 {
		j.frac[Narration] = float64(j.clipsDone) / float64(j.clipsTotal)
		if spent := j.stageSeconds(clock()); spent > 0 {
			j.expect[Narration] = spent / float64(j.clipsDone) * float64(j.clipsTotal)
		}
	}
}

// SpeechMs returns the measured narration lengths by action index.
func (j *Job) SpeechMs() map[int]float64 {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	copied := make(map[int]float64, len(j.speechMs))
	for k, v := range j.speechMs {
		copied[k] = v
	}
	return copied
}

// PlanRecording sets the expected playback time of each action, which drives
// both the recording estimate and the interpolation inside it.
func (j *Job) PlanRecording(actionMs []float64) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.actionMs = actionMs
	total := recorderPaddingSeconds
	for _, ms := range actionMs {
		total += ms / 1000
	}
	j.plan(Starting, defaultStartupSeconds)
	j.plan(Recording, total)
	j.plan(Closing, defaultClosingSeconds)
	j.plan(Encoding, total*defaultEncodeFactor)
}

// ActionStarted reports that the page began playing action index; index
// equal to the action count means playback finished.
func (j *Job) ActionStarted(index int) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	now := clock()
	if j.firstActionAt.IsZero() {
		j.firstActionAt = now
	}
	j.actionIndex, j.actionStart = index, now
}

// PlanDeterministic sets up a deterministic render. The planning pass and the
// capture both walk every frame of the timeline, whose length comes from the
// action plan until the capture reports the real frame count.
func (j *Job) PlanDeterministic(actionMs []float64, workers int) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	seconds := deterministicPaddingSeconds
	for _, ms := range actionMs {
		seconds += ms / 1000
	}
	j.workers = max(workers, 1)
	j.frames = int(seconds * framesPerSecond)
	j.plan(Starting, defaultStartupSeconds)
	j.plan(Planning, float64(j.frames)*defaultPlanSecondsPerFrame)
	j.plan(Frames, j.framesPrior())
	j.plan(Encoding, seconds*defaultMuxFactor)
}

// framesPrior is the capture estimate before frames arrive: the workers start
// their browsers (about as long as the first one took), then share the frames.
func (j *Job) framesPrior() float64 {
	boot := j.startup
	if boot <= 0 {
		boot = defaultStartupSeconds
	}
	return boot + float64(j.frames)*defaultCaptureSecondsPerFrame/float64(max(j.workers, 1))
}

// PlanningFrame reports how far the planning pass has evaluated. The frame
// count is an estimate until it finishes, so the stage stops short of done.
func (j *Job) PlanningFrame(frame int) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	j.planned = frame
	if j.frames > 0 && frame > 0 {
		j.frac[Planning] = min(float64(frame)/float64(j.frames), 0.99)
		if spent := j.stageSeconds(clock()); spent > 0 {
			j.expect[Planning] = spent / float64(frame) * float64(max(j.frames, frame+1))
		}
	}
}

// FramesDone reports captured frames out of total. The capture rate, timed
// from the first captured frame so the workers' browser start doesn't count,
// refines the estimate for the rest.
func (j *Job) FramesDone(done, total int) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	now := clock()
	if total > 0 && total != j.frames {
		j.frames = total
		if j.firstFrameAt.IsZero() {
			j.expect[Frames] = j.framesPrior()
			j.prior[Frames] = j.expect[Frames]
		}
	}
	if done > 0 && j.firstFrameAt.IsZero() {
		j.firstFrameAt, j.firstFrames = now, done
	}
	j.framesDone = done
	if j.frames <= 0 {
		return
	}
	fraction := min(float64(done)/float64(j.frames), 1)
	j.frac[Frames] = fraction
	if done > j.firstFrames {
		rate := now.Sub(j.firstFrameAt).Seconds() / float64(done-j.firstFrames)
		j.refine(Frames, fraction, float64(j.frames-done)*rate)
	}
}

// SetFraction reports progress inside the current stage (encoding uses it),
// with a measured estimate of the time left, or 0 when there is none yet.
// Early measurements are noisy (ffmpeg's reported speed climbs for its first
// seconds), so the time left blends the stage's prior expectation with the
// measurement, trusting the measurement more as the stage completes.
func (j *Job) SetFraction(fraction float64, remaining time.Duration) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	fraction = clamp(fraction)
	if fraction > j.frac[j.stage] {
		j.frac[j.stage] = fraction
	}
	if remaining > 0 {
		j.refine(j.stage, fraction, remaining.Seconds())
	}
}

// refine blends the stage's prior expectation of the time left with a
// measured one, trusting the measurement more as the stage completes.
func (j *Job) refine(stage Stage, fraction, measuredLeft float64) {
	spent := 0.0
	if j.stage == stage {
		spent = j.stageSeconds(clock())
	}
	expectedLeft := max(j.prior[stage]-spent, 0)
	j.expect[stage] = spent + (1-fraction)*expectedLeft + fraction*measuredLeft
}

// RecordedSeconds is how long the recording stage ran; the encoder compares
// ffmpeg's position against it.
func (j *Job) RecordedSeconds() float64 {
	if j == nil {
		return 0
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	switch {
	case j.stage == Recording:
		return j.stageSeconds(clock())
	case j.recorded > 0:
		return j.recorded
	}
	return j.expect[Recording]
}

// Finish marks the video done; result is the saved file.
func (j *Job) Finish(result string) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	for _, s := range j.order {
		j.frac[s] = 1
	}
	j.stage, j.result = Done, result
}

// Snapshot computes the job's state at now. The fraction is elapsed time over
// elapsed plus estimated time left, so the percentage and "0:42 / 2:05" always
// agree. It never goes backwards when an estimate is revised upwards; the bar
// waits for real progress to catch up instead.
func (j *Job) Snapshot(now time.Time) Snapshot {
	return j.snapshot(now, false)
}

// snapshot with byWork set measures the fraction as planned work done instead
// of time. A course uses it for lesson bars: a narrated lesson waits for the
// others before it renders, and that wait isn't progress.
func (j *Job) snapshot(now time.Time, byWork bool) Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := Snapshot{Label: j.label, Stage: j.stage, Result: j.result, Started: !j.started.IsZero()}
	switch j.stage {
	case Narration:
		s.Detail = fmt.Sprintf("clip %d/%d", min(j.clipsDone+1, j.clipsTotal), j.clipsTotal)
	case Recording:
		if len(j.actionMs) > 0 {
			j.frac[Recording] = j.recordingFraction(now)
			j.expect[Recording] = j.stageSeconds(now) + j.remainingInStage(now)
			// The video is as long as the recording, so encoding follows its pace.
			j.expect[Encoding] = j.expect[Recording] * defaultEncodeFactor
			s.Detail = fmt.Sprintf("action %d/%d", min(j.actionIndex+1, len(j.actionMs)), len(j.actionMs))
		}
	case Planning:
		s.Detail = fmt.Sprintf("frame %d", j.planned)
	case Frames:
		s.Detail = fmt.Sprintf("frame %d/%d", j.framesDone, j.frames)
	}
	s.remaining = j.remainingSeconds(now)
	if s.Started {
		s.Elapsed = now.Sub(j.started)
	}
	s.Total = s.Elapsed + seconds(s.remaining)
	switch {
	case j.stage == Done:
		s.Fraction, s.Total = 1, s.Elapsed
	case byWork:
		if planned := j.plannedSeconds(); planned > 0 {
			s.Fraction = clamp(1 - s.remaining/planned)
		}
	case s.Total > 0:
		s.Fraction = clamp(s.Elapsed.Seconds() / s.Total.Seconds())
	}
	if s.Fraction < j.shown {
		s.Fraction = j.shown
	}
	j.shown = s.Fraction
	return s
}

// remainingSeconds estimates the time left: what remains of the current stage
// plus every planned stage after it.
func (j *Job) remainingSeconds(now time.Time) float64 {
	if j.stage == Done {
		return 0
	}
	remaining, after := 0.0, j.stage == Waiting
	for _, stage := range j.order {
		if after {
			remaining += j.expect[stage]
			continue
		}
		if stage == j.stage {
			remaining += j.remainingInStage(now)
			after = true
		}
	}
	return remaining
}

func (j *Job) remainingInStage(now time.Time) float64 {
	switch {
	case j.frac[j.stage] >= 1:
		return 0
	case j.stage == Recording && len(j.actionMs) > 0:
		return j.recordingLeft(now)
	}
	// Narration, frames and encoding refine expect from measured rates; a
	// stage running past its estimate keeps a sliver left until it reports done.
	return max(j.expect[j.stage]-j.stageSeconds(now), 0.3)
}

func (j *Job) plannedSeconds() float64 {
	total := 0.0
	for _, stage := range j.order {
		total += j.expect[stage]
	}
	return total
}

// recordingFraction is the share of the action plan played so far: finished
// actions count fully, the current one by elapsed time (at the measured pace)
// capped at its estimate, so the bar pauses rather than runs ahead when an
// action takes longer.
func (j *Job) recordingFraction(now time.Time) float64 {
	total, before := j.planMs(len(j.actionMs)), j.planMs(j.actionIndex)
	if total <= 0 {
		return 0
	}
	if j.actionIndex >= len(j.actionMs) {
		return 1
	}
	inside := 0.0
	if !j.actionStart.IsZero() {
		inside = min(now.Sub(j.actionStart).Seconds()*1000/j.recordingPace(), j.actionMs[j.actionIndex])
	}
	return clamp((before + inside) / total)
}

// recordingPace is how much longer playback takes than its plan, measured on
// the actions finished so far. Capturing slows the page down (each keystroke
// took about 2.4 times its pause on an M-series Mac), so this replaces a fixed
// guess. It leans on the plan until a few seconds of actions have played.
func (j *Job) recordingPace() float64 {
	planned := j.planMs(j.actionIndex) / 1000
	if planned <= 0 || j.firstActionAt.IsZero() {
		return 1
	}
	measured := min(max(j.actionStart.Sub(j.firstActionAt).Seconds()/planned, 0.5), 5)
	weight := planned / (planned + 3)
	return (1 - weight) + weight*measured
}

// recordingLeft is the rest of the action plan at the measured pace, plus the
// recorder's second after the last action and whatever is left of the second
// before the first.
func (j *Job) recordingLeft(now time.Time) float64 {
	left := j.planMs(len(j.actionMs)) / 1000 * (1 - j.frac[Recording]) * j.recordingPace()
	left += recorderPaddingSeconds / 2
	if j.firstActionAt.IsZero() {
		left += max(recorderPaddingSeconds/2-j.stageSeconds(now), 0)
	}
	return left
}

// planMs sums the estimates of the first n actions.
func (j *Job) planMs(n int) float64 {
	total := 0.0
	for _, ms := range j.actionMs[:min(n, len(j.actionMs))] {
		total += ms
	}
	return total
}

// Course combines lesson jobs into one overall bar.
type Course struct {
	Jobs    []*Job
	started time.Time
	shown   float64
}

// NewCourse starts the clock for the overall bar.
func NewCourse(jobs []*Job) *Course {
	return &Course{Jobs: jobs, started: clock()}
}

// Snapshot returns the overall state followed by one snapshot per lesson.
// Lessons render one after another, so the time left is the sum of every
// lesson's time left, and the overall fraction is elapsed over elapsed plus
// that. The lesson being worked on is the Current job.
func (c *Course) Snapshot(now time.Time) (Snapshot, []Snapshot) {
	active := Current()
	lessons := make([]Snapshot, len(c.Jobs))
	remaining, working, done := 0.0, -1, 0
	for i, job := range c.Jobs {
		s := job.snapshot(now, true)
		remaining += s.remaining
		switch {
		case s.Stage == Done:
			done++
		case job == active:
			working = i
		case s.Started:
			// Narrated, and waiting for its turn to render.
			s.Stage, s.Detail = Waiting, ""
		}
		lessons[i] = s
	}
	overall := Snapshot{Label: "Course", Stage: Waiting, Started: true, Elapsed: now.Sub(c.started)}
	if working >= 0 {
		overall.Stage = lessons[working].Stage
		overall.Detail = fmt.Sprintf("lesson %d/%d", working+1, len(c.Jobs))
	}
	overall.Total = overall.Elapsed + seconds(remaining)
	if done == len(c.Jobs) {
		overall.Stage, overall.Fraction, overall.Total = Done, 1, overall.Elapsed
	} else if overall.Total > 0 {
		overall.Fraction = clamp(overall.Elapsed.Seconds() / overall.Total.Seconds())
	}
	if overall.Fraction < c.shown {
		overall.Fraction = c.shown
	}
	c.shown = overall.Fraction
	return overall, lessons
}

func seconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}

func clamp(f float64) float64 {
	return min(max(f, 0), 1)
}
