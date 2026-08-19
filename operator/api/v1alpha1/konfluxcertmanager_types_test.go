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
)

func TestShouldCreateClusterIssuer(t *testing.T) {
	g := gomega.NewWithT(t)

	// nil defaults to true
	g.Expect((&KonfluxCertManagerSpec{}).ShouldCreateClusterIssuer()).To(gomega.BeTrue())

	enabled := true
	g.Expect((&KonfluxCertManagerSpec{CreateClusterIssuer: &enabled}).ShouldCreateClusterIssuer()).To(gomega.BeTrue())

	disabled := false
	g.Expect((&KonfluxCertManagerSpec{CreateClusterIssuer: &disabled}).ShouldCreateClusterIssuer()).To(gomega.BeFalse())
}

func TestShouldDistributeClusterCABundle(t *testing.T) {
	tests := []struct {
		name        string
		field       *bool
		isOpenShift bool
		want        bool
	}{
		{
			name:        "nil on non-OpenShift defaults to true",
			field:       nil,
			isOpenShift: false,
			want:        true,
		},
		{
			name:        "nil on OpenShift defaults to false",
			field:       nil,
			isOpenShift: true,
			want:        false,
		},
		{
			name:        "explicit true on non-OpenShift",
			field:       boolPtr(true),
			isOpenShift: false,
			want:        true,
		},
		{
			name:        "explicit true on OpenShift overrides default",
			field:       boolPtr(true),
			isOpenShift: true,
			want:        true,
		},
		{
			name:        "explicit false on non-OpenShift overrides default",
			field:       boolPtr(false),
			isOpenShift: false,
			want:        false,
		},
		{
			name:        "explicit false on OpenShift",
			field:       boolPtr(false),
			isOpenShift: true,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)
			spec := &KonfluxCertManagerSpec{DistributeClusterCABundle: tt.field}
			g.Expect(spec.ShouldDistributeClusterCABundle(tt.isOpenShift)).To(gomega.Equal(tt.want))
		})
	}
}

func boolPtr(b bool) *bool { return &b }
