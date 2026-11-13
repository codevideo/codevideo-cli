package generator

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// AudioTake is a narration clip made before the render, such as a speech take
// chosen in the studio. It's matched to the narration by its exact text, and
// the CLI uses it instead of generating that line again.
type AudioTake struct {
	Text string `json:"text"`
	// Path is an audio file on disk. A relative path is read from the folder
	// of the takes file.
	Path string `json:"path,omitempty"`
	// Mp3Url is used as it is: a data URI, or a URL the render page can load.
	Mp3Url string `json:"mp3Url,omitempty"`
}

// LoadAudioTakes reads a JSON array of takes, checks that every file exists,
// and returns them keyed by their text. Each text may have only one take.
func LoadAudioTakes(path string) (map[string]AudioTake, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the audio takes: %w", err)
	}
	var list []AudioTake
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("the audio takes in %s aren't a JSON array of {text, path}: %w", path, err)
	}
	base := filepath.Dir(path)
	takes := make(map[string]AudioTake, len(list))
	for i, take := range list {
		if strings.TrimSpace(take.Text) == "" {
			return nil, fmt.Errorf("audio take %d has no text", i+1)
		}
		if (take.Path == "") == (take.Mp3Url == "") {
			return nil, fmt.Errorf("audio take %d needs either a path or an mp3Url", i+1)
		}
		if take.Path == "" && strings.HasPrefix(take.Mp3Url, "file:") {
			take.Path, take.Mp3Url = filePathFromURL(take.Mp3Url), ""
		}
		if take.Path != "" {
			if !filepath.IsAbs(take.Path) {
				take.Path = filepath.Join(base, take.Path)
			}
			info, err := os.Stat(take.Path)
			if err != nil {
				return nil, fmt.Errorf("audio take %d: %w", i+1, err)
			}
			if !info.Mode().IsRegular() || info.Size() == 0 {
				return nil, fmt.Errorf("audio take %d is not an audio file: %s", i+1, take.Path)
			}
		}
		if existing, ok := takes[take.Text]; ok && existing != take {
			return nil, fmt.Errorf("audio takes %q: only one take per exact text", take.Text)
		}
		takes[take.Text] = take
	}
	return takes, nil
}

// resolve returns the take's URL for the render page and, when the CLI can
// read it, the audio itself (to measure the clip).
func (t AudioTake) resolve() (string, []byte, error) {
	if t.Path != "" {
		audio, err := os.ReadFile(t.Path)
		if err != nil {
			return "", nil, fmt.Errorf("reading the audio take for %q: %w", t.Text, err)
		}
		return "data:" + audioMimeType(t.Path) + ";base64," + base64.StdEncoding.EncodeToString(audio), audio, nil
	}
	if payload, ok := strings.CutPrefix(t.Mp3Url, "data:"); ok {
		if _, encoded, found := strings.Cut(payload, ";base64,"); found {
			audio, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				return "", nil, fmt.Errorf("the audio take for %q is not valid base64: %w", t.Text, err)
			}
			return t.Mp3Url, audio, nil
		}
		return "", nil, fmt.Errorf("the audio take for %q must be a base64 data URI", t.Text)
	}
	return t.Mp3Url, nil, nil
}

func audioMimeType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav":
		return "audio/wav"
	case ".m4a", ".mp4":
		return "audio/mp4"
	case ".ogg", ".opus":
		return "audio/ogg"
	default:
		return "audio/mpeg"
	}
}

// filePathFromURL turns a file: URL into a path, or returns the input when it
// can't be parsed.
func filePathFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Path == "" {
		return raw
	}
	path := parsed.Path
	// file:///C:/clips/a.mp3 has the path /C:/clips/a.mp3 on Windows.
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}
