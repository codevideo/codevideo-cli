package generator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codevideo/codevideo-cli/cloud"
	"github.com/codevideo/codevideo-cli/elevenlabs"
)

// clipFakes stands in for ElevenLabs and S3, and counts how often each is used.
type clipFakes struct {
	made, uploaded, fetched int
	bucket                  map[string][]byte // public URL -> object
	failMaking              bool
}

func useClipFakes(t *testing.T) *clipFakes {
	t.Helper()
	f := &clipFakes{bucket: map[string][]byte{}}
	audio, upload, fetch := elevenLabsAudio, uploadClip, fetchClip
	t.Cleanup(func() { elevenLabsAudio, uploadClip, fetchClip = audio, upload, fetch })
	elevenLabsAudio = func(ctx context.Context, text, apiKey string, voice elevenlabs.Voice) ([]byte, error) {
		if f.failMaking {
			return nil, errors.New("quota exceeded")
		}
		f.made++
		return []byte("mp3 of " + voice.ID + " (" + voice.Model() + "): " + text), nil
	}
	uploadClip = func(ctx context.Context, s3 cloud.Config, audio []byte, filename string) (string, error) {
		f.uploaded++
		url := s3.URL(cloud.Key("v3/audio", filename))
		f.bucket[url] = audio
		return url, nil
	}
	fetchClip = func(ctx context.Context, url string) ([]byte, bool) {
		f.fetched++
		audio, ok := f.bucket[url]
		return audio, ok
	}
	return f
}

func TestWithoutS3ClipsAreEmbeddedAndReused(t *testing.T) {
	f := useClipFakes(t)
	cache := ttsCache{dir: t.TempDir(), enabled: true}
	ctx := context.Background()

	audio, url, reused, err := elevenLabsClip(ctx, "Hello there.", "key", elevenlabs.Voice{ID: "voice-a"}, cloud.Config{}, cache)
	if err != nil {
		t.Fatal(err)
	}
	if reused || f.made != 1 {
		t.Fatalf("first render: reused=%v, made=%d", reused, f.made)
	}
	if url != dataURI(audio) || !strings.HasPrefix(url, "data:audio/mpeg;base64,") {
		t.Fatalf("url = %.40q, want the clip as a data URI", url)
	}

	again, url2, reused, err := elevenLabsClip(ctx, "Hello there.", "key", elevenlabs.Voice{ID: "voice-a"}, cloud.Config{}, cache)
	if err != nil {
		t.Fatal(err)
	}
	if !reused || f.made != 1 || url2 != url || string(again) != string(audio) {
		t.Fatalf("second render: reused=%v, made=%d", reused, f.made)
	}

	// Another voice is another clip.
	if _, _, reused, _ := elevenLabsClip(ctx, "Hello there.", "key", elevenlabs.Voice{ID: "voice-b"}, cloud.Config{}, cache); reused || f.made != 2 {
		t.Fatalf("other voice: reused=%v, made=%d", reused, f.made)
	}
	if f.uploaded != 0 || f.fetched != 0 {
		t.Fatalf("used S3 without keys: uploaded=%d, fetched=%d", f.uploaded, f.fetched)
	}
	entries, _ := os.ReadDir(cache.dir)
	if len(entries) != 2 {
		t.Fatalf("cache holds %d entries, want the 2 clips and no temp files", len(entries))
	}
}

func TestWithS3TheBucketIsTheCache(t *testing.T) {
	f := useClipFakes(t)
	s3 := cloud.Config{KeyID: "k", Secret: "s", Bucket: "my-course-audio", Region: "eu-central-1"}
	cache := ttsCache{dir: t.TempDir(), enabled: true}
	ctx := context.Background()

	_, url, reused, err := elevenLabsClip(ctx, "Hello.", "key", elevenlabs.Voice{ID: "voice-a"}, s3, cache)
	if err != nil {
		t.Fatal(err)
	}
	if reused || f.made != 1 || f.uploaded != 1 {
		t.Fatalf("first render: reused=%v, made=%d, uploaded=%d", reused, f.made, f.uploaded)
	}
	if !strings.HasPrefix(url, "https://my-course-audio.s3.eu-central-1.amazonaws.com/codevideo/v3/audio/") {
		t.Fatalf("url = %q, want an object in your bucket", url)
	}

	_, url2, reused, err := elevenLabsClip(ctx, "Hello.", "key", elevenlabs.Voice{ID: "voice-a"}, s3, cache)
	if err != nil {
		t.Fatal(err)
	}
	if !reused || f.made != 1 || f.uploaded != 1 || url2 != url {
		t.Fatalf("second render: reused=%v, made=%d, uploaded=%d", reused, f.made, f.uploaded)
	}
	// With a bucket nothing piles up on the worker's disk.
	if entries, _ := os.ReadDir(cache.dir); len(entries) != 0 {
		t.Fatalf("kept %d clips locally although the bucket is the cache", len(entries))
	}
}

