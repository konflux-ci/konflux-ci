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
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

type mockDiscoveryClient struct {
	resources map[string]*metav1.APIResourceList
}

func (m *mockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if r, ok := m.resources[groupVersion]; ok {
		return r, nil
	}
	return &metav1.APIResourceList{}, nil
}

func (m *mockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return &version.Info{GitVersion: "v1.29.0"}, nil
}

func openShiftClusterInfo(t *testing.T) *clusterinfo.Info {
	t.Helper()
	info, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			"config.openshift.io/v1": {
				APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to create OpenShift clusterinfo: %v", err)
	}
	return info
}

func newTrackingClient(t *testing.T, objs ...client.Object) (client.Client, *tracking.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add client-go scheme: %v", err)
	}
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-owner",
			Namespace: "test-namespace",
			UID:       "test-owner-uid",
		},
	}
	allObjs := append([]client.Object{owner}, objs...)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(allObjs...).
		Build()
	tc := tracking.NewClientWithOwnership(fakeClient, tracking.OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     "test.example.com/owner",
		ComponentLabelKey: "test.example.com/component",
		Component:         "test-component",
		FieldManager:      "test-manager",
	})
	return fakeClient, tc
}

func TestEnsureTrustedCAConfigMap(t *testing.T) {
	t.Run("creates ConfigMap with injection label", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		fakeClient, tc := newTrackingClient(t, ns)

		err := EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, openShiftClusterInfo(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cm := &corev1.ConfigMap{}
		if err := fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      TrustedCAConfigMapName,
			Namespace: "test-namespace",
		}, cm); err != nil {
			t.Fatalf("failed to get ConfigMap: %v", err)
		}

		if cm.Name != TrustedCAConfigMapName {
			t.Errorf("expected name %q, got %q", TrustedCAConfigMapName, cm.Name)
		}
		if cm.Namespace != "test-namespace" {
			t.Errorf("expected namespace %q, got %q", "test-namespace", cm.Namespace)
		}
		val, ok := cm.Labels[OpenShiftInjectTrustedCABundleLabel]
		if !ok {
			t.Fatal("expected injection label to be present")
		}
		if val != "true" {
			t.Errorf("expected injection label value %q, got %q", "true", val)
		}

		ownerVal, ok := cm.Labels["test.example.com/owner"]
		if !ok {
			t.Fatal("expected ownership label to be present")
		}
		if ownerVal != "test-owner" {
			t.Errorf("expected owner label value %q, got %q", "test-owner", ownerVal)
		}

		componentVal, ok := cm.Labels["test.example.com/component"]
		if !ok {
			t.Fatal("expected component label to be present")
		}
		if componentVal != "test-component" {
			t.Errorf("expected component label value %q, got %q", "test-component", componentVal)
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		existingCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      TrustedCAConfigMapName,
				Namespace: "test-namespace",
				Labels: map[string]string{
					OpenShiftInjectTrustedCABundleLabel: "true",
				},
			},
		}
		fakeClient, tc := newTrackingClient(t, ns, existingCM)

		err := EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, openShiftClusterInfo(t))
		if err != nil {
			t.Fatalf("unexpected error on second apply: %v", err)
		}

		cm := &corev1.ConfigMap{}
		if err := fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      TrustedCAConfigMapName,
			Namespace: "test-namespace",
		}, cm); err != nil {
			t.Fatalf("failed to get ConfigMap: %v", err)
		}

		val, ok := cm.Labels[OpenShiftInjectTrustedCABundleLabel]
		if !ok {
			t.Fatal("expected injection label to be present after re-apply")
		}
		if val != "true" {
			t.Errorf("expected injection label value %q, got %q", "true", val)
		}
	})

	t.Run("is a no-op when clusterInfo is nil", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		fakeClient, tc := newTrackingClient(t, ns)

		err := EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cm := &corev1.ConfigMap{}
		getErr := fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      TrustedCAConfigMapName,
			Namespace: "test-namespace",
		}, cm)
		if getErr == nil {
			t.Fatal("expected ConfigMap to not be created when clusterInfo is nil")
		}
	})

	t.Run("is a no-op on non-OpenShift clusters", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		fakeClient, tc := newTrackingClient(t, ns)

		nonOpenShiftInfo, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{
			resources: map[string]*metav1.APIResourceList{},
		})
		if err != nil {
			t.Fatalf("failed to create non-OpenShift clusterinfo: %v", err)
		}

		err = EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, nonOpenShiftInfo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cm := &corev1.ConfigMap{}
		getErr := fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      TrustedCAConfigMapName,
			Namespace: "test-namespace",
		}, cm)
		if getErr == nil {
			t.Fatal("expected ConfigMap to not be created on non-OpenShift")
		}
	})

	t.Run("does not claim data field so CNO-injected content survives re-apply", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		existingCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      TrustedCAConfigMapName,
				Namespace: "test-namespace",
				Labels: map[string]string{
					OpenShiftInjectTrustedCABundleLabel: "true",
				},
			},
			Data: map[string]string{
				"ca-bundle.crt": "-----BEGIN CERTIFICATE-----\nMIIBkTCB...\n-----END CERTIFICATE-----\n",
			},
		}
		fakeClient, tc := newTrackingClient(t, ns, existingCM)

		err := EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, openShiftClusterInfo(t))
		if err != nil {
			t.Fatalf("unexpected error on re-apply: %v", err)
		}

		cm := &corev1.ConfigMap{}
		if err := fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      TrustedCAConfigMapName,
			Namespace: "test-namespace",
		}, cm); err != nil {
			t.Fatalf("failed to get ConfigMap: %v", err)
		}

		caBundle, ok := cm.Data["ca-bundle.crt"]
		if !ok {
			t.Fatal("expected ca-bundle.crt data key to survive re-apply")
		}
		if caBundle == "" {
			t.Error("expected ca-bundle.crt to retain its value")
		}
	})

	t.Run("returns error when apply fails", func(t *testing.T) {
		scheme := runtime.NewScheme()
		if err := clientgoscheme.AddToScheme(scheme); err != nil {
			t.Fatalf("failed to add client-go scheme: %v", err)
		}
		owner := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-owner",
				Namespace: "test-namespace",
				UID:       "test-owner-uid",
			},
		}
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(owner, ns).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					return fmt.Errorf("simulated API server failure")
				},
			}).
			Build()
		tc := tracking.NewClientWithOwnership(fakeClient, tracking.OwnershipConfig{
			Owner:             owner,
			OwnerLabelKey:     "test.example.com/owner",
			ComponentLabelKey: "test.example.com/component",
			Component:         "test-component",
			FieldManager:      "test-manager",
		})

		err := EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, openShiftClusterInfo(t))
		if err == nil {
			t.Fatal("expected error when apply fails")
		}
		if !strings.Contains(err.Error(), "failed to apply trusted-ca ConfigMap in test-namespace") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}
