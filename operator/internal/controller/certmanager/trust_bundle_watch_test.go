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

package certmanager

import (
	"errors"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestTrustManagerBundleWatchObjectIfInstalled(t *testing.T) {
	t.Parallel()

	t.Run("nil mapper", func(t *testing.T) {
		t.Parallel()
		if bundle, ok := trustManagerBundleWatchObjectIfInstalled(nil); ok || bundle != nil {
			t.Fatalf("expected nil mapper to skip watch object, got ok=%v bundle=%v", ok, bundle)
		}
	})

	t.Run("CRD absent", func(t *testing.T) {
		t.Parallel()
		mapper := meta.NewDefaultRESTMapper(nil)
		if bundle, ok := trustManagerBundleWatchObjectIfInstalled(mapper); ok || bundle != nil {
			t.Fatalf("expected absent CRD to skip watch object, got ok=%v bundle=%v", ok, bundle)
		}
	})

	t.Run("CRD present", func(t *testing.T) {
		t.Parallel()
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{bundleGVK.GroupVersion()})
		mapper.Add(bundleGVK, meta.RESTScopeRoot)
		bundle, ok := trustManagerBundleWatchObjectIfInstalled(mapper)
		if !ok || bundle == nil {
			t.Fatal("expected watch object when Bundle CRD is mapped")
		}
		if bundle.GroupVersionKind() != bundleGVK {
			t.Fatalf("unexpected GVK: %#v", bundle.GroupVersionKind())
		}
	})

	t.Run("non-NoMatch mapping error", func(t *testing.T) {
		t.Parallel()
		bundle, ok := trustManagerBundleWatchObjectIfInstalled(errRESTMapper{err: errors.New("discovery unavailable")})
		if ok || bundle != nil {
			t.Fatalf("expected non-NoMatch mapping error to skip watch object, got ok=%v bundle=%v", ok, bundle)
		}
	})
}

// errRESTMapper returns a fixed RESTMapping error for Bundle lookups.
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

func TestStripForeignControllerOwnerRefs(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false
	ours := metav1.OwnerReference{Kind: crKind, Name: CRName, UID: "cm", Controller: &trueVal}
	registry := metav1.OwnerReference{Kind: "KonfluxInternalRegistry", Name: "konflux-internal-registry", UID: "reg", Controller: &trueVal}
	nonController := metav1.OwnerReference{Kind: "KonfluxInternalRegistry", Name: "konflux-internal-registry", UID: "nc", Controller: &falseVal}

	t.Run("strips foreign controller and keeps ours", func(t *testing.T) {
		t.Parallel()
		kept, stripped := stripForeignControllerOwnerRefs([]metav1.OwnerReference{registry, ours})
		if len(stripped) != 1 || stripped[0].UID != "reg" {
			t.Fatalf("stripped=%#v", stripped)
		}
		if len(kept) != 1 || kept[0].UID != "cm" {
			t.Fatalf("kept=%#v", kept)
		}
	})

	t.Run("keeps non-controller refs", func(t *testing.T) {
		t.Parallel()
		kept, stripped := stripForeignControllerOwnerRefs([]metav1.OwnerReference{nonController})
		if len(stripped) != 0 {
			t.Fatalf("stripped=%#v", stripped)
		}
		if len(kept) != 1 || kept[0].UID != "nc" {
			t.Fatalf("kept=%#v", kept)
		}
	})

	t.Run("no-op when already ours", func(t *testing.T) {
		t.Parallel()
		kept, stripped := stripForeignControllerOwnerRefs([]metav1.OwnerReference{ours})
		if len(stripped) != 0 {
			t.Fatalf("stripped=%#v", stripped)
		}
		if len(kept) != 1 || kept[0].UID != "cm" {
			t.Fatalf("kept=%#v", kept)
		}
	})
}
