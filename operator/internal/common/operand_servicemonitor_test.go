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
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestOperandServiceMonitorFromObjects(t *testing.T) {
	t.Parallel()

	sm := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	sm.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "ServiceMonitor",
	})
	sm.SetNamespace(testBuildServiceNamespace)
	sm.SetName(testBuildServiceNamespace)

	other := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{}}}
	other.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "monitoring.coreos.com",
		Version: "v1",
		Kind:    "ServiceMonitor",
	})
	other.SetNamespace("image-controller")
	other.SetName("image-controller")

	objects := []client.Object{
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "noop"}},
		sm,
		other,
	}

	got, ok := OperandServiceMonitorFromObjects(objects, testBuildServiceNamespace, testBuildServiceNamespace)
	if !ok {
		t.Fatal("expected to find build-service ServiceMonitor")
	}
	if got.GetName() != testBuildServiceNamespace || got.GetNamespace() != testBuildServiceNamespace {
		t.Fatalf("unexpected SM: %s/%s", got.GetNamespace(), got.GetName())
	}

	if _, ok := OperandServiceMonitorFromObjects(objects, testBuildServiceNamespace, "missing"); ok {
		t.Fatal("expected missing ServiceMonitor to not be found")
	}
}

func TestOperandServiceMonitorWatchObjectIfInstalled(t *testing.T) {
	t.Parallel()

	t.Run("nil mapper", func(t *testing.T) {
		t.Parallel()
		if sm, ok := OperandServiceMonitorWatchObjectIfInstalled(nil); ok || sm != nil {
			t.Fatalf("expected nil mapper to skip watch object, got ok=%v sm=%v", ok, sm)
		}
	})

	t.Run("CRD absent", func(t *testing.T) {
		t.Parallel()
		mapper := meta.NewDefaultRESTMapper(nil)
		if sm, ok := OperandServiceMonitorWatchObjectIfInstalled(mapper); ok || sm != nil {
			t.Fatalf("expected absent CRD to skip watch object, got ok=%v sm=%v", ok, sm)
		}
	})

	t.Run("CRD present", func(t *testing.T) {
		t.Parallel()
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{operandServiceMonitorGVK.GroupVersion()})
		mapper.Add(operandServiceMonitorGVK, meta.RESTScopeNamespace)
		sm, ok := OperandServiceMonitorWatchObjectIfInstalled(mapper)
		if !ok || sm == nil {
			t.Fatal("expected watch object when ServiceMonitor CRD is mapped")
		}
		if sm.GroupVersionKind() != operandServiceMonitorGVK {
			t.Fatalf("unexpected GVK: %#v", sm.GroupVersionKind())
		}
	})

	t.Run("non-NoMatch mapping error", func(t *testing.T) {
		t.Parallel()
		sm, ok := OperandServiceMonitorWatchObjectIfInstalled(errRESTMapper{err: errors.New("discovery unavailable")})
		if ok || sm != nil {
			t.Fatalf("expected non-NoMatch mapping error to skip watch object, got ok=%v sm=%v", ok, sm)
		}
	})
}

// errRESTMapper returns a fixed RESTMapping error for ServiceMonitor lookups.
type errRESTMapper struct {
	err error
}

func (m errRESTMapper) KindFor(_ schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, m.err
}
func (m errRESTMapper) KindsFor(_ schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	return nil, m.err
}
func (m errRESTMapper) ResourceFor(_ schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	return schema.GroupVersionResource{}, m.err
}
func (m errRESTMapper) ResourcesFor(_ schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	return nil, m.err
}
func (m errRESTMapper) RESTMapping(_ schema.GroupKind, _ ...string) (*meta.RESTMapping, error) {
	return nil, m.err
}
func (m errRESTMapper) RESTMappings(_ schema.GroupKind, _ ...string) ([]*meta.RESTMapping, error) {
	return nil, m.err
}
func (m errRESTMapper) ResourceSingularizer(_ string) (string, error) {
	return "", m.err
}
