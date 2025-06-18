package types

type CodeVideoManifest struct {
	Environment       string             `json:"environment"`
	UserID            string             `json:"userId"`
	UUID              string             `json:"uuid"`
	Actions           []Action           `json:"actions,omitempty"`
	Lesson            Lesson             `json:"lesson,omitempty"`
	AudioItems        []AudioItem        `json:"audioItems"`
	FontSizePx        int                `json:"fontSizePx,omitempty"`
	Error             string             `json:"error,omitempty"`
	CodeVideoIDEProps *CodeVideoIDEProps `json:"codeVideoIDEProps,omitempty"`
}

// Configuration holds all CLI configuration
type Configuration struct {
	ProjectJSON     string
	OutputPath      string
	Resolution      string
	MaxConcurrent   int
	OperatingSystem string
}

type Action struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// IsValidAction checks if an action has valid Name and Value fields
func IsValidAction(action Action) bool {
	return action.Name != "" && action.Value != ""
}

// AudioItem represents an audio item in a CodeVideo project
type AudioItem struct {
	Text   string `json:"text"`
	Mp3Url string `json:"mp3Url"`
}

// Project is a generic interface for all project types
type Project interface {
	// GetType returns the type of the project (Actions, Lesson, or Course)
	GetType() string
}

// ActionsProject represents a list of actions
type ActionsProject []Action

// GetType implements the Project interface
func (a ActionsProject) GetType() string {
	return "Actions"
}

// CourseSnapshot represents a distinct moment in time during a lesson
// including the entire IDE state, mouse state, and other UI elements
// Since this structure is only forwarded to Node scripts and not processed in Go,
// we use a generic map to avoid having to define every field
type CourseSnapshot map[string]interface{}

// Lesson represents a lesson project
type Lesson struct {
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	InitialSnapshot CourseSnapshot `json:"initialSnapshot"`
	Actions         []Action       `json:"actions"`
}

// GetType implements the Project interface
func (l Lesson) GetType() string {
	return "Lesson"
}

// Course represents a course project
type Course struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Lessons     []Lesson `json:"lessons"`
}

// GetType implements the Project interface
func (c Course) GetType() string {
	return "Course"
}

type CodeVideoUserMetadata struct {
	Tokens             int    `json:"tokens"`
	StripeID           string `json:"stripeId"`
	Unlimited          bool   `json:"unlimited"`
	SubscriptionPlan   string `json:"subscriptionPlan"`
	SubscriptionStatus string `json:"subscriptionStatus"`
	TokensPerCycle     int    `json:"tokensPerCycle"`
}

type CodeVideoIDEProps struct {
	Theme                    string             `json:"theme"` // 'light' | 'dark'
	Project                  Project            `json:"project"`
	Mode                     string             `json:"mode"` // GUIMode equivalent
	AllowFocusInEditor       bool               `json:"allowFocusInEditor"`
	CurrentActionIndex       int                `json:"currentActionIndex"`
	CurrentLessonIndex       *int               `json:"currentLessonIndex"`
	DefaultLanguage          string             `json:"defaultLanguage"`
	IsExternalBrowserStepUrl *string            `json:"isExternalBrowserStepUrl"`
	IsSoundOn                bool               `json:"isSoundOn"`
	WithCaptions             bool               `json:"withCaptions"`
	SpeakActionAudios        []SpeakActionAudio `json:"speakActionAudios"`
	FileExplorerWidth        *int               `json:"fileExplorerWidth,omitempty"`
	TerminalHeight           *int               `json:"terminalHeight,omitempty"`
	MouseColor               *string            `json:"mouseColor,omitempty"`
	FontSizePx               *int               `json:"fontSizePx,omitempty"`
	IsEmbedMode              *bool              `json:"isEmbedMode,omitempty"`
	IsFileExplorerVisible    *bool              `json:"isFileExplorerVisible,omitempty"`
	IsTerminalVisible        *bool              `json:"isTerminalVisible,omitempty"`
	KeyboardTypingPauseMs    *int               `json:"keyboardTypingPauseMs,omitempty"`
	StandardPauseMs          *int               `json:"standardPauseMs,omitempty"`
	LongPauseMs              *int               `json:"longPauseMs,omitempty"`
}

type SpeakActionAudio struct {
	Text   string `json:"text"`
	Mp3Url string `json:"mp3Url"`
}
