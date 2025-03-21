package utils

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ExtractProgress extracts the progress value from a Puppeteer log string.
// Returns the progress as a float64 and an error if extraction fails.
func ExtractProgress(logString string) (float64, error) {

	if strings.Contains(logString, "Final progress") {
		return 100, nil
	}

	// Create a regex to match a number inside single quotes after "progress: "
	re := regexp.MustCompile(`progress: '(\d+\.\d+)'`)

	// Find the matches
	matches := re.FindStringSubmatch(logString)

	if len(matches) > 1 {
		// Convert the captured group (the number) to float64
		return strconv.ParseFloat(matches[1], 64)
	}

	// Return 0 and an error if no match was found
	return 0, fmt.Errorf("no progress value found in log string")
}
