// Package cmd provides helpers for running CLI commands in integration tests.
package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"gotest.tools/v3/icmd"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"

	. "github.com/onsi/gomega" //nolint:revive,staticcheck // dot import is idiomatic for Gomega
)

// Run executes a command with the default CLI timeout.
func Run(args ...string) *icmd.Result {
	return icmd.RunCmd(icmd.Cmd{Command: Command(args...), Timeout: config.CLITimeout})
}

// Command adds configured Kubernetes connection flags to oc and kubectl commands.
func Command(args ...string) []string {
	if len(args) == 0 || (args[0] != "oc" && args[0] != "kubectl") {
		return args
	}

	command := []string{args[0]}
	for _, connectionFlag := range []struct {
		name  string
		value string
	}{
		{"--kubeconfig", config.Flags.Kubeconfig},
		{"--context", config.Flags.Context},
		{"--cluster", config.Flags.Cluster},
	} {
		if connectionFlag.value != "" && !hasFlag(args[1:], connectionFlag.name) {
			command = append(command, connectionFlag.name, connectionFlag.value)
		}
	}
	return append(command, args[1:]...)
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

// MustSucceed asserts that the command ran with exit code 0.
func MustSucceed(args ...string) *icmd.Result {
	return Assert(icmd.Success, args...)
}

// Assert runs a command and verifies its exit code matches the expected one.
func Assert(exp icmd.Expected, args ...string) *icmd.Result {
	res := Run(args...)
	Expect(res.ExitCode).To(Equal(exp.ExitCode),
		fmt.Sprintf("expected exit code %d but got %d\nstdout:\n%s\nstderr:\n%s",
			exp.ExitCode, res.ExitCode, res.Stdout(), res.Stderr()))
	return res
}

// MustSucceedIncreasedTimeout asserts success using a custom timeout.
func MustSucceedIncreasedTimeout(timeout time.Duration, args ...string) *icmd.Result {
	return AssertIncreasedTimeout(icmd.Success, timeout, args...)
}

// AssertIncreasedTimeout runs a command with a custom timeout and checks its exit code.
func AssertIncreasedTimeout(exp icmd.Expected, timeout time.Duration, args ...string) *icmd.Result {
	res := RunIncreasedTimeout(timeout, args...)
	Expect(res.ExitCode).To(Equal(exp.ExitCode),
		fmt.Sprintf("expected exit code %d but got %d\nstdout:\n%s\nstderr:\n%s",
			exp.ExitCode, res.ExitCode, res.Stdout(), res.Stderr()))
	return res
}

// RunIncreasedTimeout executes a command with the specified timeout.
func RunIncreasedTimeout(timeout time.Duration, args ...string) *icmd.Result {
	return icmd.RunCmd(icmd.Cmd{Command: Command(args...), Timeout: timeout})
}

// RunWithEnv executes a command with additional environment variables appended to the current env.
func RunWithEnv(env []string, args ...string) *icmd.Result {
	return icmd.RunCmd(icmd.Cmd{Command: args, Timeout: config.CLITimeout, Env: env})
}

// MustSucceedWithEnv asserts exit code 0 for a command run with extra env vars.
func MustSucceedWithEnv(env []string, args ...string) *icmd.Result {
	res := RunWithEnv(env, args...)
	Expect(res.ExitCode).To(Equal(0),
		fmt.Sprintf("expected exit code 0 but got %d\nstdout:\n%s\nstderr:\n%s",
			res.ExitCode, res.Stdout(), res.Stderr()))
	return res
}

// MustSucceedWithStdin runs a command with stdin piped from the given reader and asserts exit code 0.
func MustSucceedWithStdin(stdin io.Reader, args ...string) *icmd.Result {
	res := icmd.RunCmd(icmd.Cmd{Command: args, Timeout: config.CLITimeout, Stdin: stdin})
	Expect(res.ExitCode).To(Equal(0),
		fmt.Sprintf("expected exit code 0 but got %d\nstdout:\n%s\nstderr:\n%s",
			res.ExitCode, res.Stdout(), res.Stderr()))
	return res
}
