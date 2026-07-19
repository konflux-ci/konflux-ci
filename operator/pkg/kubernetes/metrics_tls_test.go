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

package kubernetes

import (
	"context"
	"encoding/pem"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testMetricsTLSNamespace = "metrics-tls-ns"
	testMetricsLeafRV       = "leaf-3"
)

func testMetricsTLSScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	return s
}

func testSecretMeta(name, namespace, rv string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: namespace, ResourceVersion: rv}
}

func mustMetricsTLSMaterial(t *testing.T) (caPEM, leafPEM []byte) {
	t.Helper()
	caPEM, leafPEM, err := NewSelfSignedMetricsTLSMaterial()
	if err != nil {
		t.Fatalf("material: %v", err)
	}
	return caPEM, leafPEM
}

func metricsTLSSecret(ns, rv string, caPEM, leafPEM []byte) *corev1.Secret {
	data := map[string][]byte{}
	if caPEM != nil {
		data[MetricsCACertKey] = caPEM
	}
	if leafPEM != nil {
		data[MetricsServerCertTLSCertKey] = leafPEM
	}
	return &corev1.Secret{
		ObjectMeta: testSecretMeta(MetricsServerCertSecretName, ns, rv),
		Data:       data,
	}
}

func TestEvaluateMetricsScrapeTLS_Ready(t *testing.T) {
	ctx := context.Background()
	caPEM, leafPEM := mustMetricsTLSMaterial(t)
	c := fake.NewClientBuilder().WithScheme(testMetricsTLSScheme(t)).WithObjects(
		metricsTLSSecret(testMetricsTLSNamespace, testMetricsLeafRV, caPEM, leafPEM),
	).Build()

	result, err := EvaluateMetricsScrapeTLS(ctx, MetricsScrapeTLSInput{Client: c, Namespace: testMetricsTLSNamespace})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !result.Ready || result.Reason != "" {
		t.Fatalf("ready=%v reason=%q", result.Ready, result.Reason)
	}
	if result.CAResourceVersion != testMetricsLeafRV || result.LeafResourceVersion != testMetricsLeafRV {
		t.Fatalf("rv ca=%q leaf=%q", result.CAResourceVersion, result.LeafResourceVersion)
	}
	if result.RenewRequested {
		t.Fatal("RenewRequested should be false")
	}
}

func TestEvaluateMetricsScrapeTLS_UsesReaderNotClient(t *testing.T) {
	ctx := context.Background()
	caPEM, leafPEM := mustMetricsTLSMaterial(t)
	reader := fake.NewClientBuilder().WithScheme(testMetricsTLSScheme(t)).WithObjects(
		metricsTLSSecret(testMetricsTLSNamespace, "rv-reader", caPEM, leafPEM),
	).Build()
	// Client has no Secret — evaluation must use Reader.
	clientOnly := fake.NewClientBuilder().WithScheme(testMetricsTLSScheme(t)).Build()

	result, err := EvaluateMetricsScrapeTLS(ctx, MetricsScrapeTLSInput{
		Client:    clientOnly,
		Reader:    reader,
		Namespace: testMetricsTLSNamespace,
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected ready via reader, reason=%q", result.Reason)
	}
}

func TestEvaluateMetricsScrapeTLS_ValidationAndMissing(t *testing.T) {
	ctx := context.Background()
	if _, err := EvaluateMetricsScrapeTLS(ctx, MetricsScrapeTLSInput{}); err == nil {
		t.Fatal("expected namespace error")
	}
	if _, err := EvaluateMetricsScrapeTLS(ctx, MetricsScrapeTLSInput{Namespace: "ns"}); err == nil {
		t.Fatal("expected reader error")
	}

	c := fake.NewClientBuilder().WithScheme(testMetricsTLSScheme(t)).Build()
	result, err := EvaluateMetricsScrapeTLS(ctx, MetricsScrapeTLSInput{Client: c, Namespace: "ns"})
	if err != nil {
		t.Fatalf("missing: %v", err)
	}
	if result.Ready || result.Reason != "metrics-server-cert-missing" {
		t.Fatalf("got ready=%v reason=%q", result.Ready, result.Reason)
	}

	caPEM, leafPEM := mustMetricsTLSMaterial(t)
	emptyCA := fake.NewClientBuilder().WithScheme(testMetricsTLSScheme(t)).WithObjects(
		metricsTLSSecret("ns", "1", nil, leafPEM),
	).Build()
	result, err = EvaluateMetricsScrapeTLS(ctx, MetricsScrapeTLSInput{Client: emptyCA, Namespace: "ns"})
	if err != nil {
		t.Fatalf("empty ca: %v", err)
	}
	if result.Ready || result.Reason != "metrics-ca-empty" {
		t.Fatalf("got ready=%v reason=%q", result.Ready, result.Reason)
	}

	emptyLeaf := fake.NewClientBuilder().WithScheme(testMetricsTLSScheme(t)).WithObjects(
		metricsTLSSecret("ns", "1", caPEM, nil),
	).Build()
	result, err = EvaluateMetricsScrapeTLS(ctx, MetricsScrapeTLSInput{Client: emptyLeaf, Namespace: "ns"})
	if err != nil {
		t.Fatalf("empty leaf: %v", err)
	}
	if result.Ready || result.Reason != "metrics-server-cert-empty" {
		t.Fatalf("got ready=%v reason=%q", result.Ready, result.Reason)
	}

	otherCA, otherLeaf := mustMetricsTLSMaterial(t)
	mismatch := fake.NewClientBuilder().WithScheme(testMetricsTLSScheme(t)).WithObjects(
		metricsTLSSecret("ns", "1", caPEM, otherLeaf),
	).Build()
	_ = otherCA
	result, err = EvaluateMetricsScrapeTLS(ctx, MetricsScrapeTLSInput{Client: mismatch, Namespace: "ns"})
	if err != nil {
		t.Fatalf("mismatch: %v", err)
	}
	if result.Ready || result.Reason != "leaf-ca-mismatch" {
		t.Fatalf("got ready=%v reason=%q", result.Ready, result.Reason)
	}
}

func TestEvaluateMetricsScrapeTLS_GetErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("get failed")
	c := fake.NewClientBuilder().WithScheme(testMetricsTLSScheme(t)).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(
			ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
		) error {
			return boom
		},
	}).Build()
	if _, err := EvaluateMetricsScrapeTLS(ctx, MetricsScrapeTLSInput{Client: c, Namespace: "ns"}); err == nil {
		t.Fatal("expected get error")
	}
}