func TestCacheOffMakesEveryClipAgain(t *testing.T) {
	f := useClipFakes(t)
	cache := ttsCache{dir: t.TempDir()}
	s3 := cloud.Config{KeyID: "k", Secret: "s", Bucket: "b", Region: "us-east-1"}
	for _, bucket := range []cloud.Config{{}, s3} {
		for run := 0; run < 2; run++ {
			if _, _, reused, err := elevenLabsClip(context.Background(), "Hi.", "key", elevenlabs.Voice{ID: "v"}, bucket, cache); err != nil || reused {
				t.Fatalf("reused=%v, err=%v with the cache off", reused, err)
			}
		}
	}
	if f.made != 4 || f.fetched != 0 {
		t.Fatalf("made=%d, fetched=%d; want every clip made and no lookups", f.made, f.fetched)
	}
	if entries, _ := os.ReadDir(cache.dir); len(entries) != 0 {
		t.Fatal("cache off, but clips were kept")
	}
}

func TestAFailedClipIsNotKept(t *testing.T) {
	f := useClipFakes(t)
	f.failMaking = true
	cache := ttsCache{dir: t.TempDir(), enabled: true}
	_, _, _, err := elevenLabsClip(context.Background(), "Hi.", "key", elevenlabs.Voice{ID: "v"}, cloud.Config{}, cache)
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("err = %v, want the ElevenLabs error", err)
	}
	if entries, _ := os.ReadDir(cache.dir); len(entries) != 0 {
		t.Fatal("a failed clip left something in the cache")
	}
}

func TestTTSCacheFromEnv(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clips")
	t.Setenv("CODEVIDEO_TTS_CACHE_DIR", dir)
	t.Setenv("CODEVIDEO_TTS_CACHE", "")
	if c := ttsCacheFromEnv(); !c.enabled || c.dir != dir {
		t.Fatalf("default = %+v, want enabled in %s", c, dir)
	}
	for _, off := range []string{"off", "0", "false", "OFF"} {
		t.Setenv("CODEVIDEO_TTS_CACHE", off)
		if ttsCacheFromEnv().enabled {
			t.Fatalf("CODEVIDEO_TTS_CACHE=%s left the cache on", off)
		}
	}
}

func TestFetchPublicObjectOnlyHitsOnA200WithABody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/clip.mp3":
			w.Write([]byte("mp3"))
		case "/private.mp3":
			w.WriteHeader(http.StatusForbidden)
		case "/empty.mp3":
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx := context.Background()
	if body, ok := fetchPublicObject(ctx, server.URL+"/clip.mp3"); !ok || string(body) != "mp3" {
		t.Fatalf("existing clip: ok=%v, body=%q", ok, body)
	}
	for _, path := range []string{"/missing.mp3", "/private.mp3", "/empty.mp3"} {
		if _, ok := fetchPublicObject(ctx, server.URL+path); ok {
			t.Fatalf("%s counted as an existing clip", path)
		}
	}
}

func TestAClipIsReusedOnlyForTheSameModel(t *testing.T) {
	f := useClipFakes(t)
	cache := ttsCache{dir: t.TempDir(), enabled: true}
	ctx := context.Background()
	line := "[exaggerated clueless voice] uh, what is odd or even"

	if _, _, reused, err := elevenLabsClip(ctx, line, "key", elevenlabs.Voice{ID: "cole"}, cloud.Config{}, cache); err != nil || reused {
		t.Fatalf("first clip: reused=%v err=%v", reused, err)
	}
	v4 := elevenlabs.Voice{ID: "cole", ModelID: "eleven_v4"}
	if _, _, reused, err := elevenLabsClip(ctx, line, "key", v4, cloud.Config{}, cache); err != nil || reused {
		t.Fatalf("same line on eleven_v4 reused the multilingual clip: reused=%v err=%v", reused, err)
	}
	if _, _, reused, err := elevenLabsClip(ctx, line, "key", v4, cloud.Config{}, cache); err != nil || !reused {
		t.Fatalf("eleven_v4 clip made again instead of reused: reused=%v err=%v", reused, err)
	}
	if f.made != 2 {
		t.Fatalf("made %d clips, want 2", f.made)
	}
}

func TestVoiceAndModelComeFromTheEnvironment(t *testing.T) {
	t.Setenv("ELEVEN_LABS_VOICE_ID", " Tw2LVqLUUWkxqrCfFOpw ")
	t.Setenv("ELEVEN_LABS_VOICE_ID_CHRIS", "")
	t.Setenv("ELEVEN_LABS_MODEL_ID", " eleven_v4\n")
	if got := resolveTTSVoice(); got != (elevenlabs.Voice{ID: "Tw2LVqLUUWkxqrCfFOpw", ModelID: "eleven_v4"}) {
		t.Fatalf("resolveTTSVoice() = %+v", got)
	}
	t.Setenv("ELEVEN_LABS_MODEL_ID", "")
	if got := resolveTTSVoice(); got.ModelID != "" || got.Model() != "eleven_multilingual_v2" {
		t.Fatalf("with no model set: %+v speaks with %s", got, got.Model())
	}
}
