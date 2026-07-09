package metricsopenshift

import (
	"fmt"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
)

const (
	// UWMNamespace is OpenShift user-workload monitoring.
	UWMNamespace = "openshift-user-workload-monitoring"
	// CanaryNamespace is the dummy-service UWM canary namespace used in some environments.
	CanaryNamespace = "dummy-service"
)

// PrometheusBindingName returns the ClusterRoleBinding wired to the UWM scraper for a target.
func PrometheusBindingName(target metricsauth.Target) string {
	if target.ID == "konflux-operator" {
		return "konflux-operator-prometheus-konflux-operator-metrics-reader"
	}
	return "prometheus-" + target.ID + "-metrics-reader"
}

// ServiceMonitorName returns the ServiceMonitor resource name for a target.
func ServiceMonitorName(target metricsauth.Target) string {
	if target.ID == "konflux-operator" {
		return "controller-manager-metrics-monitor"
	}
	return target.ID
}

// UpPromQL returns a PromQL expression that is true when the target is scraped by UWM.
func UpPromQL(target metricsauth.Target) string {
	return fmt.Sprintf(
		`up{namespace=%q, service=%q} == 1`,
		target.Namespace,
		target.Service,
	)
}

// BroadUpPromQL returns a relaxed up query for debugging label mismatches in UWM.
func BroadUpPromQL(target metricsauth.Target) string {
	return fmt.Sprintf(`up{namespace=%q}`, target.Namespace)
}

// UWMCatalogTargets returns scrape-token catalog targets for UWM contract and up==1 specs
// (operator + operands with operator-managed prometheus-scrape-token).
func UWMCatalogTargets(catalog *metricsauth.Catalog) []metricsauth.Target {
	if catalog == nil {
		return nil
	}
	var out []metricsauth.Target
	for _, t := range catalog.Targets {
		if t.ScrapeTokenSecret == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// UWMUpOnlyTargets returns catalog targets that need UWM up==1 coverage without the
// scrape-token contract (legacy interim operands with UWMUpCheck set).
func UWMUpOnlyTargets(catalog *metricsauth.Catalog) []metricsauth.Target {
	if catalog == nil {
		return nil
	}
	var out []metricsauth.Target
	for _, t := range catalog.Targets {
		if t.UWMUpCheck {
			out = append(out, t)
		}
	}
	return out
}
