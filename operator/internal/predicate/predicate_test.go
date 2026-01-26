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
	"sigs.k8s.io/controller-runtime/pkg/event"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

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
