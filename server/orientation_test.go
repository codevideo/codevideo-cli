package server

import (
	"testing"

	"github.com/codevideo/codevideo-cli/cli/config"
	"github.com/codevideo/codevideo-cli/types"
)

func TestJobOrientationPrefersTheJobsOwn(t *testing.T) {
	previous := config.GlobalConfig.Orientation
	config.GlobalConfig.Orientation = "landscape"
	t.Cleanup(func() { config.GlobalConfig.Orientation = previous })

	cases := []struct {
		name     string
		manifest *types.CodeVideoManifest
		want     string
	}{
		{"portrait job", &types.CodeVideoManifest{VideoOptions: &types.VideoOptions{Orientation: "portrait"}}, "portrait"},
		{"landscape job", &types.CodeVideoManifest{VideoOptions: &types.VideoOptions{Orientation: "landscape"}}, "landscape"},
		{"no video options", &types.CodeVideoManifest{}, "landscape"},
		{"resolution only", &types.CodeVideoManifest{VideoOptions: &types.VideoOptions{Resolution: "4K"}}, "landscape"},
		{"unknown orientation", &types.CodeVideoManifest{VideoOptions: &types.VideoOptions{Orientation: "sideways"}}, "landscape"},
		{"no manifest", nil, "landscape"},
	}
	for _, c := range cases {
		if got := jobOrientation(c.manifest); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
