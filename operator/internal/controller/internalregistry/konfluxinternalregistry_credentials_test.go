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

package internalregistry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/bcrypt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

var _ = Describe("Internal registry credential helpers", func() {
	It("builds a valid docker config JSON auth entry", func() {
		const registry = "registry-service.kind-registry"
		const user = "konflux"
		const pass = "test-password-123"

		s, err := buildDockerConfigJSON(registry, user, pass)
		Expect(err).NotTo(HaveOccurred())

		var parsed struct {
			Auths map[string]struct {
				Auth string `json:"auth"`
			} `json:"auths"`
		}
		Expect(json.Unmarshal([]byte(s), &parsed)).To(Succeed())
		entry, ok := parsed.Auths[registry]
		Expect(ok).To(BeTrue())
		decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(decoded)).To(Equal(user + ":" + pass))
	})

	It("generates URL-safe random secrets", func() {
		const n = 32
		s, err := generateRandomSecret(n)
		Expect(err).NotTo(HaveOccurred())
		// RawURLEncoding: ceil(32 * 8 / 6) = 43 chars without padding.
		Expect(len(s)).To(BeNumerically(">=", 40))
		Expect(strings.ContainsAny(s, "+/=")).To(BeFalse())
	})

	It("does not regenerate credential data when both secrets already exist", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		existingHtpasswd := []byte("konflux:$2y$05$already.present.hash\n")
		existingDocker := []byte(`{"auths":{"registry-service.kind-registry":{"auth":"a29uZmx1eDpwcmVzZXQ="}}}`)

		owner := &konfluxv1alpha1.KonfluxInternalRegistry{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
				UID:  types.UID("test-owner-uid"),
			},
		}
		htpasswd := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      HtpasswdSecretName,
				Namespace: RegistryNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"htpasswd": append([]byte(nil), existingHtpasswd...),
			},
		}
		docker := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ClientCredentialsSecretName,
				Namespace: RegistryNamespace,
			},
			Type: corev1.SecretTypeDockerConfigJson,
			Data: map[string][]byte{
				corev1.DockerConfigJsonKey: append([]byte(nil), existingDocker...),
			},
		}

		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, htpasswd, docker).Build()
		tc := tracking.NewClientWithOwnership(cl, tracking.OwnershipConfig{
			Owner:             owner,
			OwnerLabelKey:     constant.KonfluxOwnerLabel,
			ComponentLabelKey: constant.KonfluxComponentLabel,
			Component:         string(manifests.Registry),
			FieldManager:      FieldManager,
		})

		r := &KonfluxInternalRegistryReconciler{Client: cl, Scheme: scheme}
		Expect(r.ensureRegistryCredentials(ctx, tc)).To(Succeed())

		gotHtpasswd := &corev1.Secret{}
		Expect(cl.Get(ctx, types.NamespacedName{Name: HtpasswdSecretName, Namespace: RegistryNamespace}, gotHtpasswd)).To(Succeed())
		Expect(gotHtpasswd.Data["htpasswd"]).To(Equal(existingHtpasswd))

		gotDocker := &corev1.Secret{}
		Expect(cl.Get(ctx, types.NamespacedName{Name: ClientCredentialsSecretName, Namespace: RegistryNamespace}, gotDocker)).To(Succeed())
		Expect(gotDocker.Data[corev1.DockerConfigJsonKey]).To(Equal(existingDocker))
	})

	It("rotates both credentials when one secret is missing", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		existingHtpasswd := []byte("konflux:$2y$05$already.present.hash\n")

		owner := &konfluxv1alpha1.KonfluxInternalRegistry{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
				UID:  types.UID("test-owner-uid"),
			},
		}
		htpasswd := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      HtpasswdSecretName,
				Namespace: RegistryNamespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"htpasswd": append([]byte(nil), existingHtpasswd...),
			},
		}

		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner, htpasswd).Build()
		tc := tracking.NewClientWithOwnership(cl, tracking.OwnershipConfig{
			Owner:             owner,
			OwnerLabelKey:     constant.KonfluxOwnerLabel,
			ComponentLabelKey: constant.KonfluxComponentLabel,
			Component:         string(manifests.Registry),
			FieldManager:      FieldManager,
		})

		r := &KonfluxInternalRegistryReconciler{Client: cl, Scheme: scheme}
		Expect(r.ensureRegistryCredentials(ctx, tc)).To(Succeed())

		gotHtpasswd := &corev1.Secret{}
		Expect(cl.Get(ctx, types.NamespacedName{Name: HtpasswdSecretName, Namespace: RegistryNamespace}, gotHtpasswd)).To(Succeed())
		Expect(gotHtpasswd.Data["htpasswd"]).NotTo(Equal(existingHtpasswd))

		gotDocker := &corev1.Secret{}
		Expect(cl.Get(ctx, types.NamespacedName{Name: ClientCredentialsSecretName, Namespace: RegistryNamespace}, gotDocker)).To(Succeed())
		Expect(gotDocker.Type).To(Equal(corev1.SecretTypeDockerConfigJson))
		dockerPassword, err := dockerConfigPassword(gotDocker, RegistryServiceHost, RegistryAuthUsername)
		Expect(err).NotTo(HaveOccurred())
		Expect(htpasswdMatchesPassword(gotHtpasswd, dockerPassword)).To(BeTrue())
	})

	It("returns an error when reading the htpasswd secret fails with a non-NotFound error", func() {
		ctx := context.Background()
		scheme := runtime.NewScheme()
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())

		owner := &konfluxv1alpha1.KonfluxInternalRegistry{
			ObjectMeta: metav1.ObjectMeta{
				Name: CRName,
				UID:  types.UID("test-owner-uid"),
			},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(owner).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(cctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if key.Namespace == RegistryNamespace && key.Name == HtpasswdSecretName {
					return apierrors.NewTimeoutError("apiserver", 0)
				}
				return c.Get(cctx, key, obj, opts...)
			},
		}).Build()
		tc := tracking.NewClientWithOwnership(cl, tracking.OwnershipConfig{
			Owner:             owner,
			OwnerLabelKey:     constant.KonfluxOwnerLabel,
			ComponentLabelKey: constant.KonfluxComponentLabel,
			Component:         string(manifests.Registry),
			FieldManager:      FieldManager,
		})
		r := &KonfluxInternalRegistryReconciler{Client: cl, Scheme: scheme}

		err := r.ensureRegistryCredentials(ctx, tc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(HtpasswdSecretName))
	})

	It("detects whether a secret key has data", func() {
		Expect(hasSecretData(nil, "key")).To(BeFalse())

		empty := &corev1.Secret{}
		Expect(hasSecretData(empty, "key")).To(BeFalse())

		withData := &corev1.Secret{Data: map[string][]byte{"key": []byte("value")}}
		Expect(hasSecretData(withData, "key")).To(BeTrue())

		withStringData := &corev1.Secret{StringData: map[string]string{"key": "value"}}
		Expect(hasSecretData(withStringData, "key")).To(BeTrue())
	})
})

func dockerConfigPassword(secret *corev1.Secret, registry, expectedUser string) (string, error) {
	raw := secret.Data[corev1.DockerConfigJsonKey]
	if len(raw) == 0 {
		return "", fmt.Errorf("missing %s data", corev1.DockerConfigJsonKey)
	}
	var parsed struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	entry, ok := parsed.Auths[registry]
	if !ok {
		return "", fmt.Errorf("missing auth entry for %s", registry)
	}
	decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid docker auth entry")
	}
	if parts[0] != expectedUser {
		return "", fmt.Errorf("unexpected username %q", parts[0])
	}
	return parts[1], nil
}

func htpasswdMatchesPassword(secret *corev1.Secret, password string) bool {
	raw := string(secret.Data["htpasswd"])
	line := strings.TrimSpace(raw)
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(parts[1]), []byte(password)) == nil
}
