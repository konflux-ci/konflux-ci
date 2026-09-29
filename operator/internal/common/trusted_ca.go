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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/contenthash"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/konflux-ci/konflux-ci/operator/pkg/tracking"
)

const (
	// TrustedCAConfigMapName is the name of the ConfigMap that OpenShift's
	// cluster network operator populates with the cluster-wide trusted CA bundle.
	TrustedCAConfigMapName = "trusted-ca"

	// OpenShiftInjectTrustedCABundleLabel is the label that triggers OpenShift's
	// CA bundle injection into a ConfigMap.
	OpenShiftInjectTrustedCABundleLabel = "config.openshift.io/inject-trusted-cabundle"

	// TrustedCAVolumeName is the extra-file volume baked into component Deployments.
	TrustedCAVolumeName = "trusted-ca"

	// TrustedCADefaultFileMountPath is where the extra CA file is mounted.
	// CAs in that file are added to the image trust store; the store is not replaced.
	TrustedCADefaultFileMountPath = "/etc/ssl/certs/ca-custom-bundle.crt"

	// TrustedCADefaultFileVolumePath is the projected filename and the default ConfigMap key.
	TrustedCADefaultFileVolumePath = "ca-bundle.crt"

	// TrustedCAHashAnnotation is stamped on the controller-manager pod template
	// so a ConfigMap content change updates the pod spec and Kubernetes rolls
	// the Deployment. subPath mounts do not propagate ConfigMap updates, and
	// Go's SystemCertPool is loaded once per process.
	TrustedCAHashAnnotation = "konflux.konflux-ci.dev/trusted-ca-content-hash"
)

// TrustedCATarget identifies the operand Deployment and singleton CR whose
// extra-file CA mount is retargeted.
type TrustedCATarget struct {
	Namespace      string
	DeploymentName string
	ContainerName  string
	CRName         string
}

// EnsureTrustedCAConfigMap creates or updates ConfigMap trusted-ca with the
// OpenShift CA injection label in the given namespace. On non-OpenShift clusters
// (or when clusterInfo is nil) this is a no-op. OpenShift's cluster network operator
// automatically populates the ca-bundle.crt key when this label is present.
func EnsureTrustedCAConfigMap(ctx context.Context, namespace string, tc *tracking.Client, ci *clusterinfo.Info) error {
	if ci == nil || !ci.IsOpenShift() {
		return nil
	}
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      TrustedCAConfigMapName,
			Namespace: namespace,
			Labels: map[string]string{
				OpenShiftInjectTrustedCABundleLabel: "true",
			},
		},
	}
	if err := tc.ApplyOwned(ctx, configMap); err != nil {
		return fmt.Errorf("failed to apply trusted-ca ConfigMap in %s: %w", namespace, err)
	}
	return nil
}

// ApplyTrustedCAMount retargets the baked-in trusted-ca volume to the
// ConfigMap named in spec when spec is set. The extra-file mount path and
// subPath are left unchanged so additional CAs are added at the same location
// in the image trust store. When spec is nil the embedded volume is left
// unchanged. contentHash is stamped on the pod template so ConfigMap data
// changes roll the Deployment. An empty hash omits the annotation, including
// a platform bundle that is not populated yet.
//
// Production objects are fresh copies of the embedded manifests, so they
// never carry the hash annotation. Omitting it on apply lets server-side
// apply prune it from the live Deployment. Callers copy a live hash when a
// user ConfigMap or key is missing so a successful mount is not rolled away.
// The delete calls only strip a leftover on an in-memory object that already
// has one.
func ApplyTrustedCAMount(deployment *appsv1.Deployment, spec *konfluxv1alpha1.TrustedCAConfigMap, contentHash, containerName string) error {
	if spec != nil {
		vol := kubernetes.FindVolume(deployment.Spec.Template.Spec.Volumes, TrustedCAVolumeName)
		if vol == nil || vol.ConfigMap == nil {
			return fmt.Errorf("trusted-ca ConfigMap volume not found on deployment %s", deployment.Name)
		}
		vol.ConfigMap.Name = spec.Name
		vol.ConfigMap.Items = []corev1.KeyToPath{{
			Key:  spec.Key,
			Path: TrustedCADefaultFileVolumePath,
		}}
		vol.ConfigMap.Optional = ptr.To(false)

		manager := kubernetes.FindContainer(deployment.Spec.Template.Spec.Containers, containerName)
		if manager == nil {
			return fmt.Errorf("container %q not found on deployment %s", containerName, deployment.Name)
		}
		if kubernetes.FindVolumeMount(manager.VolumeMounts, TrustedCAVolumeName) == nil {
			return fmt.Errorf("trusted-ca volume mount not found on container %q", containerName)
		}
	}

	if contentHash == "" {
		delete(deployment.Spec.Template.Annotations, TrustedCAHashAnnotation)
		return nil
	}
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations[TrustedCAHashAnnotation] = contentHash
	return nil
}

