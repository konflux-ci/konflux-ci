package metricsintegration

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
)

const (
	metricsReadyTimeout  = 5 * time.Minute
	metricsReadyInterval = 2 * time.Second
)

var metricsCatalog *metricsauth.Catalog

var _ = BeforeSuite(func() {
	Expect(initKubernetesClient()).To(Succeed())
	Expect(kubeClient).NotTo(BeNil())
	Expect(kubeREST).NotTo(BeNil())

	loadMetricsCatalog()
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
}
