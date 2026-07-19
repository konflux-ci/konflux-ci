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

package predicate

import (
	"maps"

	appsv1 "k8s.io/api/apps/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

// PrometheusScrapeTokenSecretPredicate limits Secret watches to the operator-managed
// prometheus-scrape-token used for metrics scraping.
var PrometheusScrapeTokenSecretPredicate = predicate.NewPredicateFuncs(kubernetes.IsPrometheusScrapeTokenSecret)

// MetricsTLSSecretPredicate limits Secret watches to metrics-server-cert
// (cert-manager issued; watched by name because it is not a CR ownerRef).
var MetricsTLSSecretPredicate = predicate.NewPredicateFuncs(kubernetes.IsMetricsTLSSecret)

// generationOrMetadataChanged returns true if the update is NOT a status-only change.
// It detects spec changes (generation bump), ownerReference changes, and
// label/annotation changes -- none of which are reflected in the generation
// field alone.
func generationOrMetadataChanged(oldObj, newObj client.Object) bool {
	if oldObj.GetGeneration() != newObj.GetGeneration() {
		return true
	}
	if !apiequality.Semantic.DeepEqual(oldObj.GetOwnerReferences(), newObj.GetOwnerReferences()) {
		return true
	}
	if !maps.Equal(oldObj.GetLabels(), newObj.GetLabels()) {
		return true
	}
	return !maps.Equal(oldObj.GetAnnotations(), newObj.GetAnnotations())
}

// IgnoreStatusUpdatesPredicate filters out status-only updates.
// Reconciliation triggers on spec changes (generation bump), ownerReference
// changes, and label/annotation changes.
//
// IMPORTANT: This predicate relies on metadata.generation incrementing on spec
// changes. It must NOT be used on resources where generation is unreliable:
//
//   - Generation-exempt resources (hardcoded in kube-apiserver, generation
//     always 0): Services, Namespaces, Nodes, PVs, PVCs, ResourceQuotas.
//     Spec changes on these resources will NOT trigger reconciliation.
//
//   - Resources without a status subresource (Secrets, ConfigMaps,
//     ServiceAccounts, RBAC resources). These never receive status-only
//     updates, so the predicate provides no filtering benefit and can only
//     block legitimate data changes.
//
// Safe to use on resources with a status subresource AND proper generation
// tracking: Deployments (use DeploymentReadinessPredicate instead),
// Certificates, Issuers, CronJobs, Ingresses, and all CRDs with /status.
//
// Exception: Namespaces keep this predicate because their spec only contains
// .spec.finalizers (not managed by our controllers), and filtering
// .status.phase transitions is desirable. Label/annotation drift is always
// detected via the metadata comparison path regardless of generation.
var IgnoreStatusUpdatesPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return true
		}
		return generationOrMetadataChanged(e.ObjectOld, e.ObjectNew)
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return true
	},
}

// DeploymentReadinessPredicate extends IgnoreStatusUpdatesPredicate by also
// triggering on deployment readiness status changes (ReadyReplicas,
// AvailableReplicas, etc.) so we can react to health changes without polling.
var DeploymentReadinessPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return true
		}
		if generationOrMetadataChanged(e.ObjectOld, e.ObjectNew) {
			return true
		}
		// Check for meaningful status changes
		oldDep, ok1 := e.ObjectOld.(*appsv1.Deployment)
		newDep, ok2 := e.ObjectNew.(*appsv1.Deployment)
		if !ok1 || !ok2 {
			return true
		}
		// Trigger on readiness changes
		return oldDep.Status.ReadyReplicas != newDep.Status.ReadyReplicas ||
			oldDep.Status.AvailableReplicas != newDep.Status.AvailableReplicas ||
			oldDep.Status.UnavailableReplicas != newDep.Status.UnavailableReplicas ||
			oldDep.Status.UpdatedReplicas != newDep.Status.UpdatedReplicas ||
			oldDep.Status.Replicas != newDep.Status.Replicas
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return true
	},
}

// KonfluxUIIngressStatusChangedPredicate extends IgnoreStatusUpdatesPredicate
// by also triggering when the Status.Ingress field changes in KonfluxUI CR.
var KonfluxUIIngressStatusChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return true
		}
		if generationOrMetadataChanged(e.ObjectOld, e.ObjectNew) {
			return true
		}
		// Check for ingress status changes
		oldUI, ok1 := e.ObjectOld.(*konfluxv1alpha1.KonfluxUI)
		newUI, ok2 := e.ObjectNew.(*konfluxv1alpha1.KonfluxUI)
		if !ok1 || !ok2 {
			return true
		}
		// Compare ingress status fields
		oldIngress := oldUI.Status.Ingress
		newIngress := newUI.Status.Ingress
		// If both are nil, no change
		if oldIngress == nil && newIngress == nil {
			return false
		}
		// If one is nil and the other is not, there's a change
		if oldIngress == nil || newIngress == nil {
			return true
		}
		// Compare ingress status fields
		return oldIngress.Enabled != newIngress.Enabled ||
			oldIngress.Hostname != newIngress.Hostname ||
			oldIngress.URL != newIngress.URL
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return true
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return true
	},
}