// TrustedCAHashForApply returns the hash to stamp on the pod template.
// A present ConfigMap key is hashed. Missing user ConfigMaps or keys fall
// back to the live Deployment annotation so server-side apply does not prune
// it and roll running pods. spec == nil hashes ConfigMap trusted-ca key
// ca-bundle.crt on OpenShift and returns empty otherwise, including when that
// key is not populated yet. Any non-NotFound Get error is returned so the
// Deployment is not applied.
func TrustedCAHashForApply(ctx context.Context, c client.Reader, ci *clusterinfo.Info, spec *konfluxv1alpha1.TrustedCAConfigMap, target TrustedCATarget) (string, error) {
	if spec == nil {
		if ci == nil || !ci.IsOpenShift() {
			return "", nil
		}
		return LookupTrustedCAHash(ctx, c, target.Namespace, &konfluxv1alpha1.TrustedCAConfigMap{
			Name: TrustedCAConfigMapName,
			Key:  TrustedCADefaultFileVolumePath,
		})
	}
	hash, err := LookupTrustedCAHash(ctx, c, target.Namespace, spec)
	if err != nil {
		return "", err
	}
	if hash != "" {
		return hash, nil
	}
	return liveTrustedCAHash(ctx, c, target)
}

func liveTrustedCAHash(ctx context.Context, c client.Reader, target TrustedCATarget) (string, error) {
	dep := &appsv1.Deployment{}
	if err := c.Get(ctx, types.NamespacedName{
		Name:      target.DeploymentName,
		Namespace: target.Namespace,
	}, dep); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return dep.Spec.Template.Annotations[TrustedCAHashAnnotation], nil
}

// LookupTrustedCAHash returns a content hash of spec.Key in the referenced
// ConfigMap. Missing ConfigMaps and missing keys return ("", nil). A nil spec
// returns ("", nil). Any other Get error is returned so the Deployment is not
// applied with an empty hash.
func LookupTrustedCAHash(ctx context.Context, c client.Reader, namespace string, spec *konfluxv1alpha1.TrustedCAConfigMap) (string, error) {
	if spec == nil {
		return "", nil
	}
	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, types.NamespacedName{Name: spec.Name, Namespace: namespace}, cm); err != nil {
		if apierrors.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return TrustedCABundleHash(cm, spec.Key), nil
}

// TrustedCABundleHash returns a content hash of key in cm.
// Data is preferred over BinaryData. A missing key returns an empty string.
func TrustedCABundleHash(cm *corev1.ConfigMap, key string) string {
	if v, ok := cm.Data[key]; ok {
		return contenthash.String(v)
	}
	if v, ok := cm.BinaryData[key]; ok {
		return contenthash.String(string(v))
	}
	return ""
}

// TrustedCASpecLookup reads the component CR and returns its TrustedCA spec.
// A nil spec with a nil error means the field is omitted. A NotFound error
// means the CR is absent. Any other error is transient.
type TrustedCASpecLookup func(ctx context.Context, crName string) (*konfluxv1alpha1.TrustedCAConfigMap, error)

// NewTrustedCAConfigMapMapper returns a handler.MapFunc that enqueues the
// singleton component CR when the ConfigMap named in spec.trustedCA changes.
// lookup reads the component CR and returns its TrustedCA spec.
// ConfigMaps outside target.Namespace are ignored.
func NewTrustedCAConfigMapMapper(target TrustedCATarget, lookup TrustedCASpecLookup) func(ctx context.Context, obj client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		spec, err := lookup(ctx, target.CRName)
		return mapTrustedCAConfigMap(obj, target, spec, err)
	}
}

// mapTrustedCAConfigMap enqueues the singleton component CR when obj is the
// ConfigMap named in spec. spec and err are the result of reading the caller's
// component CR. A NotFound error means the CR is absent. A nil spec with a nil
// error means the field is omitted. Any other error is transient and enqueues
// the CR so the ConfigMap event is not dropped until the next informer resync.
// ConfigMaps outside target.Namespace are ignored, including when err is set.
func mapTrustedCAConfigMap(obj client.Object, target TrustedCATarget, spec *konfluxv1alpha1.TrustedCAConfigMap, err error) []reconcile.Request {
	if obj.GetNamespace() != target.Namespace {
		return nil
	}
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: target.CRName}}}
	}
	if spec == nil || spec.Name != obj.GetName() {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: target.CRName}}}
}
