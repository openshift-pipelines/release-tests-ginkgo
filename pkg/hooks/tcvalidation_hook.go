package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/testcase"
)

// init() automatically validates TC-IDs for any test that imports this package
// This runs during package initialization before any tests execute
// If validation fails, it panics to halt test execution immediately
func init() {
	// Only validate if we're running tests (check if test environment is active)
	if os.Getenv("GINKGO_SUITE_NAME") != "" || isRunningUnderTest() {
		validateTestSuiteOnInit()
	}
}

func isRunningUnderTest() bool {
	for _, arg := range os.Args {
		if strings.Contains(arg, ".test") || strings.Contains(arg, "-test") {
			return true
		}
	}
	return false
}

func validateTestSuiteOnInit() {
	suiteDir := findTestSuiteDirectory()
	if suiteDir == "" {
		return
	}

	entries, err := os.ReadDir(suiteDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "suite_test.go" {
			continue
		}

		if strings.HasSuffix(entry.Name(), "_test.go") {
			fullPath := filepath.Join(suiteDir, entry.Name())
			if err := testcase.ValidateTestFileAgainstSpecMap(fullPath); err != nil {
				panic(fmt.Sprintf("TC-ID Validation Failed in %s:\n%v", suiteDir, err))
			}
		}
	}
}

func findTestSuiteDirectory() string {
	// Walk call stack to find test directory with suite_test.go
	for i := 0; i < 20; i++ {
		_, file, _, ok := runtime.Caller(i)
		if !ok {
			break
		}

		if strings.Contains(file, "_test.go") {
			dir := filepath.Dir(file)
			if _, err := os.Stat(filepath.Join(dir, "suite_test.go")); err == nil {
				return dir
			}
		}
	}

	// Fallback: check working directory and parents for suite_test.go
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(cwd, "suite_test.go")); err == nil {
			return cwd
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			break
		}
		cwd = parent
	}

	return ""
}
