package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rogpeppe/go-internal/testscript"
)

// jsonlines reads the last command's stderr, which --progress json makes one
// JSON object per line, and prints what it says: the stages each bar went
// through ("stages:" for the video or the course, "lesson N:" for a course's
// lessons), then the result, error and log lines in order. Paths read
// relative to $WORK with forward slashes, and saved files' timestamps as
// YYYY-MM-DD-hh-mm-ss. It fails on a line that isn't a JSON object, a progress
// line with a missing or impossible field, a fraction that goes backwards,
// progress after a result or an error, and output that doesn't end with one.
func jsonLines(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) > 0 {
		ts.Fatalf("usage: jsonlines")
	}
	work := ts.Getenv("WORK")
	show := func(path string) string {
		if !filepath.IsAbs(path) {
			ts.Fatalf("not an absolute path: %s", path)
		}
		if rel, err := filepath.Rel(work, path); err == nil {
			path = rel
		}
		return savedFilePattern.ReplaceAllString(filepath.ToSlash(path), "CodeVideo-YYYY-MM-DD-hh-mm-ss")
	}
	stages := map[int][]string{} // by lesson; 0 is the video or the course
	fractions := map[int]float64{}
	var messages []string
	ended := false    // a result or an error came, so progress may not
	endsWell := false // the last line is a result or an error
	for i, text := range strings.Split(strings.TrimSuffix(ts.ReadFile("stderr"), "\n"), "\n") {
		endsWell = false
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(text), &fields); err != nil {
			ts.Fatalf("stderr line %d is not a JSON object: %s", i+1, text)
		}
		var line struct {
			Stage    *string  `json:"stage"`
			Fraction *float64 `json:"fraction"`
			Elapsed  *float64 `json:"elapsed"`
			Total    *float64 `json:"total"`
			Detail   *string  `json:"detail"`
			Lesson   int      `json:"lesson"`
			Result   *string  `json:"result"`
			Error    *string  `json:"error"`
			Log      string   `json:"log"`
		}
		if err := json.Unmarshal([]byte(text), &line); err != nil {
			ts.Fatalf("stderr line %d: %v: %s", i+1, err, text)
		}
		switch {
		case line.Stage != nil:
			if ended {
				ts.Fatalf("progress after the end: %s", text)
			}
			if line.Fraction == nil || line.Elapsed == nil || line.Total == nil || line.Detail == nil {
				ts.Fatalf("progress line without fraction, elapsed, total or detail: %s", text)
			}
			if *line.Fraction < 0 || *line.Fraction > 1 || *line.Elapsed < 0 || *line.Total < *line.Elapsed || line.Lesson < 0 {
				ts.Fatalf("impossible progress: %s", text)
			}
			if *line.Fraction < fractions[line.Lesson] {
				ts.Fatalf("fraction went backwards from %v: %s", fractions[line.Lesson], text)
			}
			fractions[line.Lesson] = *line.Fraction
			if seen := stages[line.Lesson]; len(seen) == 0 || seen[len(seen)-1] != *line.Stage {
				stages[line.Lesson] = append(seen, *line.Stage)
			}
		case line.Result != nil:
			label := "result"
			if line.Lesson > 0 {
				label = fmt.Sprintf("result (lesson %d)", line.Lesson)
			}
			messages = append(messages, label+": "+show(*line.Result))
			ended, endsWell = true, true
		case line.Error != nil:
			messages = append(messages, "error: "+*line.Error)
			if line.Log != "" {
				messages = append(messages, "log: "+show(line.Log))
			}
			ended, endsWell = true, true
		default:
			// A log line: {"warning": "..."}.
			var level, message string
			for key, raw := range fields {
				level = key
				json.Unmarshal(raw, &message)
			}
			if len(fields) != 1 || message == "" {
				ts.Fatalf("stderr line %d is neither progress, a result, an error nor a log line: %s", i+1, text)
			}
			messages = append(messages, level+": "+message)
		}
	}
	if !endsWell {
		ts.Fatalf("stderr doesn't end with a result or an error")
	}
	out := ts.Stdout()
	lessons := make([]int, 0, len(stages))
	for lesson := range stages {
		lessons = append(lessons, lesson)
	}
	slices.Sort(lessons)
	for _, lesson := range lessons {
		label := "stages"
		if lesson > 0 {
			label = fmt.Sprintf("lesson %d", lesson)
		}
		fmt.Fprintf(out, "%s: %s\n", label, strings.Join(stages[lesson], ", "))
	}
	for _, message := range messages {
		fmt.Fprintln(out, message)
	}
}
