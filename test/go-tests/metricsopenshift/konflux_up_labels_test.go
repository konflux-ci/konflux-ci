package metricsopenshift

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsopenshift"
)

func init() {
	loadMetricsCatalog()
}

var _ = Describe("konflux_up ecosystem labels", Label("openshift", "metrics-uwm"), func() {
	It("exposes konflux_up with service=konflux-operator after metricRelabelings",
		Label("operator"),
		func(ctx SpecContext) {
			GinkgoHelper()
			promql := metricsopenshift.KonfluxUpPromQL()

			dumpOnFailure := true
			defer func() {
				if !dumpOnFailure {
					return
				}

				fmt.Fprintln(GinkgoWriter, "\n=== DEBUG: broad konflux_up (no label filter) ===")
				broadResult, err := metricsopenshift.QueryPrometheus(ctx, kubeREST, metricsopenshift.BroadKonfluxUpPromQL())
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "error querying broad konflux_up: %v\n", err)
				} else {
					for _, sample := range broadResult.Data.Result {
						fmt.Fprintf(GinkgoWriter, "  labels: %v\n", sample.Metric)
					}
					if len(broadResult.Data.Result) == 0 {
						fmt.Fprintln(GinkgoWriter, "  (no series)")
					}
				}

				fmt.Fprintln(GinkgoWriter, "\n=== DEBUG: all time series from konflux-operator namespace ===")
				allResult, err := metricsopenshift.QueryPrometheus(ctx, kubeREST, metricsopenshift.AllOperatorSeriesPromQL())
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "error querying operator series: %v\n", err)
				} else {
					for _, sample := range allResult.Data.Result {
						fmt.Fprintf(GinkgoWriter, "  %s %v\n", sample.Metric["__name__"], sample.Metric)
					}
					if len(allResult.Data.Result) == 0 {
						fmt.Fprintln(GinkgoWriter, "  (no series)")
					}
				}
			}()

			Eventually(func(g Gomega) {
				result, err := metricsopenshift.QueryPrometheus(ctx, kubeREST, promql)
				g.Expect(err).NotTo(HaveOccurred())
				if len(result.Data.Result) > 0 {
					dumpOnFailure = false
				}
				g.Expect(result.Data.Result).NotTo(BeEmpty(),
					"expected konflux_up{service=\"konflux-operator\"} to return at least one series; "+
						"metricRelabelings may not be restoring the service label after honorLabels override")
			}).WithTimeout(uwmTargetTimeout).WithPolling(uwmTargetInterval).Should(Succeed())
		},
	)
})
