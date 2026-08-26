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
	"k8s.io/utils/ptr"
)

func TestShouldCreateClusterIssuer(t *testing.T) {
	t.Parallel()

	t.Run("defaults to true when nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxCertManagerSpec{}
		g.Expect(spec.ShouldCreateClusterIssuer()).To(gomega.BeTrue())
	})

	t.Run("returns true when explicitly set to true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxCertManagerSpec{CreateClusterIssuer: ptr.To(true)}
		g.Expect(spec.ShouldCreateClusterIssuer()).To(gomega.BeTrue())
	})

	t.Run("returns false when explicitly set to false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxCertManagerSpec{CreateClusterIssuer: ptr.To(false)}
		g.Expect(spec.ShouldCreateClusterIssuer()).To(gomega.BeFalse())
	})
}

func TestShouldDistributeClusterCABundle(t *testing.T) {
	t.Parallel()

	t.Run("nil on non-OpenShift defaults to true", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxCertManagerSpec{}
		g.Expect(spec.ShouldDistributeClusterCABundle(false)).To(gomega.BeTrue())
	})

	t.Run("nil on OpenShift defaults to false", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxCertManagerSpec{}
		g.Expect(spec.ShouldDistributeClusterCABundle(true)).To(gomega.BeFalse())
	})

	t.Run("explicit true overrides OpenShift default", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxCertManagerSpec{DistributeClusterCABundle: ptr.To(true)}
		g.Expect(spec.ShouldDistributeClusterCABundle(true)).To(gomega.BeTrue())
	})

	t.Run("explicit false overrides non-OpenShift default", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxCertManagerSpec{DistributeClusterCABundle: ptr.To(false)}
		g.Expect(spec.ShouldDistributeClusterCABundle(false)).To(gomega.BeFalse())
	})

	t.Run("explicit true on non-OpenShift is a no-op override", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxCertManagerSpec{DistributeClusterCABundle: ptr.To(true)}
		g.Expect(spec.ShouldDistributeClusterCABundle(false)).To(gomega.BeTrue())
	})

	t.Run("explicit false on OpenShift is a no-op override", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &KonfluxCertManagerSpec{DistributeClusterCABundle: ptr.To(false)}
		g.Expect(spec.ShouldDistributeClusterCABundle(true)).To(gomega.BeFalse())
	})
}
