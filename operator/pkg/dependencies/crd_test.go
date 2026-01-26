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

package dependencies

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestIsCertManagerInstalled(t *testing.T) {
	tests := []struct {
		name           string
		setupClient    func() client.Client
		expectedResult bool
	}{
		{
			name: "cert-manager installed - all CRDs exist",
			setupClient: func() client.Client {
				scheme := setupScheme()
				return fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(
						&apiextensionsv1.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								Name: "certificates.cert-manager.io",
							},
						},
						&apiextensionsv1.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								Name: "issuers.cert-manager.io",
							},
						},
						&apiextensionsv1.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								Name: "clusterissuers.cert-manager.io",
							},
						},
					).
					Build()
			},
			expectedResult: true,
		},
		{
			name: "cert-manager not installed - only certificates CRD exists",
			setupClient: func() client.Client {
				scheme := setupScheme()
				return fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&apiextensionsv1.CustomResourceDefinition{
						ObjectMeta: metav1.ObjectMeta{
							Name: "certificates.cert-manager.io",
						},
					}).
					Build()
			},
			expectedResult: false,
		},
		{
			name: "cert-manager not installed - only issuers CRD exists",
			setupClient: func() client.Client {
				scheme := setupScheme()
				return fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&apiextensionsv1.CustomResourceDefinition{
						ObjectMeta: metav1.ObjectMeta{
							Name: "issuers.cert-manager.io",
						},
					}).
					Build()
			},
			expectedResult: false,
		},
		{
			name: "cert-manager not installed - only clusterissuers CRD exists",
			setupClient: func() client.Client {
				scheme := setupScheme()
				return fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(&apiextensionsv1.CustomResourceDefinition{
						ObjectMeta: metav1.ObjectMeta{
							Name: "clusterissuers.cert-manager.io",
						},
					}).
					Build()
			},
			expectedResult: false,
		},
		{
			name: "cert-manager not installed - two CRDs exist but one missing",
			setupClient: func() client.Client {
				scheme := setupScheme()
				return fake.NewClientBuilder().
					WithScheme(scheme).
					WithObjects(
						&apiextensionsv1.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								Name: "certificates.cert-manager.io",
							},
						},
						&apiextensionsv1.CustomResourceDefinition{
							ObjectMeta: metav1.ObjectMeta{
								Name: "issuers.cert-manager.io",
							},
						},
					).
					Build()
			},
			expectedResult: false,
		},
		{
			name: "cert-manager not installed - no CRDs exist",
			setupClient: func() client.Client {
				scheme := setupScheme()
				return fake.NewClientBuilder().
					WithScheme(scheme).
					Build()
			},
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()

			client := tt.setupClient()
			result, err := IsCertManagerInstalled(ctx, client)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(result).To(Equal(tt.expectedResult))
		})
	}
}

// Helper function to set up scheme with required types
func setupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = apiextensionsv1.AddToScheme(scheme)
	return scheme
}
