package metricsauth

import (
	"sync"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

var (
	defaultCatalogOnce sync.Once
	defaultCatalog     *Catalog
	defaultCatalogErr  error
)

var prometheusBodyMatchers = []string{
	"workqueue_",
	"controller_runtime_",
	"go_gc_",
}

// DefaultCatalog returns the built-in scrape targets for cluster integration tests.
func DefaultCatalog() (*Catalog, error) {
	defaultCatalogOnce.Do(func() {
		tlsSkip := true
		catalog := &Catalog{
			Targets: []Target{
				{
					ID:                       "konflux-operator",
					Group:                    TargetGroupOperator,
					Namespace:                "konflux-operator",
					Service:                  "konflux-operator-controller-manager-metrics-service",
					Scheme:                   "https",
					PortName:                 "https",
					Port:                     8443,
					Path:                     "/metrics",
					TLSInsecureSkipVerify:    &tlsSkip,
					MetricsReaderClusterRole: "konflux-operator-metrics-reader",
					ScrapeTokenSecret:        kubernetes.ScrapeTokenSecretName,
					BodyMustMatchAny:         prometheusBodyMatchers,
				},
				{
					ID:                       "build-service",
					Group:                    TargetGroupComponent,
					Namespace:                "build-service",
					Service:                  "build-service-controller-manager-metrics-service",
					Scheme:                   "https",
					PortName:                 "https",
					Port:                     8443,
					Path:                     "/metrics",
					TLSInsecureSkipVerify:    &tlsSkip,
					MetricsReaderClusterRole: "build-service-metrics-reader",
					ScrapeTokenSecret:        kubernetes.ScrapeTokenSecretName,
					BodyMustMatchAny:         prometheusBodyMatchers,
				},
				{
					ID:                       "integration-service",
					Group:                    TargetGroupComponent,
					Namespace:                "integration-service",
					Service:                  "integration-service-controller-manager-metrics-service",
					Scheme:                   "http",
					PortName:                 "http",
					Port:                     8080,
					Path:                     "/metrics",
					MetricsReaderClusterRole: "integration-service-metrics-reader",
					UWMUpCheck:               true,
					BodyMustMatchAny:         prometheusBodyMatchers,
				},
				{
					ID:                       "image-controller",
					Group:                    TargetGroupComponent,
					Namespace:                "image-controller",
					Service:                  "image-controller-controller-manager-metrics-service",
					Scheme:                   "https",
					PortName:                 "https",
					Port:                     8443,
					Path:                     "/metrics",
					TLSInsecureSkipVerify:    &tlsSkip,
					MetricsReaderClusterRole: "image-controller-metrics-reader",
					ScrapeTokenSecret:        kubernetes.ScrapeTokenSecretName,
					BodyMustMatchAny:         prometheusBodyMatchers,
				},
				{
					ID:                       "konflux-ui-proxy",
					Group:                    TargetGroupComponent,
					Namespace:                "konflux-ui",
					Service:                  "proxy",
					Scheme:                   "http",
					PortName:                 "metrics",
					Port:                     2112,
					Path:                     "/metrics",
					MetricsReaderClusterRole: "konflux-ui-proxy-metrics-reader",
					UWMUpCheck:               true,
					BodyMustMatchAny:         []string{"caddy_"},
				},
			},
		}
		defaultCatalogErr = catalog.validate()
		defaultCatalog = catalog
	})
	if defaultCatalogErr != nil {
		return nil, defaultCatalogErr
	}
	targets := make([]Target, len(defaultCatalog.Targets))
	copy(targets, defaultCatalog.Targets)
	return &Catalog{Targets: targets}, nil
}

// NewCatalog validates and returns a catalog from explicit targets (for tests).
func NewCatalog(targets []Target) (*Catalog, error) {
	catalog := &Catalog{Targets: targets}
	if err := catalog.validate(); err != nil {
		return nil, err
	}
	return catalog, nil
}
