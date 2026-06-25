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

package operatormetrics

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	testclock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

type fakeTokenCreator struct {
	token     string
	expiresAt time.Time
	calls     int
	scraper   types.NamespacedName
	err       error
}

func (f *fakeTokenCreator) CreateScraperToken(
	_ context.Context,
	scraper types.NamespacedName,
	_ time.Duration,
) (string, time.Time, error) {
	if f.err != nil {
		return "", time.Time{}, f.err
	}
	f.calls++
	f.scraper = scraper
	return f.token, f.expiresAt, nil
}

func testScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)
	return scheme
}

func testClientBuilder() *fake.ClientBuilder {
	return fake.NewClientBuilder().WithScheme(testScheme())
}

func TestScrapeTokenRotator_NeedLeaderElection(t *testing.T) {
	t.Parallel()
	rotator := &ScrapeTokenRotator{}
	if !rotator.NeedLeaderElection() {
		t.Fatal("expected scrape token rotator to require leader election")
	}
}

func TestScrapeTokenRotator_ReconcileWithoutKonfluxCR(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	creator := &fakeTokenCreator{token: "operator-scrape-token", expiresAt: now.Add(time.Hour)}
	rotator := &ScrapeTokenRotator{
		Client:       testClientBuilder().Build(),
		Clock:        testclock.NewFakeClock(now),
		TokenCreator: creator,
		Namespace:    OperatorNamespace,
	}

	if _, err := rotator.reconcile(ctx, OperatorNamespace); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if creator.calls != 1 {
		t.Fatalf("expected token mint without Konflux CR, got %d calls", creator.calls)
	}
}

func TestScrapeTokenRotator_ReconcileCreatesSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	creator := &fakeTokenCreator{token: "operator-scrape-token", expiresAt: expiresAt}
	c := testClientBuilder().Build()
	rotator := &ScrapeTokenRotator{
		Client:       c,
		Clock:        testclock.NewFakeClock(now),
		TokenCreator: creator,
		Namespace:    OperatorNamespace,
	}

	requeue, err := rotator.reconcile(ctx, OperatorNamespace)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if creator.calls != 1 {
		t.Fatalf("expected one token mint, got %d", creator.calls)
	}
	if requeue != 30*time.Minute {
		t.Fatalf("unexpected requeue: %s", requeue)
	}

	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      kubernetes.ScrapeTokenSecretName,
		Namespace: OperatorNamespace,
	}, secret); err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if string(secret.Data[kubernetes.ScrapeTokenSecretKey]) != "operator-scrape-token" {
		t.Fatalf("unexpected token bytes")
	}

	crb := &rbacv1.ClusterRoleBinding{}
	if err := c.Get(ctx, client.ObjectKey{Name: operatorMetricsReaderBinding}, crb); err != nil {
		t.Fatalf("get operator metrics CRB: %v", err)
	}
	if len(crb.Subjects) != 1 || crb.Subjects[0].Name != kubernetes.MetricsScraperServiceAccountName {
		t.Fatalf("unexpected CRB subjects: %#v", crb.Subjects)
	}
	if crb.Subjects[0].Namespace != OperatorNamespace {
		t.Fatalf("unexpected CRB subject namespace: %q", crb.Subjects[0].Namespace)
	}
	if crb.Annotations[kubernetes.MetricsScraperBindingAnnotation] != "true" {
		t.Fatalf("expected metrics-scraper-binding annotation on CRB")
	}

	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(serviceMonitorGVK)
	if err := c.Get(ctx, types.NamespacedName{
		Name:      operatorServiceMonitorName,
		Namespace: OperatorNamespace,
	}, sm); err != nil {
		t.Fatalf("get ServiceMonitor: %v", err)
	}
	endpoints, found, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
	if err != nil || !found || len(endpoints) != 1 {
		t.Fatalf("unexpected endpoints: found=%v err=%v len=%d", found, err, len(endpoints))
	}
	endpoint, ok := endpoints[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected endpoint map")
	}
	tokenSecret, ok := endpoint["bearerTokenSecret"].(map[string]interface{})
	if !ok || tokenSecret["name"] != kubernetes.ScrapeTokenSecretName {
		t.Fatalf("unexpected bearer token secret: %#v", endpoint["bearerTokenSecret"])
	}
}

