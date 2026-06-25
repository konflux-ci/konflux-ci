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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	testclock "k8s.io/utils/clock/testing"
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

func TestScrapeTokenRotator_NeedLeaderElection(t *testing.T) {
	t.Parallel()
	rotator := &ScrapeTokenRotator{}
	if !rotator.NeedLeaderElection() {
		t.Fatal("expected scrape token rotator to require leader election")
	}
}

func TestScrapeTokenRotator_ReconcileCreatesSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Hour)
	creator := &fakeTokenCreator{token: "operator-scrape-token", expiresAt: expiresAt}
	c := fake.NewClientBuilder().WithScheme(fakeScheme()).Build()
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
	c := fake.NewClientBuilder().WithScheme(fakeScheme()).WithObjects(existing).Build()
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

func TestScrapeTokenRotator_ReconcileUsesKindScraperOnNonOpenShift(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	creator := &fakeTokenCreator{
		token:     "kind-token",
		expiresAt: now.Add(time.Hour),
	}
	c := fake.NewClientBuilder().WithScheme(fakeScheme()).Build()
	rotator := &ScrapeTokenRotator{
		Client:       c,
		Clock:        testclock.NewFakeClock(now),
		TokenCreator: creator,
		Namespace:    OperatorNamespace,
	}

	if _, err := rotator.reconcile(ctx, OperatorNamespace); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := kubernetes.PrimaryScraperServiceAccount(false)
	if creator.scraper != want {
		t.Fatalf("scraper SA: got %#v want %#v", creator.scraper, want)
	}
}

func TestScrapeTokenRotator_StartStopsOnCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	creator := &fakeTokenCreator{token: "tok", expiresAt: now.Add(time.Hour)}
	c := fake.NewClientBuilder().WithScheme(fakeScheme()).Build()
	rotator := &ScrapeTokenRotator{
		Client:       c,
		Clock:        testclock.NewFakeClock(now),
		TokenCreator: creator,
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

func TestScrapeTokenRotator_StartLogsReconcileErrors(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())

	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	rotator := &ScrapeTokenRotator{
		Client:       fake.NewClientBuilder().WithScheme(fakeScheme()).Build(),
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
	c := fake.NewClientBuilder().WithScheme(fakeScheme()).Build()
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
	c := fake.NewClientBuilder().WithScheme(fakeScheme()).Build()
	rotator := &ScrapeTokenRotator{
		Client:       c,
		TokenCreator: creator,
		Namespace:    OperatorNamespace,
	}

	if _, err := rotator.reconcile(ctx, OperatorNamespace); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func fakeScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	return scheme
}
