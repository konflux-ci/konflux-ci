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

var _ = Describe("Proxy Unit testing", func() {
	Describe("Test dummy", func() {
		Context("When a test dummy is running", func() {
			It("Test should return true", func() {
				got := true
				Expect(got).To(BeTrue())
			})
		})
	})
})