func TestScrapeTokenRotator_ReconcileRefreshesStaleSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	existing := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetes.ScrapeTokenSecretName,
			Namespace: OperatorNamespace,
			Annotations: map[string]string{
				"konflux.konflux-ci.dev/scrape-token-expires-at": now.Add(20 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{
			kubernetes.ScrapeTokenSecretKey: []byte("stale"),
		},
	}
	creator := &fakeTokenCreator{
		token:     "fresh-token",
		expiresAt: now.Add(time.Hour),
	}
	c := testClientBuilder().WithObjects(existing).Build()
	rotator := &ScrapeTokenRotator{
		Client:       c,
		Clock:        testclock.NewFakeClock(now),
		TokenCreator: creator,
		Namespace:    OperatorNamespace,
	}

	if _, err := rotator.reconcile(ctx, OperatorNamespace); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if creator.calls != 1 {
		t.Fatalf("expected refresh mint, got %d calls", creator.calls)
	}

	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      kubernetes.ScrapeTokenSecretName,
		Namespace: OperatorNamespace,
	}, secret); err != nil {
		t.Fatalf("get secret: %v", err)
	}
	if string(secret.Data[kubernetes.ScrapeTokenSecretKey]) != "fresh-token" {
		t.Fatalf("expected refreshed token")
	}
}

func TestScrapeTokenRotator_ReconcileUsesOperandScraperSA(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	creator := &fakeTokenCreator{
		token:     "configured-token",
		expiresAt: now.Add(time.Hour),
	}
	c := testClientBuilder().Build()
	rotator := &ScrapeTokenRotator{
		Client:       c,
		Clock:        testclock.NewFakeClock(now),
		TokenCreator: creator,
		Namespace:    OperatorNamespace,
	}

	if _, err := rotator.reconcile(ctx, OperatorNamespace); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := kubernetes.OperandMetricsScraperSA(OperatorNamespace)
	if creator.scraper != want {
		t.Fatalf("scraper SA: got %#v want %#v", creator.scraper, want)
	}
}

func TestScrapeTokenRotator_rotationInterval(t *testing.T) {
	t.Parallel()
	if (&ScrapeTokenRotator{}).rotationInterval() != kubernetes.DefaultScrapeTokenRotationInterval {
		t.Fatal("expected default rotation interval")
	}
	custom := &ScrapeTokenRotator{Interval: 2 * time.Minute}
	if custom.rotationInterval() != 2*time.Minute {
		t.Fatal("expected custom rotation interval")
	}
}

func TestScrapeTokenRotator_StartStopsOnCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	creator := &fakeTokenCreator{token: "tok", expiresAt: now.Add(time.Hour)}
	c := testClientBuilder().Build()
	rotator := &ScrapeTokenRotator{
		Client:       c,
		Clock:        testclock.NewFakeClock(now),
		TokenCreator: creator,
		Namespace:    OperatorNamespace,
		Interval:     10 * time.Millisecond,
	}

	done := make(chan error, 1)
	go func() {
		done <- rotator.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}

func TestScrapeTokenRotator_StartLogsReconcileErrors(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	rotator := &ScrapeTokenRotator{
		Client:       testClientBuilder().Build(),
		Clock:        testclock.NewFakeClock(now),
		TokenCreator: &fakeTokenCreator{err: fmt.Errorf("mint failed")},
		Namespace:    OperatorNamespace,
	}

	done := make(chan error, 1)
	go func() {
		done <- rotator.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}

func TestScrapeTokenRotator_StartUsesDefaultNamespace(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	creator := &fakeTokenCreator{token: "tok", expiresAt: now.Add(time.Hour)}
	c := testClientBuilder().Build()
	rotator := &ScrapeTokenRotator{
		Client:       c,
		Clock:        testclock.NewFakeClock(now),
		TokenCreator: creator,
	}

	done := make(chan error, 1)
	go func() {
		done <- rotator.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancel")
	}

	secret := &corev1.Secret{}
	if err := c.Get(context.Background(), types.NamespacedName{
		Name:      kubernetes.ScrapeTokenSecretName,
		Namespace: OperatorNamespace,
	}, secret); err != nil {
		t.Fatalf("expected secret in default namespace: %v", err)
	}
}

func TestScrapeTokenRotator_ReconcileUsesRealClockWhenUnset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	creator := &fakeTokenCreator{
		token:     "tok",
		expiresAt: time.Now().Add(time.Hour),
	}
	c := testClientBuilder().Build()
	rotator := &ScrapeTokenRotator{
		Client:       c,
		TokenCreator: creator,
		Namespace:    OperatorNamespace,
	}

	if _, err := rotator.reconcile(ctx, OperatorNamespace); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}
