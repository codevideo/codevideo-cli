package types

type CodeVideoManifest struct {
	Environment string      `json:"environment"`
	UserID      string      `json:"userId"`
	UUID        string      `json:"uuid"`
	Actions     []Action    `json:"actions,omitempty"`
	Lesson      Lesson      `json:"lesson,omitempty"`
	AudioItems  []AudioItem `json:"audioItems"`
	FontSizePx  int         `json:"fontSizePx,omitempty"`
	Error       string      `json:"error,omitempty"`
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
