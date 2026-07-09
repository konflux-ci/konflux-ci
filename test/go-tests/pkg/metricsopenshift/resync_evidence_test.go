package metricsopenshift

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	konfluxkubernetes "github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestOperandScrapeResyncExpected(t *testing.T) {
	assert.True(t, OperandScrapeResyncExpected(metricsauth.Target{
		ID:                "build-service",
		Group:             metricsauth.TargetGroupComponent,
		ScrapeTokenSecret: konfluxkubernetes.ScrapeTokenSecretName,
	}))
	assert.False(t, OperandScrapeResyncExpected(metricsauth.Target{
		ID:    "integration-service",
		Group: metricsauth.TargetGroupComponent,
	}))
	assert.False(t, OperandScrapeResyncExpected(metricsauth.Target{
		ID:                "konflux-operator",
		Group:             metricsauth.TargetGroupOperator,
		ScrapeTokenSecret: konfluxkubernetes.ScrapeTokenSecretName,
	}))
}

func TestValidateOperandScrapeResync(t *testing.T) {
	target := metricsauth.Target{
		ID:        "build-service",
		Namespace: "build-service",
		Group:     metricsauth.TargetGroupComponent,
		ScrapeTokenSecret: konfluxkubernetes.ScrapeTokenSecretName,
	}

	sm := &unstructured.Unstructured{}
	sm.SetName("build-service")
	sm.SetNamespace("build-service")
	require.Error(t, ValidateOperandScrapeResync(sm, target))

	sm.SetAnnotations(map[string]string{
		konfluxkubernetes.ServiceMonitorResyncAnnotation: "2026-07-12T10:00:00Z",
	})
	require.NoError(t, ValidateOperandScrapeResync(sm, target))
}

func TestServiceMonitorResyncAt(t *testing.T) {
	sm := &unstructured.Unstructured{}
	assert.Empty(t, ServiceMonitorResyncAt(sm))

	sm.SetAnnotations(map[string]string{
		konfluxkubernetes.ServiceMonitorResyncAnnotation: "2026-07-12T10:00:00Z",
	})
	assert.Equal(t, "2026-07-12T10:00:00Z", ServiceMonitorResyncAt(sm))
}

func TestServiceMonitorResyncReason(t *testing.T) {
	sm := &unstructured.Unstructured{}
	assert.Empty(t, ServiceMonitorResyncReason(sm))

	sm.SetAnnotations(map[string]string{
		konfluxkubernetes.ServiceMonitorResyncReasonAnnotation: konfluxkubernetes.ServiceMonitorResyncReasonSettleRetry,
	})
	assert.Equal(t, konfluxkubernetes.ServiceMonitorResyncReasonSettleRetry, ServiceMonitorResyncReason(sm))
}

func TestFormatScrapeResyncEvidenceLine(t *testing.T) {
	target := metricsauth.Target{
		ID:                "build-service",
		Namespace:         "build-service",
		Group:             metricsauth.TargetGroupComponent,
		Service:           "build-service-controller-manager-metrics-service",
		ScrapeTokenSecret: konfluxkubernetes.ScrapeTokenSecretName,
	}

	line := formatScrapeResyncEvidenceLine(context.Background(), nil, fake.NewClientBuilder().Build(), nil, target)
	assert.Contains(t, line, "id=build-service")
	assert.Contains(t, line, "sm=build-service")
	assert.Contains(t, line, "resync_at=error:")
	assert.Contains(t, line, "scrape_token=error:")
	assert.Contains(t, line, "uwm_active_targets=unknown")
}
