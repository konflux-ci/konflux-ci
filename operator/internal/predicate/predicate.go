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
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// GenerationChangedPredicate filters out events where the generation hasn't changed
// (i.e., status-only updates that shouldn't trigger reconciliation)
var GenerationChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return true
		}
		// Only reconcile if the generation changed (spec was modified)
		// This filters out status-only updates
		return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
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

// DeploymentReadinessPredicate triggers reconciliation when:
// - Spec changes (generation changed)
// - Readiness status changes (ReadyReplicas, AvailableReplicas, UnavailableReplicas)
// This allows us to react to deployment health changes without polling
var DeploymentReadinessPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return true
		}
		// Always reconcile on spec changes
		if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
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

// LabelsOrAnnotationsChangedPredicate triggers reconciliation when labels or annotations change
// Used for resources like ConfigMaps that don't have a generation field that changes on data updates
var LabelsOrAnnotationsChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return true
		}
		// Check if generation changed
		if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
			return true
		}
		// Also check labels and annotations for resources without generation updates
		return !reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) ||
			!reflect.DeepEqual(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations())
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
