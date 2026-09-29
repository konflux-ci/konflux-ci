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

package common

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/contenthash"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

type mockDiscoveryClient struct {
	resources map[string]*metav1.APIResourceList
}

func (m *mockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if r, ok := m.resources[groupVersion]; ok {
		return r, nil
	}
	return &metav1.APIResourceList{}, nil
}

func (m *mockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return &version.Info{GitVersion: "v1.29.0"}, nil
}

func openShiftClusterInfo(t *testing.T) *clusterinfo.Info {
	t.Helper()
	info, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			"config.openshift.io/v1": {
				APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to create OpenShift clusterinfo: %v", err)
	}
	return info
}

func newTrackingClient(t *testing.T, objs ...client.Object) (client.Client, *tracking.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add client-go scheme: %v", err)
	}
	owner := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-owner",
			Namespace: "test-namespace",
			UID:       "test-owner-uid",
		},
	}
	allObjs := append([]client.Object{owner}, objs...)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(allObjs...).
		Build()
	tc := tracking.NewClientWithOwnership(fakeClient, tracking.OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     "test.example.com/owner",
		ComponentLabelKey: "test.example.com/component",
		Component:         "test-component",
		FieldManager:      "test-manager",
	})
	return fakeClient, tc
}

func TestEnsureTrustedCAConfigMap(t *testing.T) {
	t.Run("creates ConfigMap with injection label", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		fakeClient, tc := newTrackingClient(t, ns)

		err := EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, openShiftClusterInfo(t))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cm := &corev1.ConfigMap{}
		if err := fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      TrustedCAConfigMapName,
			Namespace: "test-namespace",
		}, cm); err != nil {
			t.Fatalf("failed to get ConfigMap: %v", err)
		}

		if cm.Name != TrustedCAConfigMapName {
			t.Errorf("expected name %q, got %q", TrustedCAConfigMapName, cm.Name)
		}
		if cm.Namespace != "test-namespace" {
			t.Errorf("expected namespace %q, got %q", "test-namespace", cm.Namespace)
		}
		val, ok := cm.Labels[OpenShiftInjectTrustedCABundleLabel]
		if !ok {
			t.Fatal("expected injection label to be present")
		}
		if val != "true" {
			t.Errorf("expected injection label value %q, got %q", "true", val)
		}

		ownerVal, ok := cm.Labels["test.example.com/owner"]
		if !ok {
			t.Fatal("expected ownership label to be present")
		}
		if ownerVal != "test-owner" {
			t.Errorf("expected owner label value %q, got %q", "test-owner", ownerVal)
		}

		componentVal, ok := cm.Labels["test.example.com/component"]
		if !ok {
			t.Fatal("expected component label to be present")
		}
		if componentVal != "test-component" {
			t.Errorf("expected component label value %q, got %q", "test-component", componentVal)
		}
	})

	t.Run("is idempotent", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		existingCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      TrustedCAConfigMapName,
				Namespace: "test-namespace",
				Labels: map[string]string{
					OpenShiftInjectTrustedCABundleLabel: "true",
				},
			},
		}
		fakeClient, tc := newTrackingClient(t, ns, existingCM)

		err := EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, openShiftClusterInfo(t))
		if err != nil {
			t.Fatalf("unexpected error on second apply: %v", err)
		}

		cm := &corev1.ConfigMap{}
		if err := fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      TrustedCAConfigMapName,
			Namespace: "test-namespace",
		}, cm); err != nil {
			t.Fatalf("failed to get ConfigMap: %v", err)
		}

		val, ok := cm.Labels[OpenShiftInjectTrustedCABundleLabel]
		if !ok {
			t.Fatal("expected injection label to be present after re-apply")
		}
		if val != "true" {
			t.Errorf("expected injection label value %q, got %q", "true", val)
		}
	})

	t.Run("is a no-op when clusterInfo is nil", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		fakeClient, tc := newTrackingClient(t, ns)

		err := EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cm := &corev1.ConfigMap{}
		getErr := fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      TrustedCAConfigMapName,
			Namespace: "test-namespace",
		}, cm)
		if getErr == nil {
			t.Fatal("expected ConfigMap to not be created when clusterInfo is nil")
		}
	})

	t.Run("is a no-op on non-OpenShift clusters", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		fakeClient, tc := newTrackingClient(t, ns)

		nonOpenShiftInfo, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{
			resources: map[string]*metav1.APIResourceList{},
		})
		if err != nil {
			t.Fatalf("failed to create non-OpenShift clusterinfo: %v", err)
		}

		err = EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, nonOpenShiftInfo)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		cm := &corev1.ConfigMap{}
		getErr := fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      TrustedCAConfigMapName,
			Namespace: "test-namespace",
		}, cm)
		if getErr == nil {
			t.Fatal("expected ConfigMap to not be created on non-OpenShift")
		}
	})

	t.Run("does not claim data field so CNO-injected content survives re-apply", func(t *testing.T) {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		existingCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      TrustedCAConfigMapName,
				Namespace: "test-namespace",
				Labels: map[string]string{
					OpenShiftInjectTrustedCABundleLabel: "true",
				},
			},
			Data: map[string]string{
				"ca-bundle.crt": "-----BEGIN CERTIFICATE-----\nMIIBkTCB...\n-----END CERTIFICATE-----\n",
			},
		}
		fakeClient, tc := newTrackingClient(t, ns, existingCM)

		err := EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, openShiftClusterInfo(t))
		if err != nil {
			t.Fatalf("unexpected error on re-apply: %v", err)
		}

		cm := &corev1.ConfigMap{}
		if err := fakeClient.Get(context.Background(), types.NamespacedName{
			Name:      TrustedCAConfigMapName,
			Namespace: "test-namespace",
		}, cm); err != nil {
			t.Fatalf("failed to get ConfigMap: %v", err)
		}

		caBundle, ok := cm.Data["ca-bundle.crt"]
		if !ok {
			t.Fatal("expected ca-bundle.crt data key to survive re-apply")
		}
		if caBundle == "" {
			t.Error("expected ca-bundle.crt to retain its value")
		}
	})

	t.Run("returns error when apply fails", func(t *testing.T) {
		scheme := runtime.NewScheme()
		if err := clientgoscheme.AddToScheme(scheme); err != nil {
			t.Fatalf("failed to add client-go scheme: %v", err)
		}
		owner := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-owner",
				Namespace: "test-namespace",
				UID:       "test-owner-uid",
			},
		}
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"},
		}
		fakeClient := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(owner, ns).
			WithInterceptorFuncs(interceptor.Funcs{
				Patch: func(_ context.Context, _ client.WithWatch, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					return fmt.Errorf("simulated API server failure")
				},
			}).
			Build()
		tc := tracking.NewClientWithOwnership(fakeClient, tracking.OwnershipConfig{
			Owner:             owner,
			OwnerLabelKey:     "test.example.com/owner",
			ComponentLabelKey: "test.example.com/component",
			Component:         "test-component",
			FieldManager:      "test-manager",
		})

		err := EnsureTrustedCAConfigMap(context.Background(), "test-namespace", tc, openShiftClusterInfo(t))
		if err == nil {
			t.Fatal("expected error when apply fails")
		}
		if !strings.Contains(err.Error(), "failed to apply trusted-ca ConfigMap in test-namespace") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func testTrustedCATarget() TrustedCATarget {
	return TrustedCATarget{
		Namespace:      "operand",
		DeploymentName: "controller-manager",
		ContainerName:  "manager",
		CRName:         "konflux-component",
	}
}

