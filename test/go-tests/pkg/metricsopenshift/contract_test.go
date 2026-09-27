package metricsopenshift

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
)

func TestValidateServiceMonitorSelectorMatch(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	target := metricsauth.Target{
		ID:        "namespace-lister",
		Namespace: "namespace-lister",
		Service:   "namespace-lister",
	}
	sm := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"selector": map[string]any{
				"matchLabels": map[string]any{
					"apps": "namespace-lister",
				},
			},
		},
	}}
	sm.SetNamespace(target.Namespace)
	sm.SetName("namespace-lister")

	t.Run("matches", func(t *testing.T) {
		t.Parallel()
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      target.Service,
				Namespace: target.Namespace,
				Labels:    map[string]string{"apps": "namespace-lister"},
			},
		}).Build()
		require.NoError(t, ValidateServiceMonitorSelectorMatch(context.Background(), c, target, sm))
	})

	t.Run("mismatches when Service metadata labels omit selector key", func(t *testing.T) {
		t.Parallel()
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      target.Service,
				Namespace: target.Namespace,
				// pod selector only — the UWM discovery bug
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"apps": "namespace-lister"},
			},
		}).Build()
		err := ValidateServiceMonitorSelectorMatch(context.Background(), c, target, sm)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `service missing label "apps"`)
	})
}
