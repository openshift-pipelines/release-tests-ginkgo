package pac_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPAC(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PAC Suite")
}