func trustedCADeployment() *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "controller-manager"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "manager",
						VolumeMounts: []corev1.VolumeMount{{
							Name:      TrustedCAVolumeName,
							MountPath: TrustedCADefaultFileMountPath,
							SubPath:   TrustedCADefaultFileVolumePath,
							ReadOnly:  true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: TrustedCAVolumeName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: TrustedCAConfigMapName},
								Items: []corev1.KeyToPath{{
									Key:  TrustedCADefaultFileVolumePath,
									Path: TrustedCADefaultFileVolumePath,
								}},
								Optional: ptr.To(true),
							},
						},
					}},
				},
			},
		},
	}
}

func findTestTrustedCAVolume(deployment *appsv1.Deployment) *corev1.Volume {
	return kubernetes.FindVolume(deployment.Spec.Template.Spec.Volumes, TrustedCAVolumeName)
}

func findTestTrustedCAMount(deployment *appsv1.Deployment, containerName string) *corev1.VolumeMount {
	manager := kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, containerName)
	if manager == nil {
		return nil
	}
	return kubernetes.FindVolumeMount(manager.VolumeMounts, TrustedCAVolumeName)
}

func TestApplyTrustedCAMount(t *testing.T) {
	const containerName = "manager"

	t.Run("omitted trustedCA leaves the extra-file mount unchanged", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := trustedCADeployment()
		err := ApplyTrustedCAMount(deployment, nil, "", containerName)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		mount := findTestTrustedCAMount(deployment, containerName)
		g.Expect(mount).NotTo(gomega.BeNil())
		g.Expect(mount.MountPath).To(gomega.Equal(TrustedCADefaultFileMountPath))
		g.Expect(mount.SubPath).To(gomega.Equal(TrustedCADefaultFileVolumePath))

		vol := findTestTrustedCAVolume(deployment)
		g.Expect(vol).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap.Name).To(gomega.Equal(TrustedCAConfigMapName))
		g.Expect(vol.ConfigMap.Optional).NotTo(gomega.BeNil())
		g.Expect(*vol.ConfigMap.Optional).To(gomega.BeTrue())
		g.Expect(vol.ConfigMap.Items).To(gomega.ConsistOf(corev1.KeyToPath{
			Key:  TrustedCADefaultFileVolumePath,
			Path: TrustedCADefaultFileVolumePath,
		}))
	})

	t.Run("trustedCA retargets the volume ConfigMap and keeps the extra-file mount", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.TrustedCAConfigMap{Name: "custom-ca-bundle", Key: "tls.pem"}
		deployment := trustedCADeployment()
		err := ApplyTrustedCAMount(deployment, spec, "", containerName)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		mount := findTestTrustedCAMount(deployment, containerName)
		g.Expect(mount).NotTo(gomega.BeNil())
		g.Expect(mount.MountPath).To(gomega.Equal(TrustedCADefaultFileMountPath))
		g.Expect(mount.SubPath).To(gomega.Equal(TrustedCADefaultFileVolumePath))
		g.Expect(mount.ReadOnly).To(gomega.BeTrue())

		vol := findTestTrustedCAVolume(deployment)
		g.Expect(vol).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap.Name).To(gomega.Equal(spec.Name))
		g.Expect(vol.ConfigMap.Optional).NotTo(gomega.BeNil())
		g.Expect(*vol.ConfigMap.Optional).To(gomega.BeFalse())
		g.Expect(vol.ConfigMap.Items).To(gomega.ConsistOf(corev1.KeyToPath{
			Key:  spec.Key,
			Path: TrustedCADefaultFileVolumePath,
		}))
		g.Expect(deployment.Spec.Template.Annotations).NotTo(gomega.HaveKey(TrustedCAHashAnnotation))
	})

	t.Run("trustedCA stamps a content-hash annotation on the pod template", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.TrustedCAConfigMap{Name: "custom-ca-bundle", Key: "tls.pem"}
		hash := contenthash.String("-----BEGIN CERTIFICATE-----\nnew\n-----END CERTIFICATE-----\n")
		deployment := trustedCADeployment()
		err := ApplyTrustedCAMount(deployment, spec, hash, containerName)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(deployment.Spec.Template.Annotations).To(gomega.HaveKeyWithValue(TrustedCAHashAnnotation, hash))
	})

	t.Run("empty content hash omits the annotation on a never-stamped apply", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.TrustedCAConfigMap{Name: "custom-ca-bundle", Key: "tls.pem"}
		deployment := trustedCADeployment()
		deployment.Spec.Template.Annotations = map[string]string{TrustedCAHashAnnotation: "stale-hash"}
		err := ApplyTrustedCAMount(deployment, spec, "", containerName)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(deployment.Spec.Template.Annotations).NotTo(gomega.HaveKey(TrustedCAHashAnnotation))
	})

	t.Run("omitted trustedCA removes a leftover content-hash annotation", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := trustedCADeployment()
		deployment.Spec.Template.Annotations = map[string]string{TrustedCAHashAnnotation: "stale-hash"}
		err := ApplyTrustedCAMount(deployment, nil, "", containerName)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(deployment.Spec.Template.Annotations).NotTo(gomega.HaveKey(TrustedCAHashAnnotation))
	})

	t.Run("omitted trustedCA stamps a platform content hash without retargeting the volume", func(t *testing.T) {
		g := gomega.NewWithT(t)
		hash := contenthash.String("-----BEGIN CERTIFICATE-----\nplatform\n-----END CERTIFICATE-----\n")
		deployment := trustedCADeployment()
		err := ApplyTrustedCAMount(deployment, nil, hash, containerName)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		mount := findTestTrustedCAMount(deployment, containerName)
		g.Expect(mount).NotTo(gomega.BeNil())
		g.Expect(mount.MountPath).To(gomega.Equal(TrustedCADefaultFileMountPath))
		g.Expect(mount.SubPath).To(gomega.Equal(TrustedCADefaultFileVolumePath))

		vol := findTestTrustedCAVolume(deployment)
		g.Expect(vol).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap.Name).To(gomega.Equal(TrustedCAConfigMapName))
		g.Expect(vol.ConfigMap.Optional).NotTo(gomega.BeNil())
		g.Expect(*vol.ConfigMap.Optional).To(gomega.BeTrue())
		g.Expect(vol.ConfigMap.Items).To(gomega.ConsistOf(corev1.KeyToPath{
			Key:  TrustedCADefaultFileVolumePath,
			Path: TrustedCADefaultFileVolumePath,
		}))
		g.Expect(deployment.Spec.Template.Annotations).To(gomega.HaveKeyWithValue(TrustedCAHashAnnotation, hash))
	})

	t.Run("trustedCA retargets even when ConfigMap name is the default", func(t *testing.T) {
		g := gomega.NewWithT(t)
		spec := &konfluxv1alpha1.TrustedCAConfigMap{Name: TrustedCAConfigMapName, Key: "ca-bundle.crt"}
		deployment := trustedCADeployment()
		err := ApplyTrustedCAMount(deployment, spec, "", containerName)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		vol := findTestTrustedCAVolume(deployment)
		g.Expect(vol).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap).NotTo(gomega.BeNil())
		g.Expect(vol.ConfigMap.Name).To(gomega.Equal(spec.Name))
		g.Expect(vol.ConfigMap.Optional).NotTo(gomega.BeNil())
		g.Expect(*vol.ConfigMap.Optional).To(gomega.BeFalse())
		g.Expect(vol.ConfigMap.Items).To(gomega.ConsistOf(corev1.KeyToPath{
			Key:  spec.Key,
			Path: TrustedCADefaultFileVolumePath,
		}))
	})

	t.Run("accepts key starting with special characters", func(t *testing.T) {
		g := gomega.NewWithT(t)
		for _, key := range []string{"-ca.pem", "_ca.pem", ".ca-bundle"} {
			spec := &konfluxv1alpha1.TrustedCAConfigMap{Name: "custom-ca-bundle", Key: key}
			deployment := trustedCADeployment()
			err := ApplyTrustedCAMount(deployment, spec, "", containerName)
			g.Expect(err).NotTo(gomega.HaveOccurred(), "key %q should be accepted", key)

			vol := findTestTrustedCAVolume(deployment)
			g.Expect(vol).NotTo(gomega.BeNil())
			g.Expect(vol.ConfigMap).NotTo(gomega.BeNil())
			g.Expect(vol.ConfigMap.Items).To(gomega.ConsistOf(corev1.KeyToPath{
				Key:  key,
				Path: TrustedCADefaultFileVolumePath,
			}))
		}
	})

	t.Run("returns error when trusted-ca volume is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "controller-manager"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: containerName}},
					},
				},
			},
		}
		err := ApplyTrustedCAMount(deployment, &konfluxv1alpha1.TrustedCAConfigMap{
			Name: "custom-ca-bundle", Key: "tls.pem",
		}, "", containerName)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("trusted-ca ConfigMap volume not found"))
	})

	t.Run("returns error when trusted-ca volume is not a ConfigMap", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "controller-manager"},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{{
							Name: TrustedCAVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						}},
						Containers: []corev1.Container{{Name: containerName}},
					},
				},
			},
		}
		err := ApplyTrustedCAMount(deployment, &konfluxv1alpha1.TrustedCAConfigMap{
			Name: "custom-ca-bundle", Key: "tls.pem",
		}, "", containerName)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("trusted-ca ConfigMap volume not found"))
	})

	t.Run("returns error when manager container is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := trustedCADeployment()
		deployment.Spec.Template.Spec.Containers[0].Name = "other-container"
		err := ApplyTrustedCAMount(deployment, &konfluxv1alpha1.TrustedCAConfigMap{
			Name: "custom-ca-bundle", Key: "tls.pem",
		}, "", containerName)
		g.Expect(err).To(gomega.MatchError(gomega.ContainSubstring(`container "manager" not found`)))
	})

	t.Run("returns error when trusted-ca volume mount is missing from container", func(t *testing.T) {
		g := gomega.NewWithT(t)
		deployment := trustedCADeployment()
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{{
			Name: "other-mount", MountPath: "/tmp",
		}}
		err := ApplyTrustedCAMount(deployment, &konfluxv1alpha1.TrustedCAConfigMap{
			Name: "custom-ca-bundle", Key: "tls.pem",
		}, "", containerName)
		g.Expect(err).To(gomega.HaveOccurred())
		g.Expect(err.Error()).To(gomega.ContainSubstring("trusted-ca volume mount not found"))
	})
}

