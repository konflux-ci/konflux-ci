package go_tests

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"

	"github.com/konflux-ci/konflux-ci/operator/pkg/ingress"
)

const (
	additionalFQDNIngressName = "konflux-ui-additional"
	additionalFQDNHostPrefix  = "konflux-ui-additional"
	uiDexConfigMapLabel       = "app.kubernetes.io/managed-by-konflux-ui-reconciler"
	uiDexConfigMapKey         = "config.yaml"
)

// additionalFQDNFromPrimaryHost derives a distinct alias hostname under the same
// DNS suffix as the primary UI host (typically the OpenShift apps domain).
func additionalFQDNFromPrimaryHost(primaryHost string) (string, error) {
	host := strings.TrimSpace(primaryHost)
	if host == "" {
		return "", fmt.Errorf("primary host is empty")
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("primary host %q has no DNS suffix for an additional FQDN", primaryHost)
	}
	return additionalFQDNHostPrefix + "." + strings.Join(parts[1:], "."), nil
}

func additionalFQDNURL(aliasHost string) string {
	return (&url.URL{Scheme: "https", Host: aliasHost}).String()
}

func buildAdditionalFQDNIngress(namespace, aliasHost string) *networkingv1.Ingress {
	return ingress.Build(ingress.Config{
		Name:        additionalFQDNIngressName,
		Namespace:   namespace,
		Hostname:    aliasHost,
		ServiceName: ingress.ProxyServiceName,
		ServicePort: ingress.ProxyServicePort,
		Path:        ingress.DefaultIngressPath,
	})
}

func uiNamespaceForProxyTests() string {
	if ns := strings.TrimSpace(os.Getenv("KONFLUX_UI_NAMESPACE")); ns != "" {
		return ns
	}
	return "konflux-ui"
}

// proxyAdditionalFQDNTestsEnabled reports whether CR-mutating additional-FQDN
// specs should run. Enabled for konflux-ci OpenShift CI (Prow sets
// REPO_NAME=konflux-ci) or when KONFLUX_PROXY_TEST_ADDITIONAL_FQDN=true.
// Disabled when the Konflux CR is managed externally (live patches would not hold).
func proxyAdditionalFQDNTestsEnabled() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("KONFLUX_PROXY_TEST_ADDITIONAL_FQDN")), "true") {
		return true
	}
	return strings.TrimSpace(os.Getenv("REPO_NAME")) == "konflux-ci"
}
