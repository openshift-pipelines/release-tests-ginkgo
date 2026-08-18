package pipelines

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunOCRejectsCommandFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture requires Unix")
	}

	dir := t.TempDir()
	oc := filepath.Join(dir, "oc")
	if err := os.WriteFile(oc, []byte("#!/bin/sh\necho denied >&2\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := runOC("get", "pipelinerun")
	if err == nil {
		t.Fatal("expected a failed oc command to return an error")
	}
	if !strings.Contains(err.Error(), "exit code 7") || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("unexpected error: %v", err)
	}
}
