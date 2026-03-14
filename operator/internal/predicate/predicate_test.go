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

package predicate

import (
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

func TestGenerationChangedPredicate_UpdateFunc(t *testing.T) {
	t.Run("nil objects should trigger reconciliation", func(t *testing.T) {
		g := gomega.NewWithT(t)
		e := event.UpdateEvent{}
		result := GenerationChangedPredicate.UpdateFunc(e)
		g.Expect(result).To(gomega.BeTrue())
	})

	tests := []struct {
		name        string
		oldGen      int64
		newGen      int64
		description string
		expected    bool
	}{
		{
			name:        "status-only update (same generation) should NOT trigger reconciliation",
			oldGen:      3,
			newGen:      3,
			description: "This is the infinite loop scenario: Status().Update() bumps ResourceVersion but not Generation",
			expected:    false,
		},
		{
			name:        "spec change (generation bumped) should trigger reconciliation",
			oldGen:      3,
			newGen:      4,
			description: "A real spec change from a user or controller bumps Generation",
			expected:    true,
		},
		{
			name:        "generation 0 to 0 (status-only on resources without generation) should NOT trigger",
			oldGen:      0,
			newGen:      0,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)

			// Use a ConfigMap to represent any object that receives a status-only update.
			// When a controller calls Status().Update(), the API server increments ResourceVersion
			// but does NOT increment Generation (generation only changes on spec modifications).
			// Without GenerationChangedPredicate on For(), every Status().Update() would fire a
			// watch event that passes all predicates, enqueuing another reconcile and causing
			// an infinite loop.
			e := event.UpdateEvent{
				ObjectOld: &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-object",
						Generation: tt.oldGen,
					},
				},
				ObjectNew: &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:       "test-object",
						Generation: tt.newGen,
					},
				},
			}

			result := GenerationChangedPredicate.UpdateFunc(e)
			g.Expect(result).To(gomega.Equal(tt.expected))
		})
	}
}

func TestGenerationChangedPredicate_CreateDeleteGenericFunc(t *testing.T) {
	g := gomega.NewWithT(t)

	obj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

	g.Expect(GenerationChangedPredicate.CreateFunc(event.CreateEvent{Object: obj})).To(gomega.BeTrue())
	g.Expect(GenerationChangedPredicate.DeleteFunc(event.DeleteEvent{Object: obj})).To(gomega.BeTrue())
	g.Expect(GenerationChangedPredicate.GenericFunc(event.GenericEvent{Object: obj})).To(gomega.BeTrue())
}

