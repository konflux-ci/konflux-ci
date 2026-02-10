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

package handler

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	return scheme
}

func TestMapCRDToRequest_EnqueuesWhenCRDNameIsManaged(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()

	scheme := testScheme(t)
	store, err := manifests.NewObjectStore(scheme)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	names, err := store.GetCRDNamesForComponent(manifests.ApplicationAPI)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(names).NotTo(gomega.BeEmpty(), "ApplicationAPI component should have CRDs in manifests")

	managedCRDName := names[0]
	mapFunc, err := MapCRDToRequest(store, manifests.ApplicationAPI, "konflux-application-api")
	g.Expect(err).NotTo(gomega.HaveOccurred())

	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: managedCRDName},
	}

	reqs := mapFunc(ctx, crd)
	g.Expect(reqs).To(gomega.HaveLen(1))
	g.Expect(reqs[0].Name).To(gomega.Equal("konflux-application-api"))
}

func TestMapCRDToRequest_ReturnsNilWhenCRDNameNotManaged(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()

	scheme := testScheme(t)
	store, err := manifests.NewObjectStore(scheme)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	mapFunc, err := MapCRDToRequest(store, manifests.ApplicationAPI, "konflux-application-api")
	g.Expect(err).NotTo(gomega.HaveOccurred())

	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "some.other.crd.not.in.manifest"},
	}

	reqs := mapFunc(ctx, crd)
	g.Expect(reqs).To(gomega.BeNil())
}

func TestMapCRDToRequest_ReturnsErrorWhenComponentHasNoCRDs(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()

	scheme := testScheme(t)
	store, err := manifests.NewObjectStore(scheme)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	mapFunc, err := MapCRDToRequest(store, manifests.Registry, "konflux-internal-registry")
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("no CRD names"))

	// Returned mapFunc is a no-op and returns nil
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "any.crd"},
	}
	reqs := mapFunc(ctx, crd)
	g.Expect(reqs).To(gomega.BeNil())
}

func TestMapCRDToRequest_ReturnsErrorWhenStoreErrorsForUnknownComponent(t *testing.T) {
	g := gomega.NewWithT(t)
	ctx := context.Background()

	scheme := testScheme(t)
	store, err := manifests.NewObjectStore(scheme)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	mapFunc, err := MapCRDToRequest(store, manifests.Component("unknown-component"), "some-cr")
	g.Expect(err).To(gomega.HaveOccurred())

	// Returned mapFunc is a no-op and returns nil
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "any.crd"},
	}
	reqs := mapFunc(ctx, crd)
	g.Expect(reqs).To(gomega.BeNil())
}
