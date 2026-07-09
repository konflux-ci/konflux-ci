package metricsopenshift

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsopenshift"
)

func init() {
	loadMetricsCatalog()
}

var _ = Describe("UWM Prometheus targets", Label("openshift", "metrics-uwm"), func() {
	Expect(metricsCatalog).NotTo(BeNil())
	Expect(uwmTargets).NotTo(BeEmpty())

	for _, target := range uwmTargets {
		It("scrapes "+target.ID+" via user-workload Prometheus",
			Label(target.LabelGroup()),
			func(ctx SpecContext) {
				GinkgoHelper()
				promql := metricsopenshift.UpPromQL(target)
				dumpOnFailure := true
				defer func() {
					if dumpOnFailure {
						metricsopenshift.DumpTargetDebugOnFailure(ctx, GinkgoWriter, kubeREST, kubeClient, target, promql, uwmTargets)
					}
				}()

				pollState := &metricsopenshift.UWMPollLogState{}
				Eventually(func(g Gomega) {
					result, err := metricsopenshift.QueryPrometheus(ctx, kubeREST, promql)
					g.Expect(err).NotTo(HaveOccurred())
					if metricsopenshift.HasUpSample(result) {
						dumpOnFailure = false
						return
					}
					metricsopenshift.LogUWMPollProgress(GinkgoWriter, kubeREST, target, promql, result, pollState)
					g.Expect(metricsopenshift.HasUpSample(result)).To(BeTrue(),
						"expected UWM up sample for %s (%s); got %s",
						target.ID, promql, metricsopenshift.FormatUpResult(result))
				}).WithTimeout(uwmTargetTimeout).WithPolling(uwmTargetInterval).Should(Succeed())
			},
		)
	}
})
