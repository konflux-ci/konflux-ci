package metricsintegration

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMetricsIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metrics Integration Suite")
}
