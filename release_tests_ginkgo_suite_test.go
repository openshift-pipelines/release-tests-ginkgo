package release_tests_ginkgo_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestReleaseTestsGinkgo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ReleaseTestsGinkgo Suite")
}