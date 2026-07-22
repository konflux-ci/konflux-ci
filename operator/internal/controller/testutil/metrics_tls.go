/*
Copyright 2026 Konflux CI.

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

package testutil

import (
	"context"

	. "github.com/onsi/gomega" //nolint:staticcheck // dot imports are standard for Gomega matchers

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

// EnsureMetricsTLSSecrets creates a metrics-server-cert Secret with tls.crt and ca.crt so
// deferred ServiceMonitor apply can proceed in envtest (no cert-manager controller).
func EnsureMetricsTLSSecrets(ctx context.Context, c client.Client, namespace string) {
	err := c.Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})
	Expect(client.IgnoreAlreadyExists(err)).NotTo(HaveOccurred())

	caPEM, leafPEM, err := kubernetes.NewSelfSignedMetricsTLSMaterial()
	Expect(err).NotTo(HaveOccurred())

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetes.MetricsServerCertSecretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			kubernetes.MetricsCACertKey:            caPEM,
			kubernetes.MetricsServerCertTLSCertKey: leafPEM,
		},
	}

	existing := &corev1.Secret{}
	err = c.Get(ctx, client.ObjectKeyFromObject(secret), existing)
	switch {
	case err == nil:
		existing.Data = secret.Data
		Expect(c.Update(ctx, existing)).To(Succeed())
	case apierrors.IsNotFound(err):
		Expect(c.Create(ctx, secret)).To(Succeed())
	default:
		Expect(err).NotTo(HaveOccurred())
	}
}

// DeleteMetricsTLSSecrets removes metrics-server-cert so envtests can exercise the
// deferred ServiceMonitor path while TLS is not ready.
func DeleteMetricsTLSSecrets(ctx context.Context, c client.Client, namespace string) {
	_ = client.IgnoreNotFound(c.Delete(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kubernetes.MetricsServerCertSecretName,
			Namespace: namespace,
		},
	}))
}

// ExpectVerifiedMetricsEndpointTLS asserts verified scrape tlsConfig
// (CA from metrics-server-cert ca.crt + serverName, insecureSkipVerify false).
func ExpectVerifiedMetricsEndpointTLS(g Gomega, endpoint map[string]any, wantServerName string) {
	tlsConfig, ok := endpoint["tlsConfig"].(map[string]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(tlsConfig["insecureSkipVerify"]).To(BeFalse())
	g.Expect(tlsConfig["serverName"]).To(Equal(wantServerName))
	ca, ok := tlsConfig["ca"].(map[string]any)
	g.Expect(ok).To(BeTrue())
	caSecret, ok := ca["secret"].(map[string]any)
	g.Expect(ok).To(BeTrue())
	g.Expect(caSecret["name"]).To(Equal(kubernetes.MetricsServerCertSecretName))
	g.Expect(caSecret["key"]).To(Equal(kubernetes.MetricsCACertKey))
}
