// Package renderer owns the terminal while a CLI render runs.
//
// It repaints a block of progress bars in place on stderr (one bar, or a
// course's overall bar with one bar per lesson below it), prints log lines and
// status messages above that block, and when stderr isn't a terminal (CI,
// pipes, TERM=dumb) prints plain milestone lines instead of animating. Results
// go to stdout from the caller, so `out=$(codevideo ...)` captures only those.
//
// With --progress json, every line it writes is a JSON object instead, for
// programs that run the CLI: progress at the same milestones and at least once
// a second, log lines, and at the end the saved videos or the error.
package renderer

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/codevideo/codevideo-cli/cli/progress"
	"golang.org/x/term"
)

const (
	// Bars shrink toward minBarWidth before the text after them gets cut.
	barWidth       = 32
	lessonBarWidth = 22
	minBarWidth    = 12
	repaintEvery   = 100 * time.Millisecond
	plainCheckGap  = 500 * time.Millisecond
	heartbeatEvery = time.Second // JSON progress lines while a stage runs
	labelWidth     = 14
)

// Format is how progress is written.
type Format string

const (
	// Human draws bars on a terminal and prints plain milestone lines elsewhere.
	Human Format = "human"
	// JSON writes one JSON object per line, for programs that run the CLI.
	JSON Format = "json"
)

// Options set up the renderer for a CLI run.
type Options struct {
	Format  Format
	Quiet   bool   // hide progress and status lines; log lines and errors still print
	LogPath string // the log file, which a failure points to
}

// Milestone is a stage change or a 25% step, used for plain output and Slack.
type Milestone struct {
	Label    string
	Snapshot progress.Snapshot
	text     string
}

// Text is the milestone as one line, as in
// "[ 38%] Recording · action 4/12 · 0:42 / 1:50".
func (m Milestone) Text() string { return m.text }

type ui struct {
	mu       sync.Mutex
	out      io.Writer
	tty      bool
	json     bool
	quiet    bool
	logPath  string
	width    int
	now      func() time.Time // nil means time.Now; tests set a fake clock
	job      *progress.Job
	course   *progress.Course
	drawn    int
	stop     chan struct{}
	stopped  chan struct{}
	last     map[string]milestoneState
	wrote    map[string]time.Time // when each bar last got a JSON line
	onReport func(Milestone)
}

type milestoneState struct {
	stage   progress.Stage
	quarter int
}

var screen = &ui{out: os.Stderr, width: 100}

// Configure picks the output stream and format. JSON lines go out as they
// are, even to a terminal.
func Configure(out *os.File, options Options) {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	screen.out, screen.quiet, screen.logPath = out, options.Quiet, options.LogPath
	screen.json = options.Format == JSON
	screen.tty = !screen.json && term.IsTerminal(int(out.Fd())) && os.Getenv("TERM") != "dumb" && enableVT(out)
	if w, _, err := term.GetSize(int(out.Fd())); err == nil && w > 20 {
		screen.width = w
	}
}

// OnMilestone registers a callback for stage changes and 25% steps. It runs
// on the repaint goroutine, outside the renderer's lock, so it may log; it
// should hand slow work (network calls) off rather than hold up the bars.
func OnMilestone(fn func(Milestone)) {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	screen.onReport = fn
}

// TrackJob starts drawing a single video's bar.
func TrackJob(j *progress.Job) { screen.track(j, nil) }

// TrackCourse starts drawing a course's overall bar and its lesson bars.
func TrackCourse(c *progress.Course) { screen.track(nil, c) }

// Log prints a log line above the bars: an info line as it is, other levels
// prefixed ("warning: ..."). The log level already decided whether it shows.
// With JSON it is an object keyed by its level instead ({"warning": "..."});
// error lines, fatal and panic too, are {"error": "...", "log": "<log file>"}.
func Log(level, message string) {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	screen.log(level, message)
}

// Status prints a status line above the bars unless output is quiet. JSON
// output leaves status lines out.
func Status(line string) {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	if !screen.quiet && !screen.json {
		screen.println(line)
	}
}

