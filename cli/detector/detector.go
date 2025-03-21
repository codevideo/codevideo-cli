package detector

import (
	"encoding/json"
	"fmt"

	"github.com/codevideo/codevideo-cli/types"
)

// DetectProjectType determines the type of project from a JSON string
func DetectProjectType(jsonData string) (*types.Course, *types.Lesson, *[]types.Action, error) {
	// First try to unmarshal as Course
	var course types.Course
	err := json.Unmarshal([]byte(jsonData), &course)
	if err == nil && len(course.Lessons) > 0 {
		return &course, nil, nil, nil
	}

	// Next try to unmarshal as Lesson
	var lesson types.Lesson
	err = json.Unmarshal([]byte(jsonData), &lesson)
	if err == nil && len(lesson.Actions) > 0 {
		return nil, &lesson, nil, nil
	}

	// Finally try to unmarshal as Actions
	var actions []types.Action
	err = json.Unmarshal([]byte(jsonData), &actions)
	if err == nil && len(actions) > 0 {
		// Validate each action
		for _, action := range actions {
			if !types.IsValidAction(action) {
				return nil, nil, nil, fmt.Errorf("invalid action detected: %v", action)
			}
		}
		actions = types.ActionsProject(actions)
		return nil, nil, &actions, nil
	}

	return nil, nil, nil, fmt.Errorf("unable to determine project type: %v", err)
}

// IsCourse checks if a project is a Course
func IsCourse(project types.Project) bool {
	_, ok := project.(types.Course)
	return ok
}

// IsLesson checks if a project is a Lesson
func IsLesson(project types.Project) bool {
	_, ok := project.(types.Lesson)
	return ok
}

// IsActions checks if a project is an Actions list
func IsActions(project types.Project) bool {
	_, ok := project.(types.ActionsProject)
	return ok
}
