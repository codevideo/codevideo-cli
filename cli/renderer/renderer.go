// Package renderer owns the terminal while a CLI render runs.
//
// It repaints a block of progress bars in place on stderr (one bar, or a
// course's overall bar with one bar per lesson below it), prints log lines and
// status messages above that block, and when stderr isn't a terminal (CI,
// pipes, TERM=dumb) prints plain milestone lines instead of animating. Results
// go to stdout from the caller, so `out=$(codevideo ...)` captures only those.
package renderer

import (
	"fmt"
	"io"
	"math"
	"os"
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
	labelWidth     = 14
)

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
	quiet    bool
	width    int
	now      func() time.Time // nil means time.Now; tests set a fake clock
	job      *progress.Job
	course   *progress.Course
	drawn    int
	stop     chan struct{}
	stopped  chan struct{}
	last     map[string]milestoneState
	onReport func(Milestone)
}

type milestoneState struct {
	stage   progress.Stage
	quarter int
}

var screen = &ui{out: os.Stderr, width: 100}

// Configure picks the output stream. quiet hides the bars and status lines;
// log lines still print.
func Configure(out *os.File, quiet bool) {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	screen.out, screen.quiet = out, quiet
	screen.tty = term.IsTerminal(int(out.Fd())) && os.Getenv("TERM") != "dumb" && enableVT(out)
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

// Println prints a line above the bars. Log lines use it; the log level
// already decided whether they show.
func Println(line string) {
	screen.mu.Lock()
	defer screen.mu.Unlock()
	screen.println(line)
}

// Status prints a status line above the bars unless output is quiet.
func Status(line string) {
	screen.mu.Lock()
	quiet := screen.quiet
	screen.mu.Unlock()
	if !quiet {
		Println(line)
	}
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
	u.job, u.course, u.last = j, c, map[string]milestoneState{}
	u.stop, u.stopped = make(chan struct{}), make(chan struct{})
	stop, stopped := u.stop, u.stopped
	u.mu.Unlock()

	every := plainCheckGap
	if u.tty {
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
func (u *ui) report() []Milestone {
	now := u.clock()
	var milestones []Milestone
	emit := func(key string, s progress.Snapshot, lesson bool) {
		if !s.Started || s.Stage == progress.Waiting {
			return
		}
		// Quarters come from the shown (rounded) percentage, so "[ 75%]" is
		// never posted twice from 74.6% and 75.1%.
		percent := int(math.Round(s.Fraction * 100))
		state := milestoneState{stage: s.Stage, quarter: percent / 25}
		previous, seen := u.last[key]
		if seen && previous == state {
			return
		}
		u.last[key] = state
		// Cadence: every stage change and every 25%. Could be tweaked later,
		// for example per lesson only, or every 10% for long courses.
		prefix := ""
		if lesson {
			// A lesson's own times include waiting for the other lessons.
			prefix = s.Label + " · "
		}
		m := Milestone{Label: s.Label, Snapshot: s, text: fmt.Sprintf("[%3d%%] %s%s", percent, prefix, describe(s, !lesson))}
		if !u.tty && !u.quiet {
			fmt.Fprintln(u.out, m.text)
		}
		milestones = append(milestones, m)
	}
	if u.course != nil {
		overall, lessons := u.course.Snapshot(now)
		for i, lesson := range lessons {
			emit(fmt.Sprintf("lesson-%d", i), lesson, true)
		}
		emit("course", overall, false)
	} else if u.job != nil {
		emit("job", u.job.Snapshot(now), false)
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