// Result reports a saved video, after Stop. With JSON it writes
// {"result": "<absolute path>"}, with "lesson" (from 1) in a course; for
// people the caller prints its "saved to" line on stdout instead.
func Result(path string, lesson int) {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	if !screen.json {
		return
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	screen.writeJSON(resultLine{Result: path, Lesson: lesson})
}

// Failed follows a failure's error line with where the log is. A JSON error
// line names the log itself.
func Failed() {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	if !screen.json && screen.logPath != "" {
		screen.println("See the log for details: " + screen.logPath)
	}
}

// Cancelled reports a render stopped by Ctrl-C or SIGTERM: "Cancelled", or
// with JSON an error line.
func Cancelled() {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	if screen.json {
		screen.writeError("cancelled")
		return
	}
	screen.println("Cancelled")
}

// Stop draws the final state and moves the cursor below it.
func Stop() {
	screen.mu.Lock()
	stop, stopped := screen.stop, screen.stopped
	screen.stop = nil
	screen.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-stopped
	screen.mu.Lock()
	milestones := screen.finish()
	callback := screen.onReport
	screen.mu.Unlock()
	deliver(callback, milestones)
}

func deliver(callback func(Milestone), milestones []Milestone) {
	if callback == nil {
		return
	}
	for _, m := range milestones {
		callback(m)
	}
}

func (u *ui) track(j *progress.Job, c *progress.Course) {
	u.mu.Lock()
	if u.stop != nil {
		u.mu.Unlock()
		Stop()
		u.mu.Lock()
	}
	u.job, u.course, u.last, u.wrote = j, c, map[string]milestoneState{}, map[string]time.Time{}
	u.stop, u.stopped = make(chan struct{}), make(chan struct{})
	stop, stopped := u.stop, u.stopped
	u.mu.Unlock()

	every := plainCheckGap
	if u.tty || u.json {
		// Bars animate smoothly, and JSON lines catch even a short stage.
		every = repaintEvery
	}
	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				u.mu.Lock()
				milestones := u.tick()
				callback := u.onReport
				u.mu.Unlock()
				deliver(callback, milestones)
			}
		}
	}()
}

// tick reports new milestones and repaints the bars; the caller holds the lock.
func (u *ui) tick() []Milestone {
	milestones := u.report()
	if u.tty && !u.quiet {
		u.erase()
		u.draw()
	}
	return milestones
}

// finish draws the final state and moves the cursor below it; the caller
// holds the lock.
func (u *ui) finish() []Milestone {
	milestones := u.report()
	if u.tty && !u.quiet {
		u.erase()
		u.draw()
		if u.drawn > 0 {
			fmt.Fprintln(u.out)
		}
	}
	u.drawn = 0
	u.job, u.course = nil, nil
	return milestones
}

// println prints a line above the bars; the caller holds the lock. Unlike
// the bars, the line may wrap: erase only clears the rows the bars drew.
func (u *ui) println(line string) {
	u.erase()
	fmt.Fprintln(u.out, line)
	u.draw()
}

// log prints a log line; the caller holds the lock.
func (u *ui) log(level, message string) {
	message = strings.TrimRight(message, "\n")
	switch {
	case !u.json:
		u.println(logLine(level, message))
	case level == "error" || level == "fatal" || level == "panic":
		u.writeError(message)
	default:
		u.writeJSON(map[string]string{level: message})
	}
}

func logLine(level, message string) string {
	if level == "info" {
		return message
	}
	return level + ": " + message
}

// progressLine, resultLine and errorLine are the JSON lines. Times are in
// seconds.
type progressLine struct {
	Stage    progress.Stage `json:"stage"`
	Fraction float64        `json:"fraction"`
	Elapsed  float64        `json:"elapsed"`
	Total    float64        `json:"total"`
	Detail   string         `json:"detail"`
	Lesson   int            `json:"lesson,omitempty"`
}

type resultLine struct {
	Result string `json:"result"`
	Lesson int    `json:"lesson,omitempty"`
}

