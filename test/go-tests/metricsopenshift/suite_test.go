// Package metricsopenshift is the Ginkgo e2e suite for OpenShift UWM metrics.
//
// BeforeSuite enables UWM readiness polling, then logs [UWM resync] evidence before specs.
// Contract specs validate scrape wiring; uwm_targets specs poll up==1 in UWM Prometheus.
package metricsopenshift

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestMetricsOpenShift(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metrics OpenShift Suite")
}
