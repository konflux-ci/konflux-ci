/*
Copyright 2025 Konflux CI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	testclock "k8s.io/utils/clock/testing"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

// These tests use controller-runtime's fake client, which registers types on a shared
// runtime.Scheme. Running them with t.Parallel() races concurrent Scheme map access
// (KnownTypes iteration vs Update) and can panic under load or with -race.

type fakeTokenCreator struct {
	token     string
	expiresAt time.Time
	err       error
	scraper   types.NamespacedName
}

func (f *fakeTokenCreator) CreateScraperToken(
	_ context.Context,
	scraper types.NamespacedName,
	_ time.Duration,
) (string, time.Time, error) {
	if f.err != nil {
		return "", time.Time{}, f.err
	}
	f.scraper = scraper
	return f.token, f.expiresAt, nil
}

func metricsTLSObjects(t *testing.T) []client.Object {
	t.Helper()
	caPEM, leafPEM, err := kubernetes.NewSelfSignedMetricsTLSMaterial()
	if err != nil {
		t.Fatalf("tls material: %v", err)
	}
	return []client.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:            kubernetes.MetricsServerCertSecretName,
				Namespace:       testBuildServiceNamespace,
				ResourceVersion: "leaf-1",
			},
			Data: map[string][]byte{
				kubernetes.MetricsCACertKey:            caPEM,
				kubernetes.MetricsServerCertTLSCertKey: leafPEM,
			},
		},
	}
}

func clientWithMetricsTLS(t *testing.T, extra ...client.Object) client.Client {
	t.Helper()
	objs := append(metricsTLSObjects(t), extra...)
	return fake.NewClientBuilder().WithObjects(objs...).Build()
}

func TestReconcilePrometheusScrapeToken_Validation(t *testing.T) {
	ctx := context.Background()

	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{})
	if err == nil {
		t.Fatal("expected error when token creator is nil")
	}

	_, err = ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		TokenCreator: &fakeTokenCreator{},
	})
	if err == nil {
		t.Fatal("expected error when operand namespace is empty")
	}
}

func TestReconcilePrometheusScrapeToken_CreatesToken(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	creator := &fakeTokenCreator{
		token:     "operand-token",
		expiresAt: now.Add(time.Hour),
	}
	scraper := kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace)

	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:           fake.NewClientBuilder().Build(),
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     creator,
		Scraper:          scraper,
		OperandNamespace: testBuildServiceNamespace,
		Apply: func(_ context.Context, _ *corev1.Secret) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if creator.scraper != scraper {
		t.Fatalf("scraper SA: got %#v want %#v", creator.scraper, scraper)
	}
}

func TestReconcilePrometheusScrapeToken_PropagatesErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	mintErr := errors.New("mint failed")

	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:           fake.NewClientBuilder().Build(),
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     &fakeTokenCreator{err: mintErr},
		Scraper:          types.NamespacedName{Namespace: "monitoring", Name: "prometheus"},
		OperandNamespace: testBuildServiceNamespace,
		Apply:            func(context.Context, *corev1.Secret) error { return nil },
	})
	if err == nil {
		t.Fatal("expected mint error")
	}
}

func TestReconcilePrometheusScrapeToken_TracksFreshSecret(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	fresh := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetes.ScrapeTokenSecretName,
			Namespace: testBuildServiceNamespace,
			Annotations: map[string]string{
				"konflux.konflux-ci.dev/scrape-token-expires-at": now.Add(45 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{kubernetes.ScrapeTokenSecretKey: []byte("existing")},
	}

	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:           fake.NewClientBuilder().WithObjects(fresh).Build(),
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     &fakeTokenCreator{},
		Scraper:          kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace: testBuildServiceNamespace,
		Apply:            func(context.Context, *corev1.Secret) error { return nil },
	})
	if err != nil {
		t.Fatalf("reconcile fresh secret: %v", err)
	}
}

func TestReconcilePrometheusScrapeToken_MintsAndSettles(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(operandServiceMonitorGVK)
	sm.SetNamespace(testBuildServiceNamespace)
	sm.SetName(testBuildServiceNamespace)

	c := clientWithMetricsTLS(t, sm)
	creator := &fakeTokenCreator{
		token:     "operand-token",
		expiresAt: now.Add(time.Hour),
	}
	cfg := ScrapeTokenReconcilerConfig{
		Client:             c,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       creator,
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			key := types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}
			existing := &corev1.Secret{}
			if err := c.Get(applyCtx, key, existing); apierrors.IsNotFound(err) {
				return c.Create(applyCtx, secret)
			} else if err != nil {
				return err
			}
			secret.SetResourceVersion(existing.ResourceVersion)
			return c.Update(applyCtx, secret)
		},
	}

	result, err := ReconcilePrometheusScrapeToken(ctx, cfg)
	if err != nil {
		t.Fatalf("mint reconcile: %v", err)
	}
	// TEMP EXPERIMENT: no settle-retry; token TTL drives requeue (30m for 1h token).
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("requeue: got %v want 30m", result.RequeueAfter)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM: %v", err)
	}
	if _, ok := updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation]; ok {
		t.Fatalf("expected no resync annotations on experiment arm, got %#v", updated.GetAnnotations())
	}
}

func TestReconcilePrometheusScrapeToken_SecretSyncWhenRVChanges(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(operandServiceMonitorGVK)
	sm.SetNamespace(testBuildServiceNamespace)
	sm.SetName(testBuildServiceNamespace)
	sm.SetAnnotations(map[string]string{
		kubernetes.ServiceMonitorResyncAnnotation:         "2026-07-12T07:00:00Z",
		kubernetes.ServiceMonitorResyncSecretRVAnnotation: "100",
		kubernetes.ServiceMonitorResyncReasonAnnotation:   kubernetes.ServiceMonitorResyncReasonTokenMinted,
	})

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            kubernetes.ScrapeTokenSecretName,
			Namespace:       testBuildServiceNamespace,
			ResourceVersion: "200",
			Annotations: map[string]string{
				"konflux.konflux-ci.dev/scrape-token-expires-at": now.Add(45 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{kubernetes.ScrapeTokenSecretKey: []byte("token")},
	}

	c := clientWithMetricsTLS(t, sm, secret)
	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             c,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       &fakeTokenCreator{},
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply:              func(context.Context, *corev1.Secret) error { return nil },
	})
	if err != nil {
		t.Fatalf("secret sync reconcile: %v", err)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := c.Get(ctx, client.ObjectKey{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM: %v", err)
	}
	// TEMP EXPERIMENT: secret-sync nudge disabled; seeded annotations remain.
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation] != kubernetes.ServiceMonitorResyncReasonTokenMinted {
		t.Fatalf("expected pre-existing reason unchanged, got %q", updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation])
	}
}

func TestReconcilePrometheusScrapeToken_SecretSyncBlockedDuringSettle(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(operandServiceMonitorGVK)
	sm.SetNamespace(testBuildServiceNamespace)
	sm.SetName(testBuildServiceNamespace)
	sm.SetAnnotations(map[string]string{
		kubernetes.ServiceMonitorResyncAnnotation:         "2026-07-12T07:00:00Z",
		kubernetes.ServiceMonitorResyncSecretRVAnnotation: "100",
		kubernetes.ServiceMonitorResyncReasonAnnotation:   kubernetes.ServiceMonitorResyncReasonTokenMinted,
		kubernetes.ServiceMonitorResyncSettleAnnotation:   "pending",
	})

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            kubernetes.ScrapeTokenSecretName,
			Namespace:       testBuildServiceNamespace,
			ResourceVersion: "200",
			Annotations: map[string]string{
				"konflux.konflux-ci.dev/scrape-token-expires-at": now.Add(45 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{kubernetes.ScrapeTokenSecretKey: []byte("token")},
	}

	c := clientWithMetricsTLS(t, sm, secret)
	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             c,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       &fakeTokenCreator{},
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply:              func(context.Context, *corev1.Secret) error { return nil },
	})
	if err != nil {
		t.Fatalf("reconcile during settle: %v", err)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := c.Get(ctx, client.ObjectKey{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM: %v", err)
	}
	// TEMP EXPERIMENT: settle-retry nudge disabled; seeded annotations remain.
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation] != kubernetes.ServiceMonitorResyncReasonTokenMinted {
		t.Fatalf("expected pre-existing reason unchanged, got %q", updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation])
	}
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncSettleAnnotation] != "pending" {
		t.Fatalf("expected settle pending to remain, got %#v", updated.GetAnnotations())
	}
}

func TestReconcilePrometheusScrapeToken_AppliesServiceMonitorWhenAbsent(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	c := clientWithMetricsTLS(t)
	creator := &fakeTokenCreator{
		token:     "operand-token",
		expiresAt: now.Add(time.Hour),
	}
	applied := false
	cfg := ScrapeTokenReconcilerConfig{
		Client:             c,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       creator,
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			key := types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}
			existing := &corev1.Secret{}
			if err := c.Get(applyCtx, key, existing); apierrors.IsNotFound(err) {
				return c.Create(applyCtx, secret)
			} else if err != nil {
				return err
			}
			secret.SetResourceVersion(existing.ResourceVersion)
			return c.Update(applyCtx, secret)
		},
		ApplyServiceMonitor: func(applyCtx context.Context) error {
			applied = true
			sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
			sm.SetGroupVersionKind(operandServiceMonitorGVK)
			sm.SetNamespace(testBuildServiceNamespace)
			sm.SetName(testBuildServiceNamespace)
			return c.Create(applyCtx, sm)
		},
	}

	result, err := ReconcilePrometheusScrapeToken(ctx, cfg)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !applied {
		t.Fatal("expected ApplyServiceMonitor to run when SM is absent")
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("requeue: got %v want 30m", result.RequeueAfter)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM: %v", err)
	}
	if _, ok := updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation]; ok {
		t.Fatalf("expected no resync annotations on experiment arm, got %#v", updated.GetAnnotations())
	}
}

func TestReconcilePrometheusScrapeToken_ReappliesServiceMonitorWhenPresent(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(operandServiceMonitorGVK)
	sm.SetNamespace(testBuildServiceNamespace)
	sm.SetName(testBuildServiceNamespace)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetes.ScrapeTokenSecretName,
			Namespace: testBuildServiceNamespace,
			Annotations: map[string]string{
				"konflux.konflux-ci.dev/scrape-token-expires-at": now.Add(45 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{kubernetes.ScrapeTokenSecretKey: []byte("token")},
	}

	c := clientWithMetricsTLS(t, sm, secret)
	applyCalls := 0
	result, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             c,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       &fakeTokenCreator{},
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply:              func(context.Context, *corev1.Secret) error { return nil },
		ApplyServiceMonitor: func(context.Context) error {
			applyCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if applyCalls != 1 {
		t.Fatalf("ApplyServiceMonitor calls: got %d want 1", applyCalls)
	}
	if result.RequeueAfter != 15*time.Minute {
		t.Fatalf("expected 15m requeue on fresh token with SM, got %v", result.RequeueAfter)
	}
}

func TestReconcilePrometheusScrapeToken_ValidationScraper(t *testing.T) {
	ctx := context.Background()

	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		TokenCreator:     &fakeTokenCreator{},
		OperandNamespace: testBuildServiceNamespace,
	})
	if err == nil {
		t.Fatal("expected error when scraper is empty")
	}
}

func TestReconcilePrometheusScrapeToken_NoServiceMonitorName(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)

	result, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:           fake.NewClientBuilder().Build(),
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     &fakeTokenCreator{token: "tok", expiresAt: now.Add(time.Hour)},
		Scraper:          kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace: testBuildServiceNamespace,
		Apply:            func(context.Context, *corev1.Secret) error { return nil },
	})
	if err != nil {
		t.Fatalf("reconcile without SM name: %v", err)
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("expected 30m requeue when ServiceMonitorName is empty, got %v", result.RequeueAfter)
	}
}

func TestReconcilePrometheusScrapeToken_TokenRefreshed(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)

	staleSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            kubernetes.ScrapeTokenSecretName,
			Namespace:       testBuildServiceNamespace,
			ResourceVersion: "50",
			Annotations: map[string]string{
				"konflux.konflux-ci.dev/scrape-token-expires-at": now.Add(10 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{kubernetes.ScrapeTokenSecretKey: []byte("old-token")},
	}
	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(operandServiceMonitorGVK)
	sm.SetNamespace(testBuildServiceNamespace)
	sm.SetName(testBuildServiceNamespace)

	c := clientWithMetricsTLS(t, staleSecret, sm)
	creator := &fakeTokenCreator{
		token:     "new-token",
		expiresAt: now.Add(time.Hour),
	}
	result, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             c,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       creator,
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			key := types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}
			existing := &corev1.Secret{}
			if err := c.Get(applyCtx, key, existing); err != nil {
				return err
			}
			secret.SetResourceVersion(existing.ResourceVersion)
			return c.Update(applyCtx, secret)
		},
	})
	if err != nil {
		t.Fatalf("refresh reconcile: %v", err)
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("requeue: got %v want 30m", result.RequeueAfter)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM: %v", err)
	}
	if _, ok := updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation]; ok {
		t.Fatalf("expected no resync annotations on experiment arm, got %#v", updated.GetAnnotations())
	}
}

func TestReconcilePrometheusScrapeToken_EmptyTokenAfterEnsure(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)

	emptySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetes.ScrapeTokenSecretName,
			Namespace: testBuildServiceNamespace,
		},
		Data: map[string][]byte{},
	}

	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             fake.NewClientBuilder().WithObjects(emptySecret).Build(),
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       &fakeTokenCreator{},
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply:              func(context.Context, *corev1.Secret) error { return nil },
	})
	if err == nil {
		t.Fatal("expected error when scrape token is empty after ensure")
	}
}

func TestReconcilePrometheusScrapeToken_ApplyServiceMonitorFails(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	applyErr := errors.New("apply failed")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetes.ScrapeTokenSecretName,
			Namespace: testBuildServiceNamespace,
			Annotations: map[string]string{
				"konflux.konflux-ci.dev/scrape-token-expires-at": now.Add(45 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{kubernetes.ScrapeTokenSecretKey: []byte("token")},
	}

	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             clientWithMetricsTLS(t, secret),
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       &fakeTokenCreator{},
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply:              func(context.Context, *corev1.Secret) error { return nil },
		ApplyServiceMonitor: func(context.Context) error {
			return applyErr
		},
	})
	if err == nil {
		t.Fatal("expected apply ServiceMonitor error")
	}
}

func TestReconcilePrometheusScrapeToken_SMNotFoundRequeuesWhenTokenUpdated(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	c := clientWithMetricsTLS(t)
	creator := &fakeTokenCreator{
		token:     "operand-token",
		expiresAt: now.Add(time.Hour),
	}

	result, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             c,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       creator,
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			return c.Create(applyCtx, secret)
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("requeue: got %v want 30m (no settle-retry)", result.RequeueAfter)
	}
}

func TestReconcilePrometheusScrapeToken_NilApplyServiceMonitor(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetes.ScrapeTokenSecretName,
			Namespace: testBuildServiceNamespace,
			Annotations: map[string]string{
				"konflux.konflux-ci.dev/scrape-token-expires-at": now.Add(45 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{kubernetes.ScrapeTokenSecretKey: []byte("token")},
	}
	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(operandServiceMonitorGVK)
	sm.SetNamespace(testBuildServiceNamespace)
	sm.SetName(testBuildServiceNamespace)

	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             clientWithMetricsTLS(t, secret, sm),
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       &fakeTokenCreator{},
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply:              func(context.Context, *corev1.Secret) error { return nil },
	})
	if err != nil {
		t.Fatalf("reconcile without ApplyServiceMonitor: %v", err)
	}
}

// laggingSecretGetClient simulates informer cache lag by returning NotFound for scrape
// token Secret reads after the write path has persisted the object.
type laggingSecretGetClient struct {
	client.Client
	blockSecretGets bool
}

func (l *laggingSecretGetClient) Get(
	ctx context.Context,
	key types.NamespacedName,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if l.blockSecretGets {
		if _, ok := obj.(*corev1.Secret); ok && key.Name == kubernetes.ScrapeTokenSecretName {
			return apierrors.NewNotFound(corev1.Resource("secrets"), key.Name)
		}
	}
	return l.Client.Get(ctx, key, obj, opts...)
}

func TestReconcilePrometheusScrapeToken_SucceedsWhenSecretCacheLagsAfterMint(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	base := clientWithMetricsTLS(t)
	lagging := &laggingSecretGetClient{Client: base}
	creator := &fakeTokenCreator{
		token:     "operand-token",
		expiresAt: now.Add(time.Hour),
	}

	result, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             lagging,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       creator,
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			if err := base.Create(applyCtx, secret); err != nil {
				return err
			}
			lagging.blockSecretGets = true
			return nil
		},
		ApplyServiceMonitor: func(applyCtx context.Context) error {
			sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
			sm.SetGroupVersionKind(operandServiceMonitorGVK)
			sm.SetNamespace(testBuildServiceNamespace)
			sm.SetName(testBuildServiceNamespace)
			return base.Create(applyCtx, sm)
		},
	})
	if err != nil {
		t.Fatalf("reconcile with lagging secret cache: %v", err)
	}
	if !lagging.blockSecretGets {
		t.Fatal("expected apply to enable lagging secret reads")
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("requeue: got %v want 30m", result.RequeueAfter)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := base.Get(ctx, types.NamespacedName{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM: %v", err)
	}
	if _, ok := updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation]; ok {
		t.Fatalf("expected no resync annotations on experiment arm, got %#v", updated.GetAnnotations())
	}
}

func TestReconcilePrometheusScrapeToken_RequeueAfterOnFreshToken(t *testing.T) {
	ctx := context.Background()
	clk := testclock.NewFakeClock(time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC))
	creator := &fakeTokenCreator{
		token:     "operand-token",
		expiresAt: clk.Now().Add(time.Hour),
	}

	c := clientWithMetricsTLS(t)
	cfg := ScrapeTokenReconcilerConfig{
		Client:           c,
		Clock:            clk,
		TokenCreator:     creator,
		Scraper:          kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace: testBuildServiceNamespace,
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			key := types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}
			existing := &corev1.Secret{}
			if err := c.Get(applyCtx, key, existing); apierrors.IsNotFound(err) {
				return c.Create(applyCtx, secret)
			} else if err != nil {
				return err
			}
			secret.SetResourceVersion(existing.ResourceVersion)
			return c.Update(applyCtx, secret)
		},
	}

	// First call mints the token. RequeueAfter = 30m (half of 1h TTL).
	result, err := ReconcilePrometheusScrapeToken(ctx, cfg)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("mint requeue: got %v want 30m", result.RequeueAfter)
	}

	// Advance 10m. Token is still fresh; no-op path should still return
	// RequeueAfter ≈ 20m (time until refresh_at).
	clk.SetTime(clk.Now().Add(10 * time.Minute))
	result, err = ReconcilePrometheusScrapeToken(ctx, cfg)
	if err != nil {
		t.Fatalf("fresh: %v", err)
	}
	if result.RequeueAfter != 20*time.Minute {
		t.Fatalf("fresh requeue: got %v want 20m", result.RequeueAfter)
	}

	// Advance to 1m before refresh_at. RequeueAfter should be floored
	// to DefaultScrapeTokenMinRequeue (1m).
	clk.SetTime(clk.Now().Add(19 * time.Minute))
	result, err = ReconcilePrometheusScrapeToken(ctx, cfg)
	if err != nil {
		t.Fatalf("near-threshold: %v", err)
	}
	if result.RequeueAfter != kubernetes.DefaultScrapeTokenMinRequeue {
		t.Fatalf("near-threshold requeue: got %v want %v",
			result.RequeueAfter, kubernetes.DefaultScrapeTokenMinRequeue)
	}

	// Advance to exactly refresh_at. With the <= boundary fix, the token
	// should be refreshed now.
	clk.SetTime(clk.Now().Add(time.Minute))
	creator.token = "refreshed-token"
	creator.expiresAt = clk.Now().Add(time.Hour)
	result, err = ReconcilePrometheusScrapeToken(ctx, cfg)
	if err != nil {
		t.Fatalf("boundary refresh: %v", err)
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("boundary refresh requeue: got %v want 30m", result.RequeueAfter)
	}
}

func TestReconcilePrometheusScrapeToken_DefersSMUntilTLSReady(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	c := fake.NewClientBuilder().Build()
	applied := 0
	result, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             c,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       &fakeTokenCreator{token: "tok", expiresAt: now.Add(time.Hour)},
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			return c.Create(applyCtx, secret)
		},
		ApplyServiceMonitor: func(context.Context) error {
			applied++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if applied != 0 {
		t.Fatalf("ApplyServiceMonitor calls: got %d want 0 while TLS missing", applied)
	}
	if result.RequeueAfter != kubernetes.DefaultMetricsTLSRequeue {
		t.Fatalf("requeue: got %v want %v", result.RequeueAfter, kubernetes.DefaultMetricsTLSRequeue)
	}
}

func TestReconcilePrometheusScrapeToken_CASyncWhenCARVChanges(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(operandServiceMonitorGVK)
	sm.SetNamespace(testBuildServiceNamespace)
	sm.SetName(testBuildServiceNamespace)
	sm.SetAnnotations(map[string]string{
		kubernetes.ServiceMonitorResyncAnnotation:         "2026-07-12T07:00:00Z",
		kubernetes.ServiceMonitorResyncSecretRVAnnotation: "200",
		kubernetes.ServiceMonitorResyncCARVAnnotation:     "ca-old",
		kubernetes.ServiceMonitorResyncReasonAnnotation:   kubernetes.ServiceMonitorResyncReasonTokenMinted,
	})

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            kubernetes.ScrapeTokenSecretName,
			Namespace:       testBuildServiceNamespace,
			ResourceVersion: "200",
			Annotations: map[string]string{
				"konflux.konflux-ci.dev/scrape-token-expires-at": now.Add(45 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{kubernetes.ScrapeTokenSecretKey: []byte("token")},
	}

	c := clientWithMetricsTLS(t, sm, secret)
	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             c,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       &fakeTokenCreator{},
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply:              func(context.Context, *corev1.Secret) error { return nil },
	})
	if err != nil {
		t.Fatalf("ca sync reconcile: %v", err)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM: %v", err)
	}
	// TEMP EXPERIMENT: ca-sync nudge disabled; seeded annotations remain.
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation] != kubernetes.ServiceMonitorResyncReasonTokenMinted {
		t.Fatalf("expected pre-existing reason unchanged, got %q", updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation])
	}
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncCARVAnnotation] != "ca-old" {
		t.Fatalf("expected seeded ca rv unchanged, got %q", updated.GetAnnotations()[kubernetes.ServiceMonitorResyncCARVAnnotation])
	}
}

func TestReconcilePrometheusScrapeToken_UsesSecretReaderNotStaleClient(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	base := clientWithMetricsTLS(t)
	blind := &metricsTLSBlindClient{Client: base}
	applied := 0

	result, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             blind,
		SecretReader:       base,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       &fakeTokenCreator{token: "tok", expiresAt: now.Add(time.Hour)},
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			return base.Create(applyCtx, secret)
		},
		ApplyServiceMonitor: func(context.Context) error {
			applied++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if applied != 1 {
		t.Fatalf("ApplyServiceMonitor calls: got %d want 1", applied)
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("requeue: got %v", result.RequeueAfter)
	}

	// Without SecretReader, the same Client cannot see metrics TLS Secrets.
	applied = 0
	result, err = ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             blind,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       &fakeTokenCreator{},
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		Apply:              func(context.Context, *corev1.Secret) error { return nil },
		ApplyServiceMonitor: func(context.Context) error {
			applied++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("reconcile without reader: %v", err)
	}
	if applied != 0 {
		t.Fatal("expected SM apply deferred when Client cannot see metrics TLS")
	}
	if result.RequeueAfter != kubernetes.DefaultMetricsTLSRequeue {
		t.Fatalf("requeue without reader: got %v", result.RequeueAfter)
	}
}

// TestReconcilePrometheusScrapeToken_RetainsOwnedServiceMonitorAcrossTLSWaitCleanup
// exercises the operand reconcile contract: ReconcilePrometheusScrapeToken then
// tracking.CleanupOrphans. When TLS is not ready, an already-owned ServiceMonitor must
// still be retained (ApplyOwned / tracked) so orphan cleanup does not delete it.
func TestReconcilePrometheusScrapeToken_RetainsOwnedServiceMonitorAcrossTLSWaitCleanup(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}

	owner := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "konflux-build-service",
			Namespace: testBuildServiceNamespace,
			UID:       "owner-uid-tls-wait",
		},
	}
	existingSM := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	existingSM.SetGroupVersionKind(operandServiceMonitorGVK)
	existingSM.SetNamespace(testBuildServiceNamespace)
	existingSM.SetName(testBuildServiceNamespace)
	existingSM.SetLabels(map[string]string{
		constant.KonfluxOwnerLabel:     owner.Name,
		constant.KonfluxComponentLabel: "build-service",
	})
	existingSM.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Name:       owner.Name,
		UID:        owner.UID,
		Controller: ptr.To(true),
	}})

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, existingSM).Build()
	// Fresh tracking client per reconcile (empty tracked set), matching controllers.
	tc := tracking.NewClientWithOwnership(c, tracking.OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     constant.KonfluxOwnerLabel,
		ComponentLabelKey: constant.KonfluxComponentLabel,
		Component:         "build-service",
		FieldManager:      "test-build-service",
	})

	desiredSM := func() *unstructured.Unstructured {
		sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
		sm.SetGroupVersionKind(operandServiceMonitorGVK)
		sm.SetNamespace(testBuildServiceNamespace)
		sm.SetName(testBuildServiceNamespace)
		return sm
	}

	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:             c,
		Clock:              testclock.NewFakeClock(now),
		TokenCreator:       &fakeTokenCreator{token: "tok", expiresAt: now.Add(time.Hour)},
		Scraper:            kubernetes.OperandMetricsScraperSA(testBuildServiceNamespace),
		OperandNamespace:   testBuildServiceNamespace,
		ServiceMonitorName: testBuildServiceNamespace,
		// No metrics-server-cert → TLS wait path (first-boot / heal).
		Apply: func(applyCtx context.Context, secret *corev1.Secret) error {
			return tc.ApplyOwned(applyCtx, secret)
		},
		ApplyServiceMonitor: func(applyCtx context.Context) error {
			return tc.ApplyOwned(applyCtx, desiredSM())
		},
	})
	if err != nil {
		t.Fatalf("reconcile scrape token: %v", err)
	}

	if err := tc.CleanupOrphans(
		ctx,
		constant.KonfluxOwnerLabel,
		owner.Name,
		kubernetes.ComponentMetricsOrphanCleanupGVKs,
	); err != nil {
		t.Fatalf("cleanup orphans: %v", err)
	}

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{
		Namespace: testBuildServiceNamespace,
		Name:      testBuildServiceNamespace,
	}, got); err != nil {
		t.Fatalf("owned ServiceMonitor must survive TLS-wait + CleanupOrphans: %v", err)
	}
}

// metricsTLSBlindClient simulates a stale informer cache that has not observed metrics TLS Secrets.
type metricsTLSBlindClient struct {
	client.Client
}

func (m *metricsTLSBlindClient) Get(
	ctx context.Context,
	key types.NamespacedName,
	obj client.Object,
	opts ...client.GetOption,
) error {
	if _, ok := obj.(*corev1.Secret); ok {
		switch key.Name {
		case kubernetes.MetricsServerCertSecretName:
			return apierrors.NewNotFound(corev1.Resource("secrets"), key.Name)
		}
	}
	return m.Client.Get(ctx, key, obj, opts...)
}
