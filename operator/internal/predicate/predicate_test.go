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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

var testOwnerRef = metav1.OwnerReference{
	APIVersion: "konflux.konflux-ci.dev/v1alpha1",
	Kind:       "KonfluxEnterpriseContract",
	Name:       "test-owner",
	UID:        "test-uid",
}

func TestIgnoreStatusUpdatesPredicate_UpdateFunc(t *testing.T) {
	t.Run("nil objects should trigger reconciliation", func(t *testing.T) {
		g := gomega.NewWithT(t)
		e := event.UpdateEvent{}
		g.Expect(IgnoreStatusUpdatesPredicate.UpdateFunc(e)).To(gomega.BeTrue())
	})

	tests := []struct {
		name     string
		old      *corev1.ConfigMap
		new      *corev1.ConfigMap
		expected bool
	}{
		{
			name: "generation change triggers reconciliation",
			old: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
			},
			new: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 2},
			},
			expected: true,
		},
		{
			name: "status-only update does not trigger",
			old: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Generation:      1,
					OwnerReferences: []metav1.OwnerReference{testOwnerRef},
					Labels:          map[string]string{"app": "test"},
					Annotations:     map[string]string{"note": "ok"},
				},
			},
			new: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Generation:      1,
					OwnerReferences: []metav1.OwnerReference{testOwnerRef},
					Labels:          map[string]string{"app": "test"},
					Annotations:     map[string]string{"note": "ok"},
				},
			},
			expected: false,
		},
		{
			name: "ownerReference removed triggers reconciliation",
			old: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, OwnerReferences: []metav1.OwnerReference{testOwnerRef}},
			},
			new: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, OwnerReferences: []metav1.OwnerReference{}},
			},
			expected: true,
		},
		{
			name: "ownerReference added triggers reconciliation",
			old: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
			},
			new: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, OwnerReferences: []metav1.OwnerReference{testOwnerRef}},
			},
			expected: true,
		},
		{
			name: "ownerReference modified triggers reconciliation",
			old: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, OwnerReferences: []metav1.OwnerReference{testOwnerRef}},
			},
			new: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, OwnerReferences: []metav1.OwnerReference{
					{APIVersion: "v1", Kind: "Other", Name: "other", UID: "other-uid"},
				}},
			},
			expected: true,
		},
		{
			name: "label change triggers reconciliation",
			old: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, Labels: map[string]string{"app": "old"}},
			},
			new: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, Labels: map[string]string{"app": "new"}},
			},
			expected: true,
		},
		{
			name: "annotation change triggers reconciliation",
			old: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, Annotations: map[string]string{"note": "old"}},
			},
			new: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, Annotations: map[string]string{"note": "new"}},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)
			e := event.UpdateEvent{ObjectOld: tt.old, ObjectNew: tt.new}
			g.Expect(IgnoreStatusUpdatesPredicate.UpdateFunc(e)).To(gomega.Equal(tt.expected))
		})
	}
}

func TestDeploymentReadinessPredicate_MetadataChange(t *testing.T) {
	t.Run("ownerReference removed triggers reconciliation", func(t *testing.T) {
		g := gomega.NewWithT(t)
		e := event.UpdateEvent{
			ObjectOld: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, OwnerReferences: []metav1.OwnerReference{testOwnerRef}},
			},
			ObjectNew: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1},
			},
		}
		g.Expect(DeploymentReadinessPredicate.UpdateFunc(e)).To(gomega.BeTrue())
	})
	t.Run("label change triggers reconciliation", func(t *testing.T) {
		g := gomega.NewWithT(t)
		e := event.UpdateEvent{
			ObjectOld: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, Labels: map[string]string{"app": "old"}},
			},
			ObjectNew: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, Labels: map[string]string{"app": "new"}},
			},
		}
		g.Expect(DeploymentReadinessPredicate.UpdateFunc(e)).To(gomega.BeTrue())
	})
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
		{
			name: "ownerReference removed should trigger reconciliation",
			oldUI: &konfluxv1alpha1.KonfluxUI{
				ObjectMeta: metav1.ObjectMeta{Generation: 1, OwnerReferences: []metav1.OwnerReference{testOwnerRef}},
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
			expected: true,
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
