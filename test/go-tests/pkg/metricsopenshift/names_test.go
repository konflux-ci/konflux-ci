package metricsopenshift

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
)

func TestPrometheusBindingName(t *testing.T) {
	operator := metricsauth.Target{ID: "konflux-operator"}
	assert.Equal(t, "konflux-operator-prometheus-konflux-operator-metrics-reader", PrometheusBindingName(operator))
	assert.Equal(t, "prometheus-build-service-metrics-reader", PrometheusBindingName(metricsauth.Target{ID: "build-service"}))
}

func TestServiceMonitorName(t *testing.T) {
	assert.Equal(t, "controller-manager-metrics-monitor", ServiceMonitorName(metricsauth.Target{ID: "konflux-operator"}))
	assert.Equal(t, "build-service", ServiceMonitorName(metricsauth.Target{ID: "build-service"}))
}

func TestUpPromQL(t *testing.T) {
	target := metricsauth.Target{
		Namespace: "build-service",
		Service:   "build-service-controller-manager-metrics-service",
	}
	assert.Equal(t,
		`up{namespace="build-service", service="build-service-controller-manager-metrics-service"} == 1`,
		UpPromQL(target),
	)
}

func TestUWMUpOnlyTargets(t *testing.T) {
	catalog, err := metricsauth.DefaultCatalog()
	assert.NoError(t, err)

	targets := UWMUpOnlyTargets(catalog)
	assert.Len(t, targets, 1)
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
		assert.Empty(t, target.ScrapeTokenSecret)
	}
	assert.ElementsMatch(t, []string{"konflux-ui-proxy"}, ids)
}
