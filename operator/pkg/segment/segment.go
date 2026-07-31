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

package segment

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolveWriteKey determines the effective Segment write key.
// Precedence:
//  1. crKey -- the inline spec.segmentKey value, taken as an explicit override
//     (e.g. for local/self-deployed Konflux instances).
//  2. secretKey -- the value resolved from spec.segmentKeySecretRef (e.g. a
//     Vault-backed secret in staging/production environments).
//  3. Empty, if neither is set.
//
// Returns the key and its source ("cr", "secret", or "" if unresolved).
func ResolveWriteKey(crKey, secretKey string) (key, source string) {
	if crKey != "" {
		return crKey, "cr"
	}
	if secretKey != "" {
		return secretKey, "secret"
	}
	return "", ""
}

// ResolveWriteKeySecretRef looks up the Segment write key from the Secret
// referenced by ref, if set. The Secret is expected to live in namespace
// (typically the segment-bridge namespace, shared by the segment-bridge
// CronJob's config Secret and the konflux-ui reverse proxy's Segment
// config Secret). Returns an empty string (no error) when ref is nil, or
// when ref.Optional is true and the Secret or key is missing.
//
// Callers should only invoke this when the inline write key (spec.segmentKey)
// is unset -- segmentKeySecretRef is ignored whenever segmentKey is set, so
// skipping resolution in that case avoids failing reconciliation over an
// irrelevant/unresolvable ref.
func ResolveWriteKeySecretRef(
	ctx context.Context,
	c client.Client,
	namespace string,
	ref *corev1.SecretKeySelector,
) (string, error) {
	if ref == nil {
		return "", nil
	}

	optional := ref.Optional != nil && *ref.Optional

	secret := &corev1.Secret{}
	nn := client.ObjectKey{Name: ref.Name, Namespace: namespace}
	if err := c.Get(ctx, nn, secret); err != nil {
		if apierrors.IsNotFound(err) && optional {
			return "", nil
		}
		return "", fmt.Errorf("failed to get Secret %s/%s referenced by segmentKeySecretRef: %w", nn.Namespace, nn.Name, err)
	}

	value, ok := secret.Data[ref.Key]
	if !ok {
		if optional {
			return "", nil
		}
		return "", fmt.Errorf(
			"key %q not found in Secret %s/%s referenced by segmentKeySecretRef",
			ref.Key, nn.Namespace, nn.Name,
		)
	}
	return string(value), nil
}

// LogWriteKeyResolution logs how the Segment write key was resolved.
// Returns true if a key is available, false if no key was configured.
func LogWriteKeyResolution(log logr.Logger, key, source string) bool {
	if key == "" {
		log.Info("No Segment write key configured in CR")
		return false
	}
	log.Info("Resolved Segment write key", "source", source)
	return true
}
