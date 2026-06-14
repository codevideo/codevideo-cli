package types

import (
	"encoding/json"
	"testing"
)

// These fixtures use the field names the TS codevideo-types serializes:
// "name" (not "title"), "id", "primaryLanguage". Before the parity fix the Go
// structs tagged these "title" and dropped id/primaryLanguage, so Name came
// back empty.

const tsLessonJSON = `{
  "id": "lesson-1",
  "name": "Building an MCP Server",
  "description": "A lesson",
  "actions": [
    { "name": "author-speak-before", "value": "Hello" },
    { "name": "editor-type", "value": "const x = 1;" }
  ]
}`

const tsCourseJSON = `{
  "id": "course-1",
  "name": "TypeScript Track",
  "description": "A course",
  "primaryLanguage": "typescript",
  "lessons": [
    { "id": "l1", "name": "Lesson One", "description": "", "actions": [ { "name": "editor-type", "value": "a" } ] },
    { "id": "l2", "name": "Lesson Two", "description": "", "actions": [ { "name": "editor-type", "value": "b" } ] }
  ]
}`

func TestLessonUnmarshalsTSFieldNames(t *testing.T) {
	var lesson Lesson
	if err := json.Unmarshal([]byte(tsLessonJSON), &lesson); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if lesson.ID != "lesson-1" {
		t.Errorf("ID = %q, want %q", lesson.ID, "lesson-1")
	}
	if lesson.Name != "Building an MCP Server" {
		t.Errorf("Name = %q, want %q (the TS 'name' field)", lesson.Name, "Building an MCP Server")
	}
	if len(lesson.Actions) != 2 {
		t.Errorf("Actions len = %d, want 2", len(lesson.Actions))
	}
}

func TestCourseUnmarshalsTSFieldNames(t *testing.T) {
	var course Course
	if err := json.Unmarshal([]byte(tsCourseJSON), &course); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if course.ID != "course-1" {
		t.Errorf("ID = %q, want %q", course.ID, "course-1")
	}
	if course.Name != "TypeScript Track" {
		t.Errorf("Name = %q, want %q", course.Name, "TypeScript Track")
	}
	if course.PrimaryLanguage != "typescript" {
		t.Errorf("PrimaryLanguage = %q, want %q", course.PrimaryLanguage, "typescript")
	}
	if len(course.Lessons) != 2 {
		t.Fatalf("Lessons len = %d, want 2", len(course.Lessons))
	}
	if course.Lessons[1].Name != "Lesson Two" {
		t.Errorf("nested lesson Name = %q, want %q", course.Lessons[1].Name, "Lesson Two")
	}
}

// Round-trip: marshal back out and confirm the TS field names are emitted.
func TestLessonRoundTripEmitsName(t *testing.T) {
	var lesson Lesson
	if err := json.Unmarshal([]byte(tsLessonJSON), &lesson); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	out, err := json.Marshal(lesson)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var generic map[string]interface{}
	if err := json.Unmarshal(out, &generic); err != nil {
		t.Fatalf("re-unmarshal failed: %v", err)
	}
	if _, ok := generic["name"]; !ok {
		t.Errorf("round-tripped lesson is missing a \"name\" field: %s", out)
	}
	if _, ok := generic["title"]; ok {
		t.Errorf("round-tripped lesson should NOT emit a \"title\" field: %s", out)
	}
}