type errorLine struct {
	Error string `json:"error"`
	Log   string `json:"log,omitempty"`
}

func (u *ui) writeError(message string) {
	u.writeJSON(errorLine{Error: message, Log: u.logPath})
}

// writeJSON writes v as one line; the caller holds the lock.
func (u *ui) writeJSON(v any) {
	encoder := json.NewEncoder(u.out)
	encoder.SetEscapeHTML(false)
	encoder.Encode(v)
}

// writeProgress writes a snapshot as a JSON line unless output is quiet;
// lesson numbers a course's lesson from 1, and is 0 otherwise.
func (u *ui) writeProgress(s progress.Snapshot, lesson int) {
	if u.quiet {
		return
	}
	u.writeJSON(progressLine{
		Stage:    s.Stage,
		Fraction: round(s.Fraction, 1000),
		Elapsed:  round(s.Elapsed.Seconds(), 10),
		Total:    round(s.Total.Seconds(), 10),
		Detail:   s.Detail,
		Lesson:   lesson,
	})
}

// round rounds x to the nearest 1/per.
func round(x, per float64) float64 {
	return math.Round(x*per) / per
}

func (u *ui) clock() time.Time {
	if u.now != nil {
		return u.now()
	}
	return time.Now()
}

// lines renders the current block; the caller holds the lock.
func (u *ui) lines() []string {
	now := u.clock()
	if u.course != nil {
		overall, lessons := u.course.Snapshot(now)
		text := describe(overall, true)
		out := []string{ProgressBar(overall.Fraction*100, u.barWidth(barWidth, 2+runes(text))) + "  " + text}
		// One width for every lesson bar keeps them lined up.
		digits := len(strconv.Itoa(len(lessons)))
		texts, longest := make([]string, len(lessons)), 0
		for i, lesson := range lessons {
			texts[i] = describe(lesson, false)
			longest = max(longest, runes(texts[i]))
		}
		width := u.barWidth(lessonBarWidth, 2+digits+1+labelWidth+1+2+longest)
		for i, lesson := range lessons {
			name := truncate(lesson.Label, labelWidth)
			out = append(out, fmt.Sprintf("  %*d %-*s %s  %s", digits, i+1, labelWidth, name, ProgressBar(lesson.Fraction*100, width), texts[i]))
		}
		return out
	}
	if u.job != nil {
		s := u.job.Snapshot(now)
		text := describe(s, true)
		return []string{ProgressBar(s.Fraction*100, u.barWidth(barWidth, 2+runes(text))) + "  " + text}
	}
	return nil
}

// barWidth sizes a bar for a line whose other parts take used columns: up to
// widest, shrinking to minBarWidth on a narrow terminal before fit cuts the
// text. ProgressBar's output is one column wider than the width it's given.
func (u *ui) barWidth(widest, used int) int {
	return max(min(widest, u.width-1-used-1), minBarWidth)
}

func runes(s string) int { return utf8.RuneCountInString(s) }

func (u *ui) draw() {
	if !u.tty || u.quiet {
		return
	}
	lines := u.lines()
	for i := range lines {
		lines[i] = u.fit(lines[i])
	}
	fmt.Fprint(u.out, strings.Join(lines, "\n"))
	u.drawn = len(lines)
}

// erase clears the drawn block and leaves the cursor at its first column.
func (u *ui) erase() {
	if !u.tty || u.drawn == 0 {
		return
	}
	fmt.Fprint(u.out, "\r\033[2K")
	for i := 1; i < u.drawn; i++ {
		fmt.Fprint(u.out, "\033[1A\033[2K")
	}
	u.drawn = 0
}

