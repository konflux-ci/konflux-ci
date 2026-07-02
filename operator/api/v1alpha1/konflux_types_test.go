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

package v1alpha1

import (
	"testing"

	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestKonflux_IsReady(t *testing.T) {
	g := gomega.NewWithT(t)

	g.Expect((&Konflux{}).IsReady()).To(gomega.BeFalse())

	k := &Konflux{
		Status: KonfluxStatus{
			Conditions: []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse},
			},
		},
	}
	g.Expect(k.IsReady()).To(gomega.BeFalse())

	k.Status.Conditions[0].Status = metav1.ConditionTrue
	g.Expect(k.IsReady()).To(gomega.BeTrue())
}

func TestKonflux_ReadyConditionMessage(t *testing.T) {
	g := gomega.NewWithT(t)

	g.Expect((&Konflux{}).ReadyConditionMessage()).
		To(gomega.Equal("konflux Ready condition not found in status"))

	k := &Konflux{
		Status: KonfluxStatus{
			Conditions: []metav1.Condition{
				{
					Type:    "Ready",
					Status:  metav1.ConditionFalse,
					Reason:  "ComponentsNotReady",
					Message: "waiting for ui",
				},
			},
		},
	}
	g.Expect(k.ReadyConditionMessage()).
		To(gomega.Equal("konflux Ready=False reason=ComponentsNotReady message=waiting for ui"))
}

func TestKonfluxSpec_IsComponentMetricsEnabled(t *testing.T) {
	g := gomega.NewWithT(t)

	g.Expect((&KonfluxSpec{}).IsComponentMetricsEnabled()).To(gomega.BeTrue())

	unset := &KonfluxSpec{ComponentMetrics: &ComponentMetricsConfig{}}
	g.Expect(unset.IsComponentMetricsEnabled()).To(gomega.BeTrue())

	disabled := false
	g.Expect((&KonfluxSpec{
		ComponentMetrics: &ComponentMetricsConfig{Enabled: &disabled},
	}).IsComponentMetricsEnabled()).To(gomega.BeFalse())

	enabled := true
	g.Expect((&KonfluxSpec{
		ComponentMetrics: &ComponentMetricsConfig{Enabled: &enabled},
	}).IsComponentMetricsEnabled()).To(gomega.BeTrue())
}