func TestTrustedCABundleHash(t *testing.T) {
	const pem = "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"

	t.Run("hashes Data for the requested key", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{Data: map[string]string{"tls.pem": pem}}
		g.Expect(TrustedCABundleHash(cm, "tls.pem")).To(gomega.Equal(contenthash.String(pem)))
	})

	t.Run("hashes BinaryData when Data is absent", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{BinaryData: map[string][]byte{"tls.pem": []byte(pem)}}
		g.Expect(TrustedCABundleHash(cm, "tls.pem")).To(gomega.Equal(contenthash.String(pem)))
	})

	t.Run("prefers Data over BinaryData", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{
			Data:       map[string]string{"tls.pem": pem},
			BinaryData: map[string][]byte{"tls.pem": []byte("other")},
		}
		g.Expect(TrustedCABundleHash(cm, "tls.pem")).To(gomega.Equal(contenthash.String(pem)))
	})

	t.Run("returns empty when the key is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{Data: map[string]string{"other": pem}}
		g.Expect(TrustedCABundleHash(cm, "tls.pem")).To(gomega.BeEmpty())
	})
}

func TestLookupTrustedCAHash(t *testing.T) {
	scheme := runtime.NewScheme()
	g := gomega.NewWithT(t)
	g.Expect(clientgoscheme.AddToScheme(scheme)).To(gomega.Succeed())

	target := testTrustedCATarget()
	spec := &konfluxv1alpha1.TrustedCAConfigMap{Name: "custom-ca-bundle", Key: "tls.pem"}
	const pem = "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"

	t.Run("returns empty when spec is nil", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		hash, err := LookupTrustedCAHash(context.Background(), c, target.Namespace, nil)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.BeEmpty())
	})

	t.Run("returns empty when the ConfigMap is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		hash, err := LookupTrustedCAHash(context.Background(), c, target.Namespace, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.BeEmpty())
	})

	t.Run("returns empty when the key is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: target.Namespace},
			Data:       map[string]string{"other": pem},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
		hash, err := LookupTrustedCAHash(context.Background(), c, target.Namespace, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.BeEmpty())
	})

	t.Run("hashes the referenced key", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: target.Namespace},
			Data:       map[string]string{spec.Key: pem},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm).Build()
		hash, err := LookupTrustedCAHash(context.Background(), c, target.Namespace, spec)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.Equal(contenthash.String(pem)))
	})

	t.Run("returns the Get error when it is not NotFound", func(t *testing.T) {
		g := gomega.NewWithT(t)
		forbidden := errors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, spec.Name, fmt.Errorf("denied"))
		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
				return forbidden
			},
		}).Build()
		hash, err := LookupTrustedCAHash(context.Background(), c, target.Namespace, spec)
		g.Expect(err).To(gomega.Equal(forbidden))
		g.Expect(hash).To(gomega.BeEmpty())
	})
}

