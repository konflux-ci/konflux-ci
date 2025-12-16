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

package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var konfluxlog = logf.Log.WithName("konflux-resource")

// SetupKonfluxWebhookWithManager registers the webhook for Konflux in the manager.
func SetupKonfluxWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&konfluxv1alpha1.Konflux{}).
		WithValidator(&KonfluxCustomValidator{Client: mgr.GetClient()}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-konflux-konflux-ci-dev-v1alpha1-konflux,mutating=false,failurePolicy=fail,sideEffects=None,groups=konflux.konflux-ci.dev,resources=konfluxes,verbs=create;update,versions=v1alpha1,name=vkonflux-v1alpha1.kb.io,admissionReviewVersions=v1

// KonfluxCustomValidator struct is responsible for validating the Konflux resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type KonfluxCustomValidator struct {
	Client client.Client
}

var _ webhook.CustomValidator = &KonfluxCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Konflux.
func (v *KonfluxCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	konflux, ok := obj.(*konfluxv1alpha1.Konflux)
	if !ok {
		return nil, fmt.Errorf("expected a Konflux object but got %T", obj)
	}
	konfluxlog.Info("Validation for Konflux upon creation", "name", konflux.GetName())

	// Enforce singleton: only one Konflux instance is allowed per cluster
	// List all existing Konflux instances and reject creation if any exist
	// NOTE: This check has a race condition - if two ValidateCreate calls happen concurrently,
	// both might pass this check before either instance is persisted. ValidateUpdate provides
	// a secondary safety net to catch violations that slip through.
	existingList := &konfluxv1alpha1.KonfluxList{}
	if err := v.Client.List(ctx, existingList); err != nil {
		return nil, fmt.Errorf("failed to list existing Konflux instances: %w", err)
	}

	if len(existingList.Items) > 0 {
		existingName := existingList.Items[0].GetName()
		return nil, fmt.Errorf(
			"only one Konflux instance is allowed per cluster, but instance %q already exists",
			existingName,
		)
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Konflux.
func (v *KonfluxCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	konflux, ok := newObj.(*konfluxv1alpha1.Konflux)
	if !ok {
		return nil, fmt.Errorf("expected a Konflux object for the newObj but got %T", newObj)
	}
	konfluxlog.Info("Validation for Konflux upon update", "name", konflux.GetName())

	// Enforce singleton: during an update, there should be exactly one Konflux instance
	// (the one being updated). This provides a secondary safety net to catch violations
	// that might occur due to race conditions during concurrent resource creation.
	existingList := &konfluxv1alpha1.KonfluxList{}
	if err := v.Client.List(ctx, existingList); err != nil {
		return nil, fmt.Errorf("failed to list existing Konflux instances: %w", err)
	}

	// During an update, there should be exactly one Konflux instance (the one being updated).
	// If more than one exists (due to a race condition on create), this validation will fail.
	for _, item := range existingList.Items {
		if item.GetUID() != konflux.GetUID() {
			return nil, fmt.Errorf(
				"only one Konflux instance is allowed per cluster, but another instance %q was found",
				item.GetName(),
			)
		}
	}

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Konflux.
func (v *KonfluxCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	konflux, ok := obj.(*konfluxv1alpha1.Konflux)
	if !ok {
		return nil, fmt.Errorf("expected a Konflux object but got %T", obj)
	}
	konfluxlog.Info("Validation for Konflux upon deletion", "name", konflux.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
