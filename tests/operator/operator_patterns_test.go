package operator_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/oc"
)

// operator_patterns_test.go -- Reference implementations of Ginkgo v2 patterns.
//
// These sample specs demonstrate three key patterns that will be used heavily
// across the migrated test suites:
//   - DeferCleanup: co-located resource creation and teardown (SUITE-05)
//   - Eventually/Consistently: async polling and stability assertions (SUITE-06)
//   - Ordered containers: sequential multi-step workflows (SUITE-07)
//
// All specs are wrapped in PDescribe (pending) so they do not execute without
// a live cluster. They serve as copy-paste references for future migration phases.

// ---------------------------------------------------------------------------
// Pattern A: DeferCleanup (SUITE-05)
// ---------------------------------------------------------------------------
//
// DeferCleanup registers a cleanup callback at the point of resource creation.
// This keeps the "create" and "destroy" logic together, making tests easier to
// read and maintain. Cleanups are executed in LIFO order and run even if the
// spec fails, so resources are never leaked.
//
// WRONG pattern -- do NOT capture SpecContext in a closure:
//
//	ctx := context.Background()
//	DeferCleanup(func() { deleteWithCtx(ctx) }) // stale context!
//
// RIGHT pattern -- use the callback-with-context form:
//
//	DeferCleanup(func(ctx context.Context) { deleteWithCtx(ctx) })
//
// For helpers that do not need a context (e.g., oc.DeleteProjectIgnoreErrors),
// a simple func() callback is fine.

var _ = PDescribe("DeferCleanup Pattern", Label("pattern-reference"), func() {

	It("should create a namespace and register cleanup inline", func() {
		ns := "pattern-demo-cleanup"

		// Create the resource...
		oc.CreateNewProject(ns)

		// ...and immediately register its teardown.
		// DeferCleanup runs in LIFO order and executes even on failure.
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(ns)
		})

		// The rest of the test can use `ns` knowing cleanup is guaranteed.
		Expect(oc.CheckProjectExists(ns)).To(BeTrue(), "project %s should exist after creation", ns)
	})
})

// ---------------------------------------------------------------------------
// Pattern B: Eventually / Consistently (SUITE-06)
// ---------------------------------------------------------------------------
//
// Eventually polls a function until the assertion succeeds or the timeout
// expires. Use it for any condition that may take time to become true (e.g.,
// waiting for a pod to be ready, a CRD status to update).
//
// Consistently asserts that a condition remains true for the entire duration.
// Use it to verify that something does NOT change (e.g., a resource stays
// healthy after a configuration change).
//
// Always use config constants for timeouts and polling intervals so values are
// consistent across the entire test suite:
//   - config.APITimeout      (10 min) -- upper bound for async operations
//   - config.APIRetry         (5 sec) -- polling interval between checks
//   - config.ConsistentlyDuration (30 sec) -- stability assertion window

var _ = PDescribe("Eventually/Consistently Pattern", Label("pattern-reference"), func() {

	It("should poll until a condition is met using Eventually", func() {
		// Eventually accepts a function and retries until the inner assertions
		// pass or the timeout expires. The `func(g Gomega)` signature lets you
		// use `g.Expect(...)` inside the polling function for rich matchers.
		Eventually(func(g Gomega) {
			exists := oc.CheckProjectExists(config.TargetNamespace)
			g.Expect(exists).To(BeTrue(), "namespace %s should exist", config.TargetNamespace)
		}).WithTimeout(config.APITimeout).WithPolling(config.APIRetry).Should(Succeed())
	})

	It("should verify a condition does not change using Consistently", func() {
		// Consistently verifies the function's return value does not change
		// over the specified duration. If the assertion ever fails during that
		// window, the spec fails immediately.
		Consistently(func() bool {
			return oc.CheckProjectExists(config.TargetNamespace)
		}).WithTimeout(config.ConsistentlyDuration).WithPolling(config.APIRetry).Should(BeTrue(),
			"namespace %s should remain present for the full duration", config.TargetNamespace)
	})
})

// ---------------------------------------------------------------------------
// Pattern C: Ordered Container (SUITE-07)
// ---------------------------------------------------------------------------
//
// An Ordered container guarantees that its It blocks run sequentially in the
// order they are defined. This is essential for multi-step workflows where
// each step depends on the outcome of the previous one (e.g., create a
// resource, modify it, verify the modification, then tear it down).
//
// Key rules:
//   - Declare shared state as `var` in the Describe closure -- NOT initialized.
//   - Use BeforeAll (not BeforeEach) to set up shared state once.
//   - Register DeferCleanup inside BeforeAll for teardown.
//   - If any It block fails, all subsequent It blocks in the container are
//     SKIPPED (not run). This prevents cascading failures.

var _ = PDescribe("Ordered Container Pattern", Ordered, Label("pattern-reference"), func() {

	// Shared state declared at closure scope -- set in BeforeAll, read in It blocks.
	var (
		projectName string
		projectExists bool
	)

	BeforeAll(func() {
		projectName = "pattern-demo-ordered"

		// Create shared resource once for the entire Ordered container.
		oc.CreateNewProject(projectName)

		// Register cleanup inside BeforeAll so it runs after all It blocks,
		// even if one of them fails.
		DeferCleanup(func() {
			oc.DeleteProjectIgnoreErrors(projectName)
		})
	})

	// Step 1: Verify the project was created.
	It("should have created the project", func() {
		projectExists = oc.CheckProjectExists(projectName)
		Expect(projectExists).To(BeTrue(), "project %s should exist", projectName)
	})

	// Step 2: Use the shared state from step 1.
	// If step 1 failed, this It is automatically SKIPPED.
	It("should confirm project state is consistent", func() {
		// projectExists was set in the previous It -- safe because Ordered
		// guarantees sequential execution.
		Expect(projectExists).To(BeTrue(), "projectExists should have been set to true in step 1")

		// Double-check with a fresh query to demonstrate Consistently.
		Consistently(func() bool {
			return oc.CheckProjectExists(projectName)
		}).WithTimeout(config.ConsistentlyDuration).WithPolling(config.APIRetry).Should(BeTrue(),
			"project %s should remain present", projectName)
	})
})

// Ensure sharedClients from suite_test.go is accessible in this file.
// This variable is initialized in BeforeSuite and can be used by any spec
// in the operator_test package.
var _ = Describe("sharedClients availability", Label("pattern-reference"), func() {
	It("is accessible from suite_test.go", func() {
		// sharedClients is a package-level variable declared in suite_test.go.
		// In a real test, you would use it to interact with the cluster API.
		// Here we just confirm it is accessible at compile time.
		_ = sharedClients
	})
})
