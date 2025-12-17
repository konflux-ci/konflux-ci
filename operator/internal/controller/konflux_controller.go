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

package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

const (
	// KonfluxOwnerLabel is the label used to identify resources owned by the Konflux operator.
	KonfluxOwnerLabel = "konflux.konflux-ci.dev/owner"
	// KonfluxComponentLabel is the label used to identify which component a resource belongs to.
	KonfluxComponentLabel = "konflux.konflux-ci.dev/component"
	// KonfluxCRName is the singleton name for the Konflux CR.
	KonfluxCRName = "konflux"
)

// KonfluxReconciler reconciles a Konflux object
type KonfluxReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=konflux.konflux-ci.dev,resources=konfluxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=*,resources=*,verbs=*

// TODO: Set proper RBAC rules for the controller

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *KonfluxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Konflux instance
	konflux := &konfluxv1alpha1.Konflux{}
	if err := r.Get(ctx, req.NamespacedName, konflux); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling Konflux", "name", konflux.Name)

	// Apply all embedded manifests
	if err := r.applyAllManifests(ctx, konflux); err != nil {
		log.Error(err, "Failed to apply manifests")
		return ctrl.Result{}, err
	}

	log.Info("Successfully applied all manifests")
	return ctrl.Result{}, nil
}

// applyAllManifests loads and applies all embedded manifests to the cluster.
func (r *KonfluxReconciler) applyAllManifests(ctx context.Context, owner *konfluxv1alpha1.Konflux) error {
	log := logf.FromContext(ctx)

	return manifests.WalkManifests(func(info manifests.ManifestInfo) error {
		log.Info("Applying manifests", "component", info.Component)

		objects, err := parseManifests(info.Content)
		if err != nil {
			return fmt.Errorf("failed to parse manifests for %s: %w", info.Component, err)
		}
		objects = transformObjectsForComponent(objects, info.Component, owner)
		for _, obj := range objects {
			// Set ownership labels and owner reference

			if err := r.setOwnership(obj, owner, string(info.Component)); err != nil {
				return fmt.Errorf("failed to set ownership for %s/%s (%s) from %s: %w",
					obj.GetNamespace(), obj.GetName(), obj.GetKind(), info.Component, err)
			}

			if err := r.applyObject(ctx, obj); err != nil {
				if obj.GroupVersionKind().Group == "cert-manager.io" {
					// TODO: Remove this once we decide how to install cert-manager crds in envtest
					log.Info("Skipping resource: CRD not installed",
						"kind", obj.GetKind(),
						"apiVersion", obj.GetAPIVersion(),
						"namespace", obj.GetNamespace(),
						"name", obj.GetName(),
					)
					continue
				}
				return fmt.Errorf("failed to apply object %s/%s (%s) from %s: %w",
					obj.GetNamespace(), obj.GetName(), obj.GetKind(), info.Component, err)
			}
		}

		log.Info("Applied manifests", "component", info.Component, "objectCount", len(objects))
		return nil
	})
}

func transformObjectsForComponent(objects []*unstructured.Unstructured, component manifests.Component, konflux *konfluxv1alpha1.Konflux) []*unstructured.Unstructured {
	switch component {
	case manifests.ApplicationAPI:
		return objects
	case manifests.BuildService:
		return objects
	case manifests.EnterpriseContract:
		return objects
	case manifests.ImageController:
		return transformObjectsForImageController(objects, konflux)
	case manifests.Integration:
		return objects
	case manifests.NamespaceLister:
		return objects
	case manifests.RBAC:
		return objects
	case manifests.Release:
		return objects
	case manifests.UI:
		return objects
	default:
		return objects
	}
}

func transformObjectsForImageController(_ []*unstructured.Unstructured, _ *konfluxv1alpha1.Konflux) []*unstructured.Unstructured {
	return []*unstructured.Unstructured{}
}

// parseManifests parses YAML content into a slice of unstructured objects.
func parseManifests(content []byte) ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured

	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(content), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(obj); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to decode manifest: %w", err)
		}

		// Skip empty documents
		if len(obj.Object) == 0 {
			continue
		}

		objects = append(objects, obj)
	}

	return objects, nil
}

// setOwnership sets owner reference and labels on the object to establish ownership.
func (r *KonfluxReconciler) setOwnership(obj *unstructured.Unstructured, owner *konfluxv1alpha1.Konflux, component string) error {
	// Set ownership labels
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[KonfluxOwnerLabel] = owner.Name
	labels[KonfluxComponentLabel] = component
	obj.SetLabels(labels)

	// Set owner reference for garbage collection and watch triggers
	// Using controllerutil.SetControllerReference to properly set the owner reference
	if err := controllerutil.SetControllerReference(owner, obj, r.Scheme); err != nil {
		return fmt.Errorf("failed to set controller reference: %w", err)
	}

	return nil
}

// applyObject applies a single unstructured object to the cluster using server-side apply.
func (r *KonfluxReconciler) applyObject(ctx context.Context, obj *unstructured.Unstructured) error {
	log := logf.FromContext(ctx)

	log.V(1).Info("Applying object",
		"kind", obj.GetKind(),
		"namespace", obj.GetNamespace(),
		"name", obj.GetName(),
	)

	// Use server-side apply with the field manager "konflux-operator"
	err := r.Patch(ctx, obj, client.Apply, client.FieldOwner("konflux-operator"), client.ForceOwnership)
	if err != nil {
		// Skip resources whose CRDs are not installed (e.g., cert-manager Certificate)
		var noKindMatchErr *meta.NoKindMatchError
		if errors.As(err, &noKindMatchErr) {
			log.Info("Skipping resource: CRD not installed",
				"kind", obj.GetKind(),
				"apiVersion", obj.GetAPIVersion(),
				"namespace", obj.GetNamespace(),
				"name", obj.GetName(),
			)
			return nil
		}
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KonfluxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&konfluxv1alpha1.Konflux{}).
		Named("konflux").
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Namespace{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&rbacv1.ClusterRole{}).
		Owns(&rbacv1.ClusterRoleBinding{}).
		Owns(&apiextensionsv1.CustomResourceDefinition{}).
		Owns(&certmanagerv1.Certificate{}).
		Owns(&certmanagerv1.Issuer{}).
		Complete(r)
}
