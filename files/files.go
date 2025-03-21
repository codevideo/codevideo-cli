package files

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/codevideo/codevideo-cli/types"
)

// moveFile moves a file from src to dst.
func MoveFile(src, dst string) error {
	// Ensure the destination directory exists.
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func UnmarshalManifest(manifestPath string) (*types.CodeVideoManifest, error) {
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var metadata types.CodeVideoManifest
	if err := json.Unmarshal(manifestBytes, &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}
