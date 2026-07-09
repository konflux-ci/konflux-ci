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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

func TestForwardedComponentMetrics(t *testing.T) {
	t.Parallel()

	if got := ForwardedComponentMetrics(nil); got != nil {
		t.Fatalf("expected nil for nil owner, got %#v", got)
	}

	owner := &konfluxv1alpha1.Konflux{ObjectMeta: metav1.ObjectMeta{Name: "konflux"}}
	if got := ForwardedComponentMetrics(owner); got != nil {
		t.Fatalf("expected nil when componentMetrics unset, got %#v", got)
	}

	disabled := false
	owner.Spec.ComponentMetrics = &konfluxv1alpha1.ComponentMetricsConfig{
		Enabled: &disabled,
	}
	got := ForwardedComponentMetrics(owner)
	if got == nil {
		t.Fatal("expected forwarded copy")
	}
	if got == owner.Spec.ComponentMetrics {
		t.Fatal("expected deep copy")
	}
	if got.IsEnabled() {
		t.Fatal("expected disabled config in copy")
	}
	enabled := true
	owner.Spec.ComponentMetrics.Enabled = &enabled
	if got.IsEnabled() {
		t.Fatal("expected independent copy")
	}
}
