package generator

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/codevideo/codevideo-cli/cloud"
	"github.com/codevideo/codevideo-cli/constants"
	"github.com/codevideo/codevideo-cli/elevenlabs"
)

// Swapped out by tests, so no test calls ElevenLabs, S3 or the network.
var (
	elevenLabsAudio = elevenlabs.GetAudioArrayBufferElevenLabs
	uploadClip      = func(ctx context.Context, s3 cloud.Config, audio []byte, filename string) (string, error) {
		return s3.Upload(ctx, audio, "v3/audio", filename)
	}
	fetchClip = fetchPublicObject
)

// ttsCache says whether clips made before are reused, and where they're kept
// when there's no bucket.
type ttsCache struct {
	dir     string
	enabled bool
}

// ttsCacheFromEnv turns reuse on unless CODEVIDEO_TTS_CACHE is off, for a
// fresh take of every clip.
func ttsCacheFromEnv() ttsCache {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CODEVIDEO_TTS_CACHE"))) {
	case "off", "0", "false", "no":
		return ttsCache{}
	}
	return ttsCache{dir: constants.TTSCacheFolder(), enabled: true}
}

// elevenLabsClip returns one narration clip and the URL the render page plays
// it from. A clip is named by the request that makes it (elevenlabs.ClipKey),
// so one made before is reused instead of paying ElevenLabs again:
//   - with S3 keys, the bucket is the cache, and the page plays the object's
//     public URL;
//   - without them, clips are kept in the TTS cache folder and embedded in the
//     job as data URIs, so no bucket is needed at all.
func elevenLabsClip(ctx context.Context, text, apiKey, voiceID string, s3 cloud.Config, cache ttsCache) (audio []byte, url string, reused bool, err error) {
	key, err := elevenlabs.ClipKey(text, voiceID)
	if err != nil {
		return nil, "", false, err
	}
	filename := key + ".mp3"

	if s3.Enabled() {
		url = s3.URL(cloud.Key("v3/audio", filename))
		if cache.enabled {
			if existing, ok := fetchClip(ctx, url); ok {
				return existing, url, true, nil
			}
		}
		if audio, err = elevenLabsAudio(ctx, text, apiKey, voiceID); err != nil {
			return nil, "", false, fmt.Errorf("error converting text to audio: %w", err)
		}
		if url, err = uploadClip(ctx, s3, audio, filename); err != nil {
			return nil, "", false, fmt.Errorf("error uploading audio to S3: %w", err)
		}
		return audio, url, false, nil
	}

	path := filepath.Join(cache.dir, filename)
	if cache.enabled {
		if existing, err := os.ReadFile(path); err == nil && len(existing) > 0 {
			return existing, dataURI(existing), true, nil
		}
	}
	if audio, err = elevenLabsAudio(ctx, text, apiKey, voiceID); err != nil {
		return nil, "", false, fmt.Errorf("error converting text to audio: %w", err)
	}
	if cache.enabled {
		if err := writeFileAtomic(path, audio); err != nil {
			log.Warnf("Could not keep the narration clip in the TTS cache: %v", err)
		}
	}
	return audio, dataURI(audio), false, nil
}

func dataURI(audio []byte) string {
	return "data:audio/mpeg;base64," + base64.StdEncoding.EncodeToString(audio)
}

// fetchPublicObject downloads an object from its public URL, as the render
// page would. Anything but a 200 with a body is a miss, and the clip is made
// and uploaded again.
func fetchPublicObject(ctx context.Context, url string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) == 0 {
		return nil, false
	}
	return body, true
}

// writeFileAtomic writes through a temporary file, so a render stopped
// halfway never leaves a cut-off clip behind to be reused.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".clip-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