func TestReconcileMetricsScrapeTLS_DelegatesToEvaluate(t *testing.T) {
	ctx := context.Background()
	caPEM, leafPEM := mustMetricsTLSMaterial(t)
	c := fake.NewClientBuilder().WithScheme(testMetricsTLSScheme(t)).WithObjects(
		metricsTLSSecret("ns", "9", caPEM, leafPEM),
	).Build()
	result, err := ReconcileMetricsScrapeTLS(ctx, MetricsScrapeTLSInput{Client: c, Namespace: "ns"})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !result.Ready {
		t.Fatalf("expected ready, reason=%q", result.Reason)
	}
}

func TestVerifyCertPEMSignedByCA(t *testing.T) {
	caPEM, leafPEM := mustMetricsTLSMaterial(t)
	if err := VerifyCertPEMSignedByCA(leafPEM, caPEM); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := VerifyCertPEMSignedByCA([]byte("not-pem"), caPEM); err == nil {
		t.Fatal("expected leaf parse error")
	}
	if err := VerifyCertPEMSignedByCA(leafPEM, []byte("not-pem")); err == nil {
		t.Fatal("expected ca parse error")
	}
	otherCA, _ := mustMetricsTLSMaterial(t)
	if err := VerifyCertPEMSignedByCA(leafPEM, otherCA); err == nil {
		t.Fatal("expected mismatch")
	}
	// Non-certificate PEM block then cert.
	padded := append(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("x")}), leafPEM...)
	if err := VerifyCertPEMSignedByCA(padded, caPEM); err != nil {
		t.Fatalf("skip non-cert block: %v", err)
	}
}

func TestParseFirstCertPEM_NoBlock(t *testing.T) {
	if _, err := parseFirstCertPEM(nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestIsMetricsTLSSecret_SingleSecret(t *testing.T) {
	if IsMetricsTLSSecret(nil) {
		t.Fatal("nil")
	}
	if IsMetricsTLSSecret(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: MetricsServerCertSecretName}}) {
		t.Fatal("empty namespace")
	}
	if !IsMetricsTLSSecret(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: MetricsServerCertSecretName, Namespace: "ns"},
	}) {
		t.Fatal("expected true")
	}
	if IsMetricsTLSSecret(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "ns"},
	}) {
		t.Fatal("expected false")
	}
}

// Ensure NotFound path distinction stays covered when interceptor returns typed errors.
func TestEvaluateMetricsScrapeTLS_NotFoundTyped(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(testMetricsTLSScheme(t)).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(
			ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption,
		) error {
			return apierrors.NewNotFound(corev1.Resource("secrets"), key.Name)
		},
	}).Build()
	result, err := EvaluateMetricsScrapeTLS(ctx, MetricsScrapeTLSInput{Client: c, Namespace: "ns"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if result.Reason != "metrics-server-cert-missing" {
		t.Fatalf("reason=%q", result.Reason)
	}
}
