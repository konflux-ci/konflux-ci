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
	"k8s.io/apimachinery/pkg/types"
	testclock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
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

	c := fake.NewClientBuilder().WithObjects(sm).Build()
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
	if result.RequeueAfter != kubernetes.DefaultServiceMonitorResyncSettleDelay {
		t.Fatalf("requeue: got %v want %v", result.RequeueAfter, kubernetes.DefaultServiceMonitorResyncSettleDelay)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM: %v", err)
	}
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation] != kubernetes.ServiceMonitorResyncReasonTokenMinted {
		t.Fatalf("reason: got %q", updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation])
	}

	settleResult, err := ReconcilePrometheusScrapeToken(ctx, cfg)
	if err != nil {
		t.Fatalf("settle reconcile: %v", err)
	}
	if settleResult.RequeueAfter != 30*time.Minute {
		t.Fatalf("expected 30m requeue on settle pass, got %v", settleResult.RequeueAfter)
	}

	if err := c.Get(ctx, types.NamespacedName{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM after settle: %v", err)
	}
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation] != kubernetes.ServiceMonitorResyncReasonSettleRetry {
		t.Fatalf("settle reason: got %q", updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation])
	}
	if _, ok := updated.GetAnnotations()[kubernetes.ServiceMonitorResyncSettleAnnotation]; ok {
		t.Fatalf("expected settle pending to be cleared")
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

	c := fake.NewClientBuilder().WithObjects(sm, secret).Build()
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
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation] != kubernetes.ServiceMonitorResyncReasonSecretSync {
		t.Fatalf("reason: got %q", updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation])
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

	c := fake.NewClientBuilder().WithObjects(sm, secret).Build()
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
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation] != kubernetes.ServiceMonitorResyncReasonSettleRetry {
		t.Fatalf("expected settle-retry instead of secret-sync during settle pending, got %q",
			updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation])
	}
}

func TestReconcilePrometheusScrapeToken_AppliesServiceMonitorWhenAbsent(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	c := fake.NewClientBuilder().Build()
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
	if result.RequeueAfter != kubernetes.DefaultServiceMonitorResyncSettleDelay {
		t.Fatalf("requeue: got %v want %v", result.RequeueAfter, kubernetes.DefaultServiceMonitorResyncSettleDelay)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM: %v", err)
	}
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation] != kubernetes.ServiceMonitorResyncReasonTokenMinted {
		t.Fatalf("reason: got %q", updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation])
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

	c := fake.NewClientBuilder().WithObjects(sm, secret).Build()
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

	c := fake.NewClientBuilder().WithObjects(staleSecret, sm).Build()
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
	if result.RequeueAfter != kubernetes.DefaultServiceMonitorResyncSettleDelay {
		t.Fatalf("requeue: got %v", result.RequeueAfter)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM: %v", err)
	}
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation] != kubernetes.ServiceMonitorResyncReasonTokenRefreshed {
		t.Fatalf("reason: got %q want %q",
			updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation],
			kubernetes.ServiceMonitorResyncReasonTokenRefreshed,
		)
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
		Client:             fake.NewClientBuilder().WithObjects(secret).Build(),
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
	c := fake.NewClientBuilder().Build()
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
	if result.RequeueAfter != kubernetes.DefaultServiceMonitorResyncSettleDelay {
		t.Fatalf("requeue: got %v want settle delay", result.RequeueAfter)
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
		Client:             fake.NewClientBuilder().WithObjects(secret, sm).Build(),
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
	base := fake.NewClientBuilder().Build()
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
	if result.RequeueAfter != kubernetes.DefaultServiceMonitorResyncSettleDelay {
		t.Fatalf("requeue: got %v want %v", result.RequeueAfter, kubernetes.DefaultServiceMonitorResyncSettleDelay)
	}

	updated := &unstructured.Unstructured{}
	updated.SetGroupVersionKind(operandServiceMonitorGVK)
	if err := base.Get(ctx, types.NamespacedName{Namespace: testBuildServiceNamespace, Name: testBuildServiceNamespace}, updated); err != nil {
		t.Fatalf("get SM: %v", err)
	}
	if updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation] != kubernetes.ServiceMonitorResyncReasonTokenMinted {
		t.Fatalf("reason: got %q want %q",
			updated.GetAnnotations()[kubernetes.ServiceMonitorResyncReasonAnnotation],
			kubernetes.ServiceMonitorResyncReasonTokenMinted,
		)
	}
}

func TestReconcilePrometheusScrapeToken_RequeueAfterOnFreshToken(t *testing.T) {
	ctx := context.Background()
	clk := testclock.NewFakeClock(time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC))
	creator := &fakeTokenCreator{
		token:     "operand-token",
		expiresAt: clk.Now().Add(time.Hour),
	}

	c := fake.NewClientBuilder().Build()
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
