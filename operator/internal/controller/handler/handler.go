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

// Package handler provides shared controller-runtime handler helpers (e.g. map
// functions for watches) used by reconcilers.
package handler

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlhandler "sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

// MapCRDToRequest returns a handler that enqueues a reconcile request for the
// given CR when a CRD managed by the component is updated or deleted. Use it
// with Watches(..., handler.EnqueueRequestsFromMapFunc(...)) so that
// out-of-band CRD deletion triggers reconciliation and re-apply.
// It returns an error if the store fails or the component has no CRD names.
//
//	store is the ObjectStore (e.g. r.ObjectStore).
//	component is the manifests component that deploys the CRDs (e.g. manifests.ApplicationAPI).
//	crName is the name of the singleton CR to enqueue (e.g. applicationapi.CRName).
func MapCRDToRequest(
	store *manifests.ObjectStore,
	component manifests.Component,
	crName string,
) (ctrlhandler.MapFunc, error) {
	names, err := store.GetCRDNamesForComponent(component)
	if err != nil {
		return func(context.Context, client.Object) []reconcile.Request { return nil }, err
	}
	if len(names) == 0 {
		return func(
			context.Context, client.Object) []reconcile.Request {
			return nil
		}, fmt.Errorf("no CRD names for component %s", component)
	}
	managedNames := sets.New(names...)
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		if managedNames.Has(obj.GetName()) {
			return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: crName}}}
		}
		return nil
	}, nil
}