func defaultClusterInfo(t *testing.T) *clusterinfo.Info {
	t.Helper()
	info, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{},
	})
	if err != nil {
		t.Fatalf("failed to create default clusterinfo: %v", err)
	}
	return info
}

func TestTrustedCAHashForApply(t *testing.T) {
	scheme := runtime.NewScheme()
	g := gomega.NewWithT(t)
	g.Expect(clientgoscheme.AddToScheme(scheme)).To(gomega.Succeed())

	target := testTrustedCATarget()
	spec := &konfluxv1alpha1.TrustedCAConfigMap{Name: "custom-ca-bundle", Key: "tls.pem"}
	const pem = "-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n"
	const liveHash = "live-hash"

	liveDeployment := func(hash string) *appsv1.Deployment {
		annotations := map[string]string{}
		if hash != "" {
			annotations[TrustedCAHashAnnotation] = hash
		}
		return &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      target.DeploymentName,
				Namespace: target.Namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Annotations: annotations},
				},
			},
		}
	}

	t.Run("returns empty when spec is nil even if the live annotation exists", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(liveDeployment(liveHash)).Build()
		hash, err := TrustedCAHashForApply(context.Background(), c, nil, nil, target)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.BeEmpty())
	})

	t.Run("hashes the platform bundle when spec is nil on OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: TrustedCAConfigMapName, Namespace: target.Namespace},
			Data:       map[string]string{TrustedCADefaultFileVolumePath: pem},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, liveDeployment(liveHash)).Build()
		hash, err := TrustedCAHashForApply(context.Background(), c, openShiftClusterInfo(t), nil, target)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.Equal(contenthash.String(pem)))
	})

	t.Run("returns empty when the platform bundle key is missing on OpenShift", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: TrustedCAConfigMapName, Namespace: target.Namespace},
			Data:       map[string]string{"other": pem},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, liveDeployment(liveHash)).Build()
		hash, err := TrustedCAHashForApply(context.Background(), c, openShiftClusterInfo(t), nil, target)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.BeEmpty())
	})

	t.Run("returns empty when spec is nil on a non-OpenShift cluster", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: TrustedCAConfigMapName, Namespace: target.Namespace},
			Data:       map[string]string{TrustedCADefaultFileVolumePath: pem},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, liveDeployment(liveHash)).Build()
		hash, err := TrustedCAHashForApply(context.Background(), c, defaultClusterInfo(t), nil, target)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.BeEmpty())
	})

	t.Run("hashes the referenced key when the ConfigMap exists", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: target.Namespace},
			Data:       map[string]string{spec.Key: pem},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, liveDeployment("stale")).Build()
		hash, err := TrustedCAHashForApply(context.Background(), c, nil, spec, target)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.Equal(contenthash.String(pem)))
	})

	t.Run("returns empty when the ConfigMap and Deployment are missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		hash, err := TrustedCAHashForApply(context.Background(), c, nil, spec, target)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.BeEmpty())
	})

	t.Run("preserves the live hash when the ConfigMap is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(liveDeployment(liveHash)).Build()
		hash, err := TrustedCAHashForApply(context.Background(), c, nil, spec, target)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.Equal(liveHash))
	})

	t.Run("preserves the live hash when the key is missing", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: spec.Name, Namespace: target.Namespace},
			Data:       map[string]string{"other": pem},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cm, liveDeployment(liveHash)).Build()
		hash, err := TrustedCAHashForApply(context.Background(), c, nil, spec, target)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(hash).To(gomega.Equal(liveHash))
	})

	t.Run("returns the Deployment Get error when it is not NotFound", func(t *testing.T) {
		g := gomega.NewWithT(t)
		forbidden := errors.NewForbidden(schema.GroupResource{Resource: "deployments"}, target.DeploymentName, fmt.Errorf("denied"))
		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
				if _, ok := obj.(*appsv1.Deployment); ok {
					return forbidden
				}
				return errors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, spec.Name)
			},
		}).Build()
		hash, err := TrustedCAHashForApply(context.Background(), c, nil, spec, target)
		g.Expect(err).To(gomega.Equal(forbidden))
		g.Expect(hash).To(gomega.BeEmpty())
	})
}

