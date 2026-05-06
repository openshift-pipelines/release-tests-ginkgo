package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LoadEnvironmentFromProperties loads environment variables from properties file
func LoadEnvironmentFromProperties() error {
	// Get the project root directory
	_, b, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(b), "..", "..")
	propertiesFile := filepath.Join(projectRoot, "env", "default", "default.properties")

	// Check if properties file exists
	if _, err := os.Stat(propertiesFile); os.IsNotExist(err) {
		// Properties file doesn't exist, continue without error
		// This allows tests to run without the file if env vars are set externally
		return nil
	}

	file, err := os.Open(propertiesFile)
	if err != nil {
		return fmt.Errorf("failed to open properties file %s: %w", propertiesFile, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse key = value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Only set if not already set (allows override from CI/CD)
		if os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading properties file: %w", err)
	}

	return nil
}

// MustLoadEnvironment loads environment from properties file or panics
func MustLoadEnvironment() {
	if err := LoadEnvironmentFromProperties(); err != nil {
		panic(fmt.Sprintf("Failed to load environment: %v", err))
	}
}

// GetPropertiesFilePath returns the path to the properties file
func GetPropertiesFilePath() string {
	_, b, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(b), "..", "..")
	return filepath.Join(projectRoot, "env", "default", "default.properties")
}
