package speech

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The same file is codevideo-types' __tests__/fixtures/speech-tag-cases.json,
// so captions (types, in the render page) and the speech sent to a voice (here)
// always agree on what a tag is.
func TestStripTagsMatchesTheSharedFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "speech-tag-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Text     string `json:"text"`
			Stripped string `json:"stripped"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("no cases in the fixture")
	}
	for _, c := range fixture.Cases {
		if got := StripTags(c.Text); got != c.Stripped {
			t.Errorf("StripTags(%q) = %q, want %q", c.Text, got, c.Stripped)
		}
	}
}

func TestHasTags(t *testing.T) {
	cases := map[string]bool{
		"[sighs] Fine.":       true,
		"Well, [sigh] fine.":  true,
		"Index arr[0].":       false,
		"A list [1, 2, 3].":   false,
		"":                    false,
		"No brackets at all.": false,
	}
	for text, want := range cases {
		if got := HasTags(text); got != want {
			t.Errorf("HasTags(%q) = %v, want %v", text, got, want)
		}
	}
}