// report finds new milestones and, when the bars aren't animated, prints
// them as plain lines; the caller passes them to the milestone callback.
// JSON output gets a line per milestone too, and one a second while a stage
// runs.
func (u *ui) report() []Milestone {
	now := u.clock()
	if u.last == nil {
		u.last = map[string]milestoneState{}
	}
	if u.wrote == nil {
		u.wrote = map[string]time.Time{}
	}
	var milestones []Milestone
	// lesson numbers a course's lesson from 1, and is 0 otherwise.
	emit := func(key string, s progress.Snapshot, lesson int) {
		if !s.Started || s.Stage == progress.Waiting {
			return
		}
		// Quarters come from the shown (rounded) percentage, so "[ 75%]" is
		// never posted twice from 74.6% and 75.1%.
		percent := int(math.Round(s.Fraction * 100))
		state := milestoneState{stage: s.Stage, quarter: percent / 25}
		previous, seen := u.last[key]
		changed := !seen || previous != state
		// The heartbeat lets a program tell a long stage from a stuck one.
		if u.json && (changed || (s.Stage != progress.Done && now.Sub(u.wrote[key]) >= heartbeatEvery)) {
			u.writeProgress(s, lesson)
			u.wrote[key] = now
		}
		if !changed {
			return
		}
		u.last[key] = state
		// Cadence: every stage change and every 25%. Could be tweaked later,
		// for example per lesson only, or every 10% for long courses.
		prefix := ""
		if lesson > 0 {
			// A lesson's own times include waiting for the other lessons.
			prefix = s.Label + " · "
		}
		m := Milestone{Label: s.Label, Snapshot: s, text: fmt.Sprintf("[%3d%%] %s%s", percent, prefix, describe(s, lesson == 0))}
		if !u.tty && !u.json && !u.quiet {
			fmt.Fprintln(u.out, m.text)
		}
		milestones = append(milestones, m)
	}
	if u.course != nil {
		overall, lessons := u.course.Snapshot(now)
		for i, lesson := range lessons {
			emit(fmt.Sprintf("lesson-%d", i), lesson, i+1)
		}
		emit("course", overall, 0)
	} else if u.job != nil {
		emit("job", u.job.Snapshot(now), 0)
	}
	return milestones
}

// describe is the text after a bar: stage, detail and times, as in
// "Recording · action 4/12 · 0:42 / 2:05" or "Done · 2:01".
func describe(s progress.Snapshot, withTime bool) string {
	if !s.Started {
		return string(progress.Waiting)
	}
	text := string(s.Stage)
	if s.Detail != "" && s.Stage != progress.Done {
		text += " · " + s.Detail
	}
	switch {
	case !withTime:
	case s.Stage == progress.Done:
		text += " · " + Clock(s.Elapsed)
	default:
		text += " · " + Clock(s.Elapsed) + " / " + Clock(s.Total)
	}
	return text
}

// Clock formats a duration as m:ss, or h:mm:ss from an hour on.
func Clock(d time.Duration) string {
	seconds := int(d.Round(time.Second).Seconds())
	if seconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}

// fit cuts a line to the terminal width so it never wraps, which would break
// the cursor arithmetic in erase.
func (u *ui) fit(line string) string {
	if !u.tty {
		return line
	}
	return truncate(line, u.width-1)
}

func truncate(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max == 1 {
		return string(runes[:1])
	}
	return strings.TrimRight(string(runes[:max-1]), " ") + "…"
}

// ProgressBar renders a progress bar in the CLI based on a percentage value
// It returns a string in the format: [===== ] XX%
// where the number of "=" characters represents the progress percentage
// totalWidth defines the total width of the progress bar including brackets
func ProgressBar(percentage float64, totalWidth int) string {
	// Validate input
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}
	if totalWidth < 7 { // Minimum width needed for "[] 100%"
		totalWidth = 32 // Default to 32 characters wide
	}

	// Calculate the available width for the progress indicators
	// Subtract 2 for the brackets and 4 for the percentage display (e.g., " 45%")
	barWidth := totalWidth - 6

	// Calculate how many "=" characters to display
	completedWidth := int((percentage / 100) * float64(barWidth))

	// Build the progress bar
	bar := "["
	bar += strings.Repeat("=", completedWidth)
	bar += strings.Repeat(" ", barWidth-completedWidth)
	bar += "]"

	// Format the percentage (right-aligned)
	percentStr := fmt.Sprintf(" %3.0f%%", percentage)

	return bar + percentStr
}
