package go_tests

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGoTests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "GoTests Suite")
}
