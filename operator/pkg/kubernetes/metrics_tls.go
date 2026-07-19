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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// MetricsServerCertSecretName is the metrics TLS Secret (tls.crt/tls.key + ca.crt).
	// Operands: issued by ClusterIssuer konflux-issuer. Operator manager: issued by a
	// namespace-local SelfSigned Issuer (konflux-issuer does not exist until the operator
	// starts). Same Secret shape either way so scrape wiring stays identical.
	MetricsServerCertSecretName = "metrics-server-cert" //nolint:gosec // G101: Secret resource name, not a credential
	// MetricsCASecretName is the Secret ServiceMonitors trust via tlsConfig.ca (key ca.crt).
	// Single-Secret pattern: same name as MetricsServerCertSecretName.
	MetricsCASecretName = MetricsServerCertSecretName
	// MetricsCACertKey is the CA certificate key used by ServiceMonitor tlsConfig.ca and
	// EvaluateMetricsScrapeTLS.
	MetricsCACertKey = "ca.crt"
	// MetricsServerCertTLSCertKey is the leaf certificate key in MetricsServerCertSecretName.
	MetricsServerCertTLSCertKey = "tls.crt"
	// MetricsLeafCertificateName is the cert-manager Certificate that owns the metrics Secret.
	MetricsLeafCertificateName = "metrics-certs"

	// DefaultMetricsTLSRequeue is used while waiting for metrics-server-cert readiness.
	DefaultMetricsTLSRequeue = 15 * time.Second
)

// MetricsScrapeTLSResult is the outcome of evaluating metrics scrape TLS readiness.
//
// ResourceVersion fields come from the Secret Get used for verification — callers must not
// re-Get that Secret from a potentially stale informer cache in the same reconcile.
type MetricsScrapeTLSResult struct {
	Ready bool
	// CAResourceVersion is the metrics TLS Secret resourceVersion (same Secret as the leaf
	// under the konflux-issuer pattern). Retained for ServiceMonitor ca-sync resync nudges.
	CAResourceVersion string
	// LeafResourceVersion is the same Secret resourceVersion (alias of CAResourceVersion).
	LeafResourceVersion string
	// RenewRequested is always false; cert-manager owns leaf rotation.
	RenewRequested bool
	// Reason is a short machine-readable explanation when Ready is false.
	Reason string
}

// MetricsScrapeTLSInput configures Evaluate/Reconcile of metrics scrape TLS readiness.
type MetricsScrapeTLSInput struct {
	// Client is used when Reader is nil.
	Client client.Client
	// Reader loads the metrics Secret. Prefer an uncached API reader so cert-manager
	// updates are not missed due to informer lag. When nil, Client is used.
	Reader    client.Reader
	Namespace string
}

func metricsTLSReader(in MetricsScrapeTLSInput) client.Reader {
	if in.Reader != nil {
		return in.Reader
	}
	return in.Client
}

// EvaluateMetricsScrapeTLS reports whether metrics-server-cert has non-empty tls.crt and
// ca.crt and the leaf verifies against that CA. It only reads Secrets and never writes.
func EvaluateMetricsScrapeTLS(ctx context.Context, in MetricsScrapeTLSInput) (MetricsScrapeTLSResult, error) {
	if in.Namespace == "" {
		return MetricsScrapeTLSResult{}, fmt.Errorf("namespace is required")
	}
	reader := metricsTLSReader(in)
	if reader == nil {
		return MetricsScrapeTLSResult{}, fmt.Errorf("secret reader is required")
	}

	secret := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{
		Namespace: in.Namespace,
		Name:      MetricsServerCertSecretName,
	}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return MetricsScrapeTLSResult{Reason: "metrics-server-cert-missing"}, nil
		}
		return MetricsScrapeTLSResult{}, fmt.Errorf("get metrics TLS secret: %w", err)
	}

	rv := secret.ResourceVersion
	caPEM := secret.Data[MetricsCACertKey]
	if len(caPEM) == 0 {
		return MetricsScrapeTLSResult{
			CAResourceVersion:   rv,
			LeafResourceVersion: rv,
			Reason:              "metrics-ca-empty",
		}, nil
	}
	leafPEM := secret.Data[MetricsServerCertTLSCertKey]
	if len(leafPEM) == 0 {
		return MetricsScrapeTLSResult{
			CAResourceVersion:   rv,
			LeafResourceVersion: rv,
			Reason:              "metrics-server-cert-empty",
		}, nil
	}

	if err := VerifyCertPEMSignedByCA(leafPEM, caPEM); err != nil {
		return MetricsScrapeTLSResult{
			CAResourceVersion:   rv,
			LeafResourceVersion: rv,
			Reason:              "leaf-ca-mismatch",
		}, nil
	}

	return MetricsScrapeTLSResult{
		Ready:               true,
		CAResourceVersion:   rv,
		LeafResourceVersion: rv,
	}, nil
}

// ReconcileMetricsScrapeTLS evaluates metrics TLS readiness. With the single-Secret
// pattern there is no operator-driven leaf renew — cert-manager keeps tls.crt and
// ca.crt consistent — so this is EvaluateMetricsScrapeTLS.
func ReconcileMetricsScrapeTLS(ctx context.Context, in MetricsScrapeTLSInput) (MetricsScrapeTLSResult, error) {
	return EvaluateMetricsScrapeTLS(ctx, in)
}

// VerifyCertPEMSignedByCA parses PEM certificate material and verifies leaf against caPEM.
func VerifyCertPEMSignedByCA(leafPEM, caPEM []byte) error {
	leafCert, err := parseFirstCertPEM(leafPEM)
	if err != nil {
		return fmt.Errorf("parse leaf certificate: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("parse CA certificate PEM")
	}
	if _, err := leafCert.Verify(x509.VerifyOptions{Roots: roots}); err != nil {
		return err
	}
	return nil
}

func parseFirstCertPEM(pemBytes []byte) (*x509.Certificate, error) {
	rest := pemBytes
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, fmt.Errorf("no CERTIFICATE PEM block")
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		return x509.ParseCertificate(block.Bytes)
	}
}