func TestMapTrustedCAConfigMap(t *testing.T) {
	target := testTrustedCATarget()
	want := []reconcile.Request{{NamespacedName: types.NamespacedName{Name: target.CRName}}}
	matchingCM := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "custom-ca-bundle", Namespace: target.Namespace,
	}}
	spec := &konfluxv1alpha1.TrustedCAConfigMap{Name: "custom-ca-bundle", Key: "tls.pem"}

	notFound := errors.NewNotFound(schema.GroupResource{Resource: "components"}, target.CRName)
	forbidden := errors.NewForbidden(schema.GroupResource{Resource: "components"}, target.CRName, fmt.Errorf("denied"))

	tests := []struct {
		name string
		obj  client.Object
		spec *konfluxv1alpha1.TrustedCAConfigMap
		err  error
		want []reconcile.Request
	}{
		{
			name: "enqueues when the ConfigMap matches spec.trustedCA",
			obj:  matchingCM,
			spec: spec,
			want: want,
		},
		{
			name: "ignores ConfigMaps in other namespaces",
			obj: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name: "custom-ca-bundle", Namespace: "other",
			}},
			err: forbidden,
		},
		{
			name: "ignores ConfigMaps with a different name",
			obj: &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
				Name: "other-bundle", Namespace: target.Namespace,
			}},
			spec: spec,
		},
		{
			name: "ignores ConfigMaps when the CR is missing",
			obj:  matchingCM,
			err:  notFound,
		},
		{
			name: "ignores ConfigMaps when trustedCA is omitted",
			obj:  matchingCM,
		},
		{
			name: "enqueues when getting the CR fails with a non-NotFound error",
			obj:  matchingCM,
			err:  forbidden,
			want: want,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)
			g.Expect(mapTrustedCAConfigMap(tt.obj, target, tt.spec, tt.err)).To(gomega.Equal(tt.want))
		})
	}
}

