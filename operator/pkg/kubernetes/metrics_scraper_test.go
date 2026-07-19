package kubernetes

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testMetricsScraperNamespace = "build-service"

func TestOperandMetricsScraperSA(t *testing.T) {
	got := OperandMetricsScraperSA(testMetricsScraperNamespace)
	if got.Namespace != testMetricsScraperNamespace || got.Name != MetricsScraperServiceAccountName {
		t.Fatalf("unexpected scraper SA: %#v", got)
	}
}

func TestEnsureMetricsScraperServiceAccount(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	ctx := context.Background()

	if err := EnsureMetricsScraperServiceAccount(ctx, c, testMetricsScraperNamespace); err != nil {
		t.Fatalf("create metrics scraper SA: %v", err)
	}
	sa := &corev1.ServiceAccount{}
	if err := c.Get(ctx, OperandMetricsScraperSA(testMetricsScraperNamespace), sa); err != nil {
		t.Fatalf("get metrics scraper SA: %v", err)
	}
	if sa.Name != MetricsScraperServiceAccountName {
		t.Fatalf("unexpected SA name %q", sa.Name)
	}

	if err := EnsureMetricsScraperServiceAccount(ctx, c, "image-controller"); err != nil {
		t.Fatalf("ensure metrics scraper SA in second namespace: %v", err)
	}
}
