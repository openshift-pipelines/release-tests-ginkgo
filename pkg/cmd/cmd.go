package cmd

import (
	"fmt"
	"time"

	"github.com/srivickynesh/release-tests-ginkgo/pkg/config"
	"gotest.tools/v3/icmd"

	. "github.com/onsi/gomega"
)

// Run executes a command with the default CLI timeout.
func Run(cmd ...string) *icmd.Result {
	return icmd.RunCmd(icmd.Cmd{Command: cmd, Timeout: config.CLITimeout})
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
func RunIncreasedTimeout(timeout time.Duration, cmd ...string) *icmd.Result {
	return icmd.RunCmd(icmd.Cmd{Command: cmd, Timeout: timeout})
}