package kindmulticluster

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSystemBashScenarioCLI(t *testing.T) {
	// Given the system interpreter when available, otherwise the PATH Bash.
	shell := "/bin/bash"
	if _, err := os.Stat(shell); errors.Is(err, os.ErrNotExist) {
		shell = "bash"
	} else if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../scripts/kind-multicluster.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		exit int
	}{
		{[]string{"--help"}, 0},
		{[]string{"unexpected"}, 2},
		{[]string{"--spokes=0"}, 2},
		{[]string{"--spokes=3"}, 2},
		{[]string{"--spokes=2", "extra"}, 2},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			// When parsing help or invalid arguments, before any provisioning.
			output, err := exec.CommandContext(ctx, shell, append([]string{script}, tc.args...)...).CombinedOutput()

			// Then the documented CLI exit code is preserved.
			code := 0
			if err != nil {
				var exit *exec.ExitError
				if !errors.As(err, &exit) {
					t.Fatal(err)
				}
				code = exit.ExitCode()
			}
			if code != tc.exit {
				t.Fatalf("exit=%d want=%d output=%s", code, tc.exit, output)
			}
		})
	}
}
