package generator

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/codevideo/codevideo-cli/types"
)

// kokoroStandIn serves the self-hosted TTS API and counts the lines it was asked for.
func kokoroStandIn(t *testing.T) *atomic.Int32 {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Write([]byte("generated mp3"))
	}))
	t.Cleanup(server.Close)
	t.Setenv("CODEVIDEO_TTS_PROVIDER", "kokoro")
	t.Setenv("TTS_SERVICE_URL", server.URL)
	return &requests
}

func writeTakes(t *testing.T, dir, json string) string {
	t.Helper()
	path := filepath.Join(dir, "takes.json")
	if err := os.WriteFile(path, []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeClip(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTakesAreUsedInsteadOfGeneratingSpeech(t *testing.T) {
	requests := kokoroStandIn(t)
	dir := t.TempDir()
	writeClip(t, filepath.Join(dir, "clips", "hello.mp3"), "take one")
	writeClip(t, filepath.Join(dir, "bye.wav"), "take two")
	takes, err := LoadAudioTakes(writeTakes(t, dir, `[
		{"text": "Hello there.", "path": "clips/hello.mp3"},
		{"text": "Bye now.", "path": "`+filepath.ToSlash(filepath.Join(dir, "bye.wav"))+`"}
	]`))
	if err != nil {
		t.Fatal(err)
	}

	items, err := generateAudioItems(context.Background(), []types.Action{
		{Name: "author-speak-before", Value: "Hello there."},
		{Name: "editor-type", Value: "const a = 1;"},
		{Name: "author-speak-after", Value: "Bye now."},
	}, takes)
	if err != nil {
		t.Fatal(err)
	}

	if requests.Load() != 0 {
		t.Fatalf("the TTS service was called %d times; every line had a take", requests.Load())
	}
	want := []struct {
		index int
		url   string
	}{
		{0, "data:audio/mpeg;base64," + base64.StdEncoding.EncodeToString([]byte("take one"))},
		{2, "data:audio/wav;base64," + base64.StdEncoding.EncodeToString([]byte("take two"))},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d audio items, want %d", len(items), len(want))
	}
	for i, w := range want {
		if items[i].ActionIndex == nil || *items[i].ActionIndex != w.index || items[i].Mp3Url != w.url {
			t.Errorf("item %d = %+v, want action %d with %s", i, items[i], w.index, w.url)
		}
	}
}

func TestLinesWithoutATakeAreStillGenerated(t *testing.T) {
	requests := kokoroStandIn(t)
	dir := t.TempDir()
	writeClip(t, filepath.Join(dir, "a.mp3"), "take")
	takes, err := LoadAudioTakes(writeTakes(t, dir, `[{"text": "With a take.", "path": "a.mp3"}]`))
	if err != nil {
		t.Fatal(err)
	}

	items, err := generateAudioItems(context.Background(), []types.Action{
		{Name: "author-speak-before", Value: "With a take."},
		{Name: "author-speak-before", Value: "Without one."},
	}, takes)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("the TTS service was called %d times, want 1", requests.Load())
	}
	if got := items[1].Mp3Url; got != "data:audio/mpeg;base64,"+base64.StdEncoding.EncodeToString([]byte("generated mp3")) {
		t.Fatalf("the line without a take got %s", got)
	}
}

func TestDataURITakesPassThrough(t *testing.T) {
	kokoroStandIn(t)
	uri := "data:audio/mpeg;base64," + base64.StdEncoding.EncodeToString([]byte("inline"))
	takes, err := LoadAudioTakes(writeTakes(t, t.TempDir(), `[{"text": "Inline.", "mp3Url": "`+uri+`"}]`))
	if err != nil {
		t.Fatal(err)
	}
	items, err := generateAudioItems(context.Background(), []types.Action{{Name: "author-speak-during", Value: "Inline."}}, takes)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Mp3Url != uri {
		t.Fatalf("got %s", items[0].Mp3Url)
	}
}

func TestFileURLTakesReadTheFile(t *testing.T) {
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mp3")
	writeClip(t, clip, "from a file URL")
	takes, err := LoadAudioTakes(writeTakes(t, dir, `[{"text": "Hi.", "mp3Url": "file://`+filepath.ToSlash(clip)+`"}]`))
	if err != nil {
		t.Fatal(err)
	}
	url, audio, err := takes["Hi."].resolve()
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "from a file URL" || !strings.HasPrefix(url, "data:audio/mpeg;base64,") {
		t.Fatalf("got %q / %s", audio, url)
	}
}

func TestBadTakesAreRefusedBeforeTheRender(t *testing.T) {
	dir := t.TempDir()
	writeClip(t, filepath.Join(dir, "a.mp3"), "a")
	writeClip(t, filepath.Join(dir, "b.mp3"), "b")
	writeClip(t, filepath.Join(dir, "empty.mp3"), "")
	for name, tc := range map[string]struct{ json, want string }{
		"not an array":         {`{"text": "x"}`, "JSON array"},
		"no text":              {`[{"text": " ", "path": "a.mp3"}]`, "has no text"},
		"no source":            {`[{"text": "x"}]`, "either a path or an mp3Url"},
		"two sources":          {`[{"text": "x", "path": "a.mp3", "mp3Url": "data:audio/mpeg;base64,AA=="}]`, "either a path or an mp3Url"},
		"missing file":         {`[{"text": "x", "path": "nope.mp3"}]`, "nope.mp3"},
		"empty file":           {`[{"text": "x", "path": "empty.mp3"}]`, "not an audio file"},
		"two takes, same text": {`[{"text": "x", "path": "a.mp3"}, {"text": "x", "path": "b.mp3"}]`, "only one take per exact text"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadAudioTakes(writeTakes(t, dir, tc.json))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error mentioning %q", err, tc.want)
			}
		})
	}

	// The same take listed twice is fine.
	if _, err := LoadAudioTakes(writeTakes(t, dir, `[{"text": "x", "path": "a.mp3"}, {"text": "x", "path": "a.mp3"}]`)); err != nil {
		t.Fatalf("a repeated identical take was refused: %v", err)
	}
}
