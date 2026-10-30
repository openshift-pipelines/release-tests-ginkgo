// Package testcase provides TC-ID validation functionality for test suites.
package testcase

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TCSpecMap holds the mapping of TC-IDs to test files
type TCSpecMap map[string]string

// LoadTCSpecMap loads the tc_spec_map.json file from the repo root
// Returns both the spec map and the repo root directory
func LoadTCSpecMap() (TCSpecMap, string, error) {
	// Find repo root by looking for tc_spec_map.json
	wd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get working directory: %w", err)
	}

	// Search up the directory tree for tc_spec_map.json
	for {
		mapPath := filepath.Join(wd, "tc_spec_map.json")
		if _, err := os.Stat(mapPath); err == nil {
			// Found it, load the file
			data, err := os.ReadFile(mapPath)
			if err != nil {
				return nil, "", fmt.Errorf("failed to read tc_spec_map.json: %w", err)
			}

			var m TCSpecMap
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, "", fmt.Errorf("failed to parse tc_spec_map.json: %w", err)
			}
			return m, wd, nil
		}

		// Move up one directory
		parent := filepath.Dir(wd)
		if parent == wd {
			// Reached root without finding file
			return nil, "", fmt.Errorf("tc_spec_map.json not found in repo root")
		}
		wd = parent
	}
}

// ValidateTestFileAgainstSpecMap validates that:
// 1. The current test file is registered in tc_spec_map.json
// 2. All TC-IDs found in the test file are in the spec map
// 3. The TC-IDs in the file match the TC-IDs in the spec map for that file
// This is designed to run once from hooks at suite initialization.
func ValidateTestFileAgainstSpecMap(testFilePath string) error {
	specs, repoRoot, err := LoadTCSpecMap()
	if err != nil {
		return fmt.Errorf("failed to load tc_spec_map.json: %w", err)
	}

	// Normalize the test file path for comparison using the repo root
	testFileNormalized := normalizeFilePathWithRoot(testFilePath, repoRoot)

	// Find which TC-IDs are registered for this file
	registeredTCIDs := make([]string, 0)
	for tcID, filePath := range specs {
		if normalizeFilePathWithRoot(filepath.Join(repoRoot, filePath), repoRoot) == testFileNormalized {
			registeredTCIDs = append(registeredTCIDs, tcID)
		}
	}

	if len(registeredTCIDs) == 0 {
		// File is not registered at all
		return fmt.Errorf(
			"test file '%s' is not registered in tc_spec_map.json\n\n"+
				"SOLUTION: Please add this file to tc_spec_map.json with its TC-ID(s):\n"+
				"  {\n"+
				"    \"PIPELINES-NN\": \"%s\"\n"+
				"  }",
			testFilePath, testFileNormalized,
		)
	}

	// Read the file and extract TC-IDs
	data, err := os.ReadFile(testFilePath)
	if err != nil {
		return fmt.Errorf("failed to read test file %s: %w", testFilePath, err)
	}

	fileContent := string(data)
	foundTCIDs := extractAllTCIDsFromContent(fileContent)

	if len(foundTCIDs) == 0 {
		return fmt.Errorf(
			"no TC-IDs found in test file '%s'\n\n"+
				"SOLUTION: Add TC-ID(s) to your Describe blocks:\n"+
				"  var _ = Describe(\"Description: PIPELINES-NN\", func() { ... })",
			testFilePath,
		)
	}

	// Check if all found TC-IDs are registered and belong to this file
	for tcID := range foundTCIDs {
		mappedFile, exists := specs[tcID]
		if !exists {
			return fmt.Errorf(
				"TC-ID '%s' found in '%s' but not registered in tc_spec_map.json\n\n"+
					"SOLUTION: Please add this TC-ID to tc_spec_map.json:\n"+
					"  {\n"+
					"    \"%s\": \"%s\"\n"+
					"  }",
				tcID, testFilePath, tcID, testFileNormalized,
			)
		}

		// Verify the TC-ID is mapped to THIS file, not another file
		mappedFileNormalized := normalizeFilePathWithRoot(filepath.Join(repoRoot, mappedFile), repoRoot)
		if mappedFileNormalized != testFileNormalized {
			return fmt.Errorf(
				"TC-ID '%s' found in '%s' but is registered to '%s' in tc_spec_map.json\n\n"+
					"SOLUTION: Either:\n"+
					"  1. Use a different TC-ID in this file\n"+
					"  2. Update tc_spec_map.json to register '%s' to '%s':\n"+
					"     {\n"+
					"       \"%s\": \"%s\"\n"+
					"     }",
				tcID, testFileNormalized, mappedFileNormalized,
				tcID, testFileNormalized, tcID, testFileNormalized,
			)
		}
	}

	// Check if all registered TC-IDs are actually used in the file
	for _, tcID := range registeredTCIDs {
		if _, used := foundTCIDs[tcID]; !used {
			return fmt.Errorf(
				"TC-ID '%s' is registered for '%s' in tc_spec_map.json but not found in the file\n\n"+
					"SOLUTION: Either:\n"+
					"  1. Add the TC-ID to a Describe block: Describe(\"Description: %s\", ...)\n"+
					"  2. Remove the TC-ID from tc_spec_map.json if it's no longer used",
				tcID, testFileNormalized, tcID,
			)
		}
	}

	return nil
}