func TestKonfluxUIIngressStatusChangedPredicate_UpdateFunc(t *testing.T) {
	t.Run("nil objects should trigger reconciliation", func(t *testing.T) {
		g := gomega.NewWithT(t)
		// Use empty UpdateEvent with nil interface values (not typed nil pointers)
		e := event.UpdateEvent{}
		result := KonfluxUIIngressStatusChangedPredicate.UpdateFunc(e)
		g.Expect(result).To(gomega.BeTrue())
	})

	tests := []struct {
		name     string
		oldUI    *konfluxv1alpha1.KonfluxUI
		newUI    *konfluxv1alpha1.KonfluxUI
		expected bool
	}{
		{
			name: "generation change should trigger reconciliation",
			oldUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
			},
			newUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
			},
			expected: true,
		},
		{
			name: "both ingress nil should not trigger reconciliation",
			oldUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     konfluxv1alpha1.KonfluxUIStatus{Ingress: nil},
			},
			newUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     konfluxv1alpha1.KonfluxUIStatus{Ingress: nil},
			},
			expected: false,
		},
		{
			name: "old ingress nil, new ingress set should trigger reconciliation",
			oldUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     konfluxv1alpha1.KonfluxUIStatus{Ingress: nil},
			},
			newUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: konfluxv1alpha1.KonfluxUIStatus{
					Ingress: &konfluxv1alpha1.IngressStatus{
						Enabled: true,
						URL:     "https://konflux.example.com",
					},
				},
			},
			expected: true,
		},
		{
			name: "old ingress set, new ingress nil should trigger reconciliation",
			oldUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: konfluxv1alpha1.KonfluxUIStatus{
					Ingress: &konfluxv1alpha1.IngressStatus{
						Enabled: true,
						URL:     "https://konflux.example.com",
					},
				},
			},
			newUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status:     konfluxv1alpha1.KonfluxUIStatus{Ingress: nil},
			},
			expected: true,
		},
		{
			name: "URL change should trigger reconciliation",
			oldUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: konfluxv1alpha1.KonfluxUIStatus{
					Ingress: &konfluxv1alpha1.IngressStatus{
						Enabled: true,
						URL:     "https://old.example.com",
					},
				},
			},
			newUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: konfluxv1alpha1.KonfluxUIStatus{
					Ingress: &konfluxv1alpha1.IngressStatus{
						Enabled: true,
						URL:     "https://new.example.com",
					},
				},
			},
			expected: true,
		},
		{
			name: "Hostname change should trigger reconciliation",
			oldUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: konfluxv1alpha1.KonfluxUIStatus{
					Ingress: &konfluxv1alpha1.IngressStatus{
						Enabled:  true,
						Hostname: "old.example.com",
					},
				},
			},
			newUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: konfluxv1alpha1.KonfluxUIStatus{
					Ingress: &konfluxv1alpha1.IngressStatus{
						Enabled:  true,
						Hostname: "new.example.com",
					},
				},
			},
			expected: true,
		},
		{
			name: "Enabled change should trigger reconciliation",
			oldUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: konfluxv1alpha1.KonfluxUIStatus{
					Ingress: &konfluxv1alpha1.IngressStatus{
						Enabled: false,
					},
				},
			},
			newUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: konfluxv1alpha1.KonfluxUIStatus{
					Ingress: &konfluxv1alpha1.IngressStatus{
						Enabled: true,
					},
				},
			},
			expected: true,
		},
		{
			name: "no ingress change should not trigger reconciliation",
			oldUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: konfluxv1alpha1.KonfluxUIStatus{
					Ingress: &konfluxv1alpha1.IngressStatus{
						Enabled:  true,
						Hostname: "konflux.example.com",
						URL:      "https://konflux.example.com",
					},
				},
			},
			newUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
				Status: konfluxv1alpha1.KonfluxUIStatus{
					Ingress: &konfluxv1alpha1.IngressStatus{
						Enabled:  true,
						Hostname: "konflux.example.com",
						URL:      "https://konflux.example.com",
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)

			e := event.UpdateEvent{
				ObjectOld: tt.oldUI,
				ObjectNew: tt.newUI,
			}

			result := KonfluxUIIngressStatusChangedPredicate.UpdateFunc(e)
			g.Expect(result).To(gomega.Equal(tt.expected))
		})
	}
}

func TestKonfluxUIIngressStatusChangedPredicate_CreateFunc(t *testing.T) {
	g := gomega.NewWithT(t)

	e := event.CreateEvent{
		Object: &konfluxv1alpha1.KonfluxUI{
			ObjectMeta: metav1.ObjectMeta{Name: "konflux-ui"},
		},
	}

	result := KonfluxUIIngressStatusChangedPredicate.CreateFunc(e)
	g.Expect(result).To(gomega.BeTrue())
}

func TestKonfluxUIIngressStatusChangedPredicate_DeleteFunc(t *testing.T) {
	g := gomega.NewWithT(t)

	e := event.DeleteEvent{
		Object: &konfluxv1alpha1.KonfluxUI{
			ObjectMeta: metav1.ObjectMeta{Name: "konflux-ui"},
		},
	}

	result := KonfluxUIIngressStatusChangedPredicate.DeleteFunc(e)
	g.Expect(result).To(gomega.BeTrue())
}

func TestKonfluxUIIngressStatusChangedPredicate_GenericFunc(t *testing.T) {
	g := gomega.NewWithT(t)

	e := event.GenericEvent{
		Object: &konfluxv1alpha1.KonfluxUI{
			ObjectMeta: metav1.ObjectMeta{Name: "konflux-ui"},
		},
	}

	result := KonfluxUIIngressStatusChangedPredicate.GenericFunc(e)
	g.Expect(result).To(gomega.BeTrue())
}
