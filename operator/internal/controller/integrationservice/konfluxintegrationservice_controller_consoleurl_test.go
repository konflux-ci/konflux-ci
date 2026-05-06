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

package integrationservice

import (
	"context"
	"errors"
	"testing"

	"github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/ui"
)

type failingKonfluxUIGetClient struct {
	client.Client
	err error
}

func (c *failingKonfluxUIGetClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*konfluxv1alpha1.KonfluxUI); ok && key.Name == ui.CRName {
		return c.err
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func TestGetConsoleURLFromKonfluxUI(t *testing.T) {
	t.Run("returns URL when KonfluxUI ingress is set", func(t *testing.T) {
		g := gomega.NewWithT(t)

		s := runtime.NewScheme()
		err := konfluxv1alpha1.AddToScheme(s)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		konfluxUI := &konfluxv1alpha1.KonfluxUI{
			ObjectMeta: metav1.ObjectMeta{Name: ui.CRName},
			Status: konfluxv1alpha1.KonfluxUIStatus{
				Ingress: &konfluxv1alpha1.IngressStatus{URL: testConsoleURL},
			},
		}

		c := fake.NewClientBuilder().WithScheme(s).WithObjects(konfluxUI).Build()

		consoleURL, err := getConsoleURLFromKonfluxUI(context.Background(), c)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(consoleURL).To(gomega.Equal(testConsoleURL))
	})

	t.Run("returns empty URL when KonfluxUI is not found", func(t *testing.T) {
		g := gomega.NewWithT(t)

		s := runtime.NewScheme()
		err := konfluxv1alpha1.AddToScheme(s)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		c := fake.NewClientBuilder().WithScheme(s).Build()

		consoleURL, err := getConsoleURLFromKonfluxUI(context.Background(), c)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(consoleURL).To(gomega.BeEmpty())
	})

	t.Run("returns error on non-NotFound KonfluxUI get failures", func(t *testing.T) {
		g := gomega.NewWithT(t)

		s := runtime.NewScheme()
		err := konfluxv1alpha1.AddToScheme(s)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		baseClient := fake.NewClientBuilder().WithScheme(s).Build()
		getErr := apierrors.NewForbidden(
			schema.GroupResource{Group: "konflux.konflux-ci.dev", Resource: "konfluxuis"},
			ui.CRName,
			errors.New("rbac denied"),
		)
		c := &failingKonfluxUIGetClient{Client: baseClient, err: getErr}

		consoleURL, err := getConsoleURLFromKonfluxUI(context.Background(), c)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("failed to get KonfluxUI"))
		g.Expect(apierrors.IsForbidden(err)).To(gomega.BeTrue())
		g.Expect(consoleURL).To(gomega.BeEmpty())
	})
}
