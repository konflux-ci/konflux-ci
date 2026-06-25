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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
)

func TestComponentMetricsEnabled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := konfluxv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	t.Run("defaults to true when Konflux CR is absent", func(t *testing.T) {
		t.Parallel()
		reader := fake.NewClientBuilder().WithScheme(scheme).Build()
		enabled, err := ComponentMetricsEnabled(ctx, reader)
		if err != nil {
			t.Fatalf("ComponentMetricsEnabled: %v", err)
		}
		if !enabled {
			t.Fatal("expected metrics enabled by default")
		}
	})

	t.Run("returns reader errors", func(t *testing.T) {
		t.Parallel()
		reader := &brokenReader{err: errors.New("apiserver unavailable")}
		_, err := ComponentMetricsEnabled(ctx, reader)
		if err == nil {
			t.Fatal("expected get error")
		}
	})

	t.Run("reads explicit disabled value", func(t *testing.T) {
		t.Parallel()
		disabled := false
		konflux := &konfluxv1alpha1.Konflux{
			ObjectMeta: metav1.ObjectMeta{Name: constant.KonfluxSingletonName},
			Spec: konfluxv1alpha1.KonfluxSpec{
				ComponentMetrics: &konfluxv1alpha1.ComponentMetricsConfig{Enabled: &disabled},
			},
		}
		reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(konflux).Build()
		enabled, err := ComponentMetricsEnabled(ctx, reader)
		if err != nil {
			t.Fatalf("ComponentMetricsEnabled: %v", err)
		}
		if enabled {
			t.Fatal("expected metrics disabled")
		}
	})

	t.Run("defaults to true when field is unset", func(t *testing.T) {
		t.Parallel()
		konflux := &konfluxv1alpha1.Konflux{
			ObjectMeta: metav1.ObjectMeta{Name: constant.KonfluxSingletonName},
		}
		reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(konflux).Build()
		enabled, err := ComponentMetricsEnabled(ctx, reader)
		if err != nil {
			t.Fatalf("ComponentMetricsEnabled: %v", err)
		}
		if !enabled {
			t.Fatal("expected metrics enabled when field unset")
		}
	})
}

type brokenReader struct {
	err error
}

func (b *brokenReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return b.err
}

func (b *brokenReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return b.err
}
