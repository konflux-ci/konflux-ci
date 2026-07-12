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
	require.Len(t, catalog.Targets, 5)

	byID := map[string]Target{}
	for _, target := range catalog.Targets {
		byID[target.ID] = target
	}

	assert.Equal(t, TargetGroupOperator, byID["konflux-operator"].LabelGroup())
	assert.Equal(t, TargetGroupComponent, byID["build-service"].LabelGroup())
	assert.Equal(t, TargetGroupComponent, byID["integration-service"].LabelGroup())
	assert.Equal(t, TargetGroupComponent, byID["image-controller"].LabelGroup())
	assert.Equal(t, TargetGroupComponent, byID["konflux-ui-proxy"].LabelGroup())
}

func TestNewCatalog_InvalidGroup(t *testing.T) {
	tlsSkip := true
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
			TLSInsecureSkipVerify:    &tlsSkip,
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
