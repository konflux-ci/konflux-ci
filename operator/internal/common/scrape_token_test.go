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
	"k8s.io/apimachinery/pkg/version"
	testclock "k8s.io/utils/clock/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

type commonFakeTokenCreator struct {
	token     string
	expiresAt time.Time
	err       error
	scraper   types.NamespacedName
}

func (f *commonFakeTokenCreator) CreateScraperToken(
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

	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{})
	if err == nil {
		t.Fatal("expected error when token creator is nil")
	}

	_, err = ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		TokenCreator: &commonFakeTokenCreator{},
	})
	if err == nil {
		t.Fatal("expected error when operand namespace is empty")
	}
}

func TestReconcilePrometheusScrapeToken_CreatesTokenAndRequeues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	creator := &commonFakeTokenCreator{
		token:     "operand-token",
		expiresAt: now.Add(time.Hour),
	}

	result, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:           &absentSecretReader{},
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     creator,
		OperandNamespace: "build-service",
		Apply: func(_ context.Context, _ *corev1.Secret) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.RequeueAfter != 30*time.Minute {
		t.Fatalf("unexpected requeue: %s", result.RequeueAfter)
	}
	want := kubernetes.PrimaryScraperServiceAccount(false)
	if creator.scraper != want {
		t.Fatalf("scraper SA: got %#v want %#v", creator.scraper, want)
	}
}

func TestReconcilePrometheusScrapeToken_UsesOpenShiftScraper(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	creator := &commonFakeTokenCreator{
		token:     "ocp-token",
		expiresAt: now.Add(time.Hour),
	}
	clusterInfo, err := clusterinfo.DetectWithClient(&clusterinfoMockDiscovery{
		resources: map[string]*metav1.APIResourceList{
			"config.openshift.io/v1": {
				APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
			},
		},
		serverVersion: &version.Info{GitVersion: "v1.29.0"},
	})
	if err != nil {
		t.Fatalf("detect openshift: %v", err)
	}

	_, err = ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:           &absentSecretReader{},
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     creator,
		ClusterInfo:      clusterInfo,
		OperandNamespace: "build-service",
		Apply:            func(context.Context, *corev1.Secret) error { return nil },
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := kubernetes.PrimaryScraperServiceAccount(true)
	if creator.scraper != want {
		t.Fatalf("scraper SA: got %#v want %#v", creator.scraper, want)
	}
}

func TestReconcilePrometheusScrapeToken_PropagatesErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	mintErr := errors.New("mint failed")

	_, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:           &absentSecretReader{},
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     &commonFakeTokenCreator{err: mintErr},
		OperandNamespace: "build-service",
		Apply:            func(context.Context, *corev1.Secret) error { return nil },
	})
	if err == nil {
		t.Fatal("expected mint error")
	}
}

func TestMergeRequeueAfter(t *testing.T) {
	t.Parallel()

	base := ctrl.Result{RequeueAfter: 10 * time.Minute}
	if got := MergeRequeueAfter(base, ctrl.Result{}); got.RequeueAfter != 10*time.Minute {
		t.Fatalf("expected unchanged result, got %s", got.RequeueAfter)
	}

	got := MergeRequeueAfter(base, ctrl.Result{RequeueAfter: 5 * time.Minute})
	if got.RequeueAfter != 5*time.Minute {
		t.Fatalf("expected shorter requeue, got %s", got.RequeueAfter)
	}

	got = MergeRequeueAfter(ctrl.Result{}, ctrl.Result{RequeueAfter: 2 * time.Minute})
	if got.RequeueAfter != 2*time.Minute {
		t.Fatalf("expected extra requeue when base empty, got %s", got.RequeueAfter)
	}
}

type clusterinfoMockDiscovery struct {
	resources     map[string]*metav1.APIResourceList
	serverVersion *version.Info
}

func (m *clusterinfoMockDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if list, ok := m.resources[groupVersion]; ok {
		return list, nil
	}
	return nil, errors.New("not found")
}

func (m *clusterinfoMockDiscovery) ServerVersion() (*version.Info, error) {
	return m.serverVersion, nil
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

	result, err := ReconcilePrometheusScrapeToken(ctx, ScrapeTokenReconcilerConfig{
		Client:           &existingSecretReader{secret: fresh},
		Clock:            testclock.NewFakeClock(now),
		TokenCreator:     &commonFakeTokenCreator{},
		OperandNamespace: "build-service",
		Apply:            func(context.Context, *corev1.Secret) error { return nil },
	})
	if err != nil {
		t.Fatalf("reconcile fresh secret: %v", err)
	}
	if result.RequeueAfter != 15*time.Minute {
		t.Fatalf("expected requeue, got %s", result.RequeueAfter)
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
