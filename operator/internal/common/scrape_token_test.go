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
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	testclock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

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
	t.Parallel()
	ctx := context.Background()

	err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{})
	if err == nil {
		t.Fatal("expected error when token creator is nil")
	}

	err = ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		TokenCreator: &fakeTokenCreator{},
	})
	if err == nil {
		t.Fatal("expected error when operand namespace is empty")
	}
}

func TestReconcilePrometheusScrapeToken_CreatesToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	creator := &fakeTokenCreator{
		token:     "operand-token",
		expiresAt: now.Add(time.Hour),
	}
	scraper := kubernetes.OperandMetricsScraperSA("build-service")

	err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:           &absentSecretReader{},
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     creator,
		Scraper:          scraper,
		OperandNamespace: "build-service",
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
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	mintErr := errors.New("mint failed")

	err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:           &absentSecretReader{},
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     &fakeTokenCreator{err: mintErr},
		Scraper:          types.NamespacedName{Namespace: "monitoring", Name: "prometheus"},
		OperandNamespace: "build-service",
		Apply:            func(context.Context, *corev1.Secret) error { return nil },
	})
	if err == nil {
		t.Fatal("expected mint error")
	}
}

func TestReconcilePrometheusScrapeToken_TracksFreshSecret(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	fresh := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetes.ScrapeTokenSecretName,
			Namespace: "build-service",
			Annotations: map[string]string{
				"konflux.konflux-ci.dev/scrape-token-expires-at": now.Add(45 * time.Minute).UTC().Format(time.RFC3339),
			},
		},
		Data: map[string][]byte{kubernetes.ScrapeTokenSecretKey: []byte("existing")},
	}

	err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:           &existingSecretReader{secret: fresh},
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     &fakeTokenCreator{},
		Scraper:          kubernetes.OperandMetricsScraperSA("build-service"),
		OperandNamespace: "build-service",
		Apply:            func(context.Context, *corev1.Secret) error { return nil },
	})
	if err != nil {
		t.Fatalf("reconcile fresh secret: %v", err)
	}
}

type existingSecretReader struct {
	secret *corev1.Secret
}

func (r *existingSecretReader) Get(
	_ context.Context,
	key types.NamespacedName,
	obj client.Object,
	_ ...client.GetOption,
) error {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return fmt.Errorf("unexpected type %T", obj)
	}
	if r.secret == nil || key.Name != r.secret.Name || key.Namespace != r.secret.Namespace {
		return apierrors.NewNotFound(corev1.Resource("secrets"), key.Name)
	}
	*secret = *r.secret
	return nil
}

func (existingSecretReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return fmt.Errorf("not implemented")
}

type absentSecretReader struct{}

func (absentSecretReader) Get(
	_ context.Context,
	key types.NamespacedName,
	obj client.Object,
	_ ...client.GetOption,
) error {
	if _, ok := obj.(*corev1.Secret); !ok {
		return fmt.Errorf("unexpected type %T", obj)
	}
	return apierrors.NewNotFound(corev1.Resource("secrets"), key.Name)
}

func (absentSecretReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return fmt.Errorf("not implemented")
}