// extractAllTCIDsFromContent extracts all TC-IDs from Ginkgo Describe blocks only
// This ensures TC-IDs in comments or other code don't count as "used"
func extractAllTCIDsFromContent(content string) map[string]bool {
	found := make(map[string]bool)

	// Extract all Describe block descriptions
	// Matches: Describe("description with TC-ID", func() { ... })
	//          var _ = Describe("description", ...)
	describeRe := regexp.MustCompile(`Describe\s*\(\s*"([^"]*)"`)
	describeMatches := describeRe.FindAllStringSubmatch(content, -1)

	// Now extract PIPELINES-* IDs from each Describe description
	tcIDRe := regexp.MustCompile(`\b(PIPELINES-\d+)\b`)
	for _, match := range describeMatches {
		if len(match) > 1 {
			// match[1] is the description text from Describe("...")
			tcIDMatches := tcIDRe.FindAllStringSubmatch(match[1], -1)
			for _, tcIDMatch := range tcIDMatches {
				if len(tcIDMatch) > 1 {
					found[tcIDMatch[1]] = true
				}
			}
		}
	}
	return found
}

// normalizeFilePathWithRoot normalizes file paths relative to the repo root
// This correctly handles cases where "/tests/" appears in parent directories
func normalizeFilePathWithRoot(absPath, repoRoot string) string {
	absPath = strings.TrimSpace(absPath)
	absPath = strings.ReplaceAll(absPath, "\\", "/")

	repoRoot = strings.TrimSpace(repoRoot)
	repoRoot = strings.ReplaceAll(repoRoot, "\\", "/")

	// Use filepath.Rel to compute relative path from repo root
	if relPath, err := filepath.Rel(repoRoot, absPath); err == nil {
		// Convert to forward slashes for consistency
		return strings.ReplaceAll(relPath, "\\", "/")
	}

	// Fallback to string heuristic if filepath.Rel fails
	return normalizeFilePath(absPath)
}

// normalizeFilePath normalizes file paths for comparison (fallback method)
// Converts absolute paths to relative paths for matching against tc_spec_map.json
// Uses string heuristic: extracts from last occurrence of "/tests/"
func normalizeFilePath(path string) string {
	// Convert to forward slashes and remove leading/trailing spaces
	path = strings.TrimSpace(path)
	path = strings.ReplaceAll(path, "\\", "/")

	// If path is absolute, try to convert to relative path
	// Use LastIndex to find the LAST /tests/ (handles nested tests/ dirs)
	if idx := strings.LastIndex(path, "/tests/"); idx != -1 {
		// Extract from /tests/ onwards (e.g., tests/chains/chains_test.go)
		path = path[idx+1:] // Skip the leading /
	}

	return path
}
