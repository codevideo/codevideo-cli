package renderer

import (
	"fmt"
	"strings"
)

// ProgressBar renders a progress bar in the CLI based on a percentage value
// It returns a string in the format: [===== ] XX%
// where the number of "=" characters represents the progress percentage
// totalWidth defines the total width of the progress bar including brackets
func ProgressBar(percentage float64, totalWidth int) string {
	// Validate input
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}
	if totalWidth < 7 { // Minimum width needed for "[] 100%"
		totalWidth = 32 // Default to 32 characters wide
	}

	// Calculate the available width for the progress indicators
	// Subtract 2 for the brackets and 4 for the percentage display (e.g., " 45%")
	barWidth := totalWidth - 6

	// Calculate how many "=" characters to display
	completedWidth := int((percentage / 100) * float64(barWidth))

	// Build the progress bar
	bar := "["
	bar += strings.Repeat("=", completedWidth)
	bar += strings.Repeat(" ", barWidth-completedWidth)
	bar += "]"

	// Format the percentage (right-aligned)
	percentStr := fmt.Sprintf(" %3.0f%%", percentage)

	return bar + percentStr
}

// MultiProgressBar renders multiple progress bars for a Course project
// Takes a slice of percentages and labels and renders them all
func MultiProgressBar(percentages []float64, labels []string, totalWidth int) []string {
	if len(percentages) != len(labels) {
		return []string{"Error: Number of percentages and labels must match"}
	}

	result := make([]string, len(percentages))

	for i, percentage := range percentages {
		status := "Awaiting resources..."
		if percentage > 0 {
			status = ""
		}

		bar := ProgressBar(percentage, totalWidth)

		// If this is "Awaiting resources", replace the progress indicators
		if status != "" {
			bar = strings.Replace(bar, "]", status+"]", 1)
		}

		result[i] = fmt.Sprintf("%s %s", labels[i], bar)
	}

	return result
}

// RenderProgressToConsole prints the progress bar to the console
// This function can be called repeatedly to update the progress display
func RenderProgressToConsole(percentage float64, message string) {
	bar := ProgressBar(percentage, 32)
	// "\r" returns the cursor to the beginning and "\033[2K" clears the entire line
	fmt.Printf("\r\033[2K%s %s", bar, message)
}

// RenderMultiProgressToConsole prints multiple progress bars to the console
func RenderMultiProgressToConsole(percentages []float64, labels []string) {
	// Clear the terminal lines first
	for i := 0; i < len(percentages); i++ {
		fmt.Print("\033[1A\033[K") // Move up one line and clear it
	}

	bars := MultiProgressBar(percentages, labels, 32)
	for _, bar := range bars {
		fmt.Println(bar)
	}
}