func TestNewTrustedCAConfigMapMapper(t *testing.T) {
	target := testTrustedCATarget()
	spec := &konfluxv1alpha1.TrustedCAConfigMap{Name: "custom-ca-bundle", Key: "tls.pem"}
	want := []reconcile.Request{{NamespacedName: types.NamespacedName{Name: target.CRName}}}
	matchingCM := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
		Name: "custom-ca-bundle", Namespace: target.Namespace,
	}}

	t.Run("calls lookup with the target CR name and enqueues on match", func(t *testing.T) {
		g := gomega.NewWithT(t)
		var calledWith string
		mapper := NewTrustedCAConfigMapMapper(target, func(_ context.Context, crName string) (*konfluxv1alpha1.TrustedCAConfigMap, error) {
			calledWith = crName
			return spec, nil
		})
		g.Expect(mapper(context.Background(), matchingCM)).To(gomega.Equal(want))
		g.Expect(calledWith).To(gomega.Equal(target.CRName))
	})

	t.Run("returns nil when lookup returns nil spec", func(t *testing.T) {
		g := gomega.NewWithT(t)
		mapper := NewTrustedCAConfigMapMapper(target, func(_ context.Context, _ string) (*konfluxv1alpha1.TrustedCAConfigMap, error) {
			return nil, nil
		})
		g.Expect(mapper(context.Background(), matchingCM)).To(gomega.BeNil())
	})
}
