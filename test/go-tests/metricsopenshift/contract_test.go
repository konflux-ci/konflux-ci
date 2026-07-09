package metricsopenshift

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsopenshift"
)

func init() {
	loadMetricsCatalog()
}

var _ = Describe("Metrics scrape contract", Label("openshift", "metrics-contract"), func() {
	Expect(metricsCatalog).NotTo(BeNil())
	Expect(uwmTargets).NotTo(BeEmpty())

	for _, target := range uwmTargets {
		It("wires HTTPS scrape resources for "+target.ID,
			Label(target.LabelGroup()),
			func(ctx SpecContext) {
				GinkgoHelper()
				Eventually(func(g Gomega) {
					ready, err := metricsauth.WaitForServiceEndpointsReady(ctx, kubeClient, target.Namespace, target.Service, target.Port)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(ready).To(BeTrue(), "metrics service endpoints should be ready")
				}).WithTimeout(contractReadyTimeout).WithPolling(contractReadyInterval).Should(Succeed())

				Eventually(func(g Gomega) {
					g.Expect(metricsopenshift.ValidateScrapeContract(ctx, kubeClient, target)).To(Succeed())
				}).WithTimeout(contractReadyTimeout).WithPolling(contractReadyInterval).Should(Succeed())
			},
		)
	}
})
