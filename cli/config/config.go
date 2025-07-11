package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/codevideo/codevideo-cli/types"
	"github.com/spf13/cobra"
)

// Config holds all configuration settings for the CLI
type Config struct {
	// Project settings
	ProjectJSON string

	// Output settings
	OutputDir      string
	OutputFileName string
	OutputFormat   string

	// Processing settings
	Resolution  string
	Orientation string
	Debug       bool // Debug mode flag

	// Environment
	OperatingSystem string
	Environment     string

	// Config file path
	ConfigFilePath string
}

// Global configuration pointer
var GlobalConfig *Config

// Initialize with default configuration
func init() {
	GlobalConfig = DefaultConfig()
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		OutputDir:       ".",
		OutputFileName:  fmt.Sprintf("CodeVideo-%s", time.Now().Format("2006-01-02-15-04-05")),
		OutputFormat:    "mp4",
		Resolution:      "1080p",     // 1080p by default, could be 4K
		Orientation:     "landscape", // Default to landscape
		Debug:           false,       // Debug mode disabled by default
		OperatingSystem: runtime.GOOS,
		Environment:     "local",
	}
}

// LoadFromFlags updates the configuration from command flags
func LoadFromFlags(cmd *cobra.Command) error {
	// Read output path if provided
	output, _ := cmd.Flags().GetString("output")
	if output != "" {
		GlobalConfig.OutputDir = filepath.Dir(output)
		GlobalConfig.OutputFileName = filepath.Base(output)
		// Remove extension if present
		GlobalConfig.OutputFileName = GlobalConfig.OutputFileName[:len(GlobalConfig.OutputFileName)-len(filepath.Ext(GlobalConfig.OutputFileName))]
	}

	// Read resolution if provided
	resolution, _ := cmd.Flags().GetString("resolution")
	if resolution != "" {
		GlobalConfig.Resolution = resolution
	}

	// Read orientation if provided
	orientation, _ := cmd.Flags().GetString("orientation")
	if orientation != "" {
		GlobalConfig.Orientation = orientation
	}

	// Read config file path if provided
	configPath, _ := cmd.Flags().GetString("config")
	if configPath != "" {
		GlobalConfig.ConfigFilePath = configPath
	}

	// Read debug flag if provided
	debug, _ := cmd.Flags().GetBool("debug")
	GlobalConfig.Debug = debug

	// Ensure output directories exist
	return GlobalConfig.EnsureOutputDirs()
}

// EnsureOutputDirs ensures that all required output directories exist
func (c *Config) EnsureOutputDirs() error {
	// Make sure output directory exists
	if err := os.MkdirAll(c.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create temp directories for processing
	tempDirs := []string{
		filepath.Join(os.TempDir(), "codevideo", "new"),
		filepath.Join(os.TempDir(), "codevideo", "error"),
		filepath.Join(os.TempDir(), "codevideo", "success"),
		filepath.Join(os.TempDir(), "codevideo", "video"),
	}

	for _, dir := range tempDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create temp directory %s: %w", dir, err)
		}
	}

	return nil
}

// GetTempDir returns the path to a specific temporary directory
func (c *Config) GetTempDir(dirType string) string {
	return filepath.Join(os.TempDir(), "codevideo", dirType)
}

// GenerateOutputPath generates the full path for an output file
func (c *Config) GenerateOutputPath(suffix string) string {
	filename := c.OutputFileName
	if suffix != "" {
		filename = fmt.Sprintf("%s-%s", filename, suffix)
	}
	return filepath.Join(c.OutputDir, fmt.Sprintf("%s.%s", filename, c.OutputFormat))
}

// LoadConfigFile loads and parses the configuration file
func LoadConfigFile(configPath string) (*types.CodeVideoIDEProps, error) {
	if configPath == "" {
		return nil, nil
	}

	// Check if file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %s", configPath)
	}

	// Read file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse JSON
	var config types.CodeVideoIDEProps
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	// Validate required fields
	if config.Theme != "light" && config.Theme != "dark" {
		return nil, fmt.Errorf("theme must be 'light' or 'dark', got: %s", config.Theme)
	}

	if config.DefaultLanguage == "" {
		return nil, fmt.Errorf("defaultLanguage is required")
	}

	return &config, nil
}
