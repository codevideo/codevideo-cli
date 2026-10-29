package types

type RenderTrace struct {
	Version    int         `json:"version"`
	RenderMode RenderMode  `json:"renderMode"`
	RenderSeed string      `json:"renderSeed"`
	FPS        int         `json:"fps"`
	FrameCount int         `json:"frameCount"`
	DurationMs float64     `json:"durationMs"`
	SpeechCues []SpeechCue `json:"speechCues"`
}

type SpeechCue struct {
	ActionIndex int     `json:"actionIndex"`
	Type        string  `json:"type"`
	AssetID     string  `json:"assetId"`
	StartMs     float64 `json:"startMs"`
	DurationMs  float64 `json:"durationMs"`
}
