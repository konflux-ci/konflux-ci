package metricsintegration

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
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
	if target.ScrapeTokenSecret != "" {
		Eventually(func(g Gomega) {
			var tokenErr error
			token, tokenErr = metricsauth.SecretToken(ctx, kubeClient, target.Namespace, target.ScrapeTokenSecret, kubernetes.ScrapeTokenSecretKey)
			g.Expect(tokenErr).NotTo(HaveOccurred())
			g.Expect(token).NotTo(BeEmpty())
		}).WithTimeout(metricsReadyTimeout).WithPolling(metricsReadyInterval).Should(Succeed())
	}

	var caCert []byte
	if target.MetricsCASecret != "" {
		Eventually(func(g Gomega) {
			var caErr error
			caCert, caErr = metricsauth.SecretBytes(ctx, kubeClient, target.Namespace, target.MetricsCASecret, metricsauth.MetricsCACertKey)
			g.Expect(caErr).NotTo(HaveOccurred())
			g.Expect(caCert).NotTo(BeEmpty())
		}).WithTimeout(metricsReadyTimeout).WithPolling(metricsReadyInterval).Should(Succeed())
	}

	pf, err := metricsauth.StartPortForward(ctx, kubeREST, metricsauth.ServiceRef{
		Namespace: target.Namespace,
		Name:      target.Service,
		Port:      target.Port,
	})
	Expect(err).NotTo(HaveOccurred())
	defer pf.Close()

	scrapeURL := metricsauth.LocalMetricsURL(pf.LocalPort(), target.Path, target.Scheme)
	result, err := metricsauth.ScrapeLocal(ctx, scrapeURL, token, target.Scheme, target.ScrapeTLSConfigFor(caCert))
	Expect(err).NotTo(HaveOccurred())
	Expect(metricsauth.ValidatePrometheusText(result, target.BodyMustMatchAny)).To(Succeed())
}
