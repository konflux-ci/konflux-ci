package metricsopenshift

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsopenshift"
)

const (
	contractReadyTimeout  = 5 * time.Minute
	contractReadyInterval = 5 * time.Second
	uwmTargetTimeout      = 10 * time.Minute
	uwmTargetInterval     = 15 * time.Second
)

var (
	metricsCatalog  *metricsauth.Catalog
	uwmTargets      []metricsauth.Target
	uwmUpOnlyTargets []metricsauth.Target
)

var _ = BeforeSuite(func(ctx SpecContext) {
	Expect(initKubernetesClient()).To(Succeed())
	Expect(kubeClient).NotTo(BeNil())
	Expect(kubeREST).NotTo(BeNil())

	loadMetricsCatalog()

	waitCfg := metricsopenshift.WaitConfigFromEnv()
	Expect(metricsopenshift.WaitReady(ctx, kubeREST, waitCfg)).To(Succeed())

	metricsopenshift.LogScrapeResyncEvidence(ctx, kubeREST, kubeClient, uwmTargets)
})

func loadMetricsCatalog() {
	if metricsCatalog != nil {
		return
	}

	catalog, err := metricsauth.DefaultCatalog()
	if err != nil {
		panic(fmt.Sprintf("metrics catalog: %v", err))
	}
	metricsCatalog = catalog
	uwmTargets = metricsopenshift.UWMCatalogTargets(catalog)
	uwmUpOnlyTargets = metricsopenshift.UWMUpOnlyTargets(catalog)
}
