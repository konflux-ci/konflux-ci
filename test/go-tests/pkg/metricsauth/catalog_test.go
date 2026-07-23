package metricsauth

import (
	"testing"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultCatalog_TargetGroups(t *testing.T) {
	catalog, err := DefaultCatalog()
	require.NoError(t, err)
	require.Len(t, catalog.Targets, 6)

	byID := map[string]Target{}
	for _, target := range catalog.Targets {
		byID[target.ID] = target
	}

	assert.Equal(t, TargetGroupOperator, byID["konflux-operator"].LabelGroup())
	assert.Equal(t, TargetGroupComponent, byID["build-service"].LabelGroup())
	assert.Equal(t, TargetGroupComponent, byID["integration-service"].LabelGroup())
	assert.Equal(t, TargetGroupComponent, byID["release-service"].LabelGroup())
	assert.Equal(t, TargetGroupComponent, byID["image-controller"].LabelGroup())
	assert.Equal(t, TargetGroupComponent, byID["konflux-ui-proxy"].LabelGroup())
	assert.False(t, byID["integration-service"].UWMUpCheck)
	assert.Equal(t, kubernetes.ScrapeTokenSecretName, byID["integration-service"].ScrapeTokenSecret)
	assert.Equal(t, "https", byID["integration-service"].Scheme)
	assert.False(t, byID["release-service"].UWMUpCheck)
	assert.Equal(t, kubernetes.ScrapeTokenSecretName, byID["release-service"].ScrapeTokenSecret)
	assert.Equal(t, "https", byID["release-service"].Scheme)
	assert.True(t, byID["konflux-ui-proxy"].UWMUpCheck)
	assert.Empty(t, byID["konflux-ui-proxy"].ScrapeTokenSecret)

	assert.False(t, byID["konflux-operator"].TLSInsecureSkipVerifyForScrape())
	assert.Equal(t, MetricsCASecretName, byID["konflux-operator"].MetricsCASecret)
	assert.Equal(t,
		"konflux-operator-controller-manager-metrics-service.konflux-operator.svc",
		byID["konflux-operator"].TLSServerNameForScrape())
	assert.False(t, byID["build-service"].TLSInsecureSkipVerifyForScrape())
	assert.Equal(t, MetricsCASecretName, byID["build-service"].MetricsCASecret)
	assert.Equal(t,
		"build-service-controller-manager-metrics-service.build-service.svc",
		byID["build-service"].TLSServerNameForScrape())
	assert.False(t, byID["image-controller"].TLSInsecureSkipVerifyForScrape())
	assert.Equal(t, MetricsCASecretName, byID["image-controller"].MetricsCASecret)
	assert.Equal(t,
		"image-controller-controller-manager-metrics-service.image-controller.svc",
		byID["image-controller"].TLSServerNameForScrape())
	assert.False(t, byID["release-service"].TLSInsecureSkipVerifyForScrape())
	assert.Equal(t, MetricsCASecretName, byID["release-service"].MetricsCASecret)
	assert.Equal(t,
		"release-service-controller-manager-metrics-service.release-service.svc",
		byID["release-service"].TLSServerNameForScrape())
	assert.False(t, byID["integration-service"].TLSInsecureSkipVerifyForScrape())
	assert.Equal(t, MetricsCASecretName, byID["integration-service"].MetricsCASecret)
	assert.Equal(t,
		"integration-service-controller-manager-metrics-service.integration-service.svc",
		byID["integration-service"].TLSServerNameForScrape())
}

func TestNewCatalog_InvalidGroup(t *testing.T) {
	insecureSkipVerify := true
	_, err := NewCatalog([]Target{
		{
			ID:                       "bad",
			Group:                    "unknown",
			Namespace:                "ns",
			Service:                  "metrics",
			Scheme:                   "https",
			PortName:                 "https",
			Port:                     8443,
			Path:                     "/metrics",
			TLSInsecureSkipVerify:    &insecureSkipVerify,
			MetricsReaderClusterRole: "role",
			ScrapeTokenSecret:        kubernetes.ScrapeTokenSecretName,
			BodyMustMatchAny:         []string{"workqueue_"},
		},
	})
	assert.Error(t, err)
}

func TestNewCatalog_HTTPSRequiresScrapeTokenSecret(t *testing.T) {
	_, err := NewCatalog([]Target{
		{
			ID:                       "bad",
			Namespace:                "ns",
			Service:                  "metrics",
			Scheme:                   "https",
			PortName:                 "https",
			Port:                     8443,
			Path:                     "/metrics",
			MetricsReaderClusterRole: "role",
			BodyMustMatchAny:         []string{"workqueue_"},
		},
	})
	assert.Error(t, err)
}

func TestNewCatalog_HTTPSVerifiedRequiresCASecret(t *testing.T) {
	insecureSkipVerify := false
	_, err := NewCatalog([]Target{
		{
			ID:                       "bad",
			Namespace:                "ns",
			Service:                  "metrics",
			Scheme:                   "https",
			PortName:                 "https",
			Port:                     8443,
			Path:                     "/metrics",
			TLSInsecureSkipVerify:    &insecureSkipVerify,
			MetricsReaderClusterRole: "role",
			ScrapeTokenSecret:        kubernetes.ScrapeTokenSecretName,
			BodyMustMatchAny:         []string{"workqueue_"},
		},
	})
	assert.Error(t, err)
}
