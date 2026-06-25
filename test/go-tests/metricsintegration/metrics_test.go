package metricsintegration

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
)

func init() {
	loadMetricsCatalog()
}

var _ = Describe("Metrics scraping", func() {
	Expect(metricsCatalog).NotTo(BeNil())
	Expect(metricsCatalog.Targets).NotTo(BeEmpty())

	for _, target := range metricsCatalog.Targets {
		target := target
		It("scrapes "+target.ID,
			Label("metrics", target.LabelGroup()),
			func(ctx SpecContext) {
				scrapeTarget(ctx, target)
			},
		)
	}
})

func scrapeTarget(ctx SpecContext, target metricsauth.Target) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		ready, err := metricsauth.WaitForServiceEndpointsReady(ctx, kubeClient, target.Namespace, target.Service, target.Port)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(ready).To(BeTrue(), "metrics service endpoints should be ready")
	}).WithTimeout(metricsReadyTimeout).WithPolling(metricsReadyInterval).Should(Succeed())

	var token string
	var err error
	if target.ScrapeTokenSecret != "" {
		Eventually(func(g Gomega) {
			var tokenErr error
			token, tokenErr = metricsauth.SecretToken(ctx, kubeClient, target.Namespace, target.ScrapeTokenSecret, "token")
			g.Expect(tokenErr).NotTo(HaveOccurred())
			g.Expect(token).NotTo(BeEmpty())
		}).WithTimeout(metricsReadyTimeout).WithPolling(metricsReadyInterval).Should(Succeed())
	} else {
		token, err = metricsauth.ServiceAccountToken(ctx, kubeREST, metricsCatalog.Scraper.Namespace, metricsCatalog.Scraper.ServiceAccount)
		Expect(err).NotTo(HaveOccurred())
	}

	pf, err := metricsauth.StartPortForward(ctx, kubeREST, metricsauth.ServiceRef{
		Namespace: target.Namespace,
		Name:      target.Service,
		Port:      target.Port,
	})
	Expect(err).NotTo(HaveOccurred())
	defer pf.Close()

	scrapeURL := metricsauth.LocalMetricsURL(pf.LocalPort(), target.Path, target.Scheme)
	result, err := metricsauth.ScrapeLocal(ctx, scrapeURL, token, target.Scheme, target.TLSInsecureSkipVerifyForScrape())
	Expect(err).NotTo(HaveOccurred())
	Expect(metricsauth.ValidatePrometheusText(result, target.BodyMustMatchAny)).To(Succeed())
}
