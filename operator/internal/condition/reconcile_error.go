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

package condition

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

// ReconcileErrorHandler handles error reporting for reconcilers.
// It provides a consistent way to set failed conditions, update status,
// and log errors with proper context.
type ReconcileErrorHandler struct {
	log          logr.Logger
	statusClient client.StatusWriter
	cr           konfluxv1alpha1.ConditionAccessor
	crKind       string
}

// NewReconcileErrorHandler creates a new ReconcileErrorHandler for a specific CR.
// crKind is used in error messages to identify which CR type failed (e.g., "KonfluxBuildService").
func NewReconcileErrorHandler(
	log logr.Logger,
	statusClient client.StatusWriter,
	cr konfluxv1alpha1.ConditionAccessor,
	crKind string,
) *ReconcileErrorHandler {
	return &ReconcileErrorHandler{
		log:          log,
		statusClient: statusClient,
		cr:           cr,
		crKind:       crKind,
	}
}

// Handle sets a failed condition on the CR, updates the status, and returns the error.
// The operation parameter provides context about what operation failed (e.g., "apply manifests", "cleanup orphans").
// Returns a ctrl.Result and the original error, suitable for returning from a Reconcile function.
func (h *ReconcileErrorHandler) Handle(
	ctx context.Context,
	err error,
	reason string,
	operation string,
) (ctrl.Result, error) {
	h.log.Error(err, fmt.Sprintf("Failed to %s", operation))

	// Set the failed condition with a descriptive message
	SetFailedCondition(h.cr, TypeReady, reason, fmt.Errorf("%s: %w", operation, err))

	// Update the status
	if updateErr := h.statusClient.Update(ctx, h.cr); updateErr != nil {
		h.log.Error(updateErr, fmt.Sprintf("Failed to update %s status after %s failure", h.crKind, operation))
	}

	return ctrl.Result{}, err
}

// HandleApplyError handles errors that occur when applying manifests.
func (h *ReconcileErrorHandler) HandleApplyError(ctx context.Context, err error) (ctrl.Result, error) {
	return h.Handle(ctx, err, ReasonApplyFailed, "apply manifests")
}

// HandleCleanupError handles errors that occur when cleaning up orphaned resources.
func (h *ReconcileErrorHandler) HandleCleanupError(ctx context.Context, err error) (ctrl.Result, error) {
	return h.Handle(ctx, err, ReasonCleanupFailed, "cleanup orphaned resources")
}

// HandleStatusUpdateError handles errors that occur when updating component statuses.
func (h *ReconcileErrorHandler) HandleStatusUpdateError(ctx context.Context, err error) (ctrl.Result, error) {
	return h.Handle(ctx, err, ReasonStatusUpdateFailed, "update component statuses")
}

// HandleWithReason handles errors with a custom reason and operation description.
// This is useful for component-specific error handling that doesn't fit the standard patterns.
func (h *ReconcileErrorHandler) HandleWithReason(ctx context.Context, err error, reason string, operation string) (ctrl.Result, error) {
	return h.Handle(ctx, err, reason, operation)
}
