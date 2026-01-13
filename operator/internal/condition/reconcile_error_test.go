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
	"errors"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

// mockStatusWriter is a mock implementation of client.StatusWriter for testing
type mockStatusWriter struct {
	updateCalled bool
	updateErr    error
	lastObject   client.Object
}

func (m *mockStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	m.updateCalled = true
	m.lastObject = obj
	return m.updateErr
}

func (m *mockStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return nil
}

func (m *mockStatusWriter) Create(ctx context.Context, obj client.Object, subResource client.Object, opts ...client.SubResourceCreateOption) error {
	return nil
}

var _ = Describe("ReconcileErrorHandler", func() {
	var (
		ctx        context.Context
		testObject *konfluxv1alpha1.KonfluxRBAC
		mockStatus *mockStatusWriter
		handler    *ReconcileErrorHandler
		testErr    error
	)

	BeforeEach(func() {
		ctx = context.Background()
		testErr = errors.New("test error")

		testObject = &konfluxv1alpha1.KonfluxRBAC{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "test-rbac",
				Namespace:  "default",
				Generation: 1,
			},
		}

		mockStatus = &mockStatusWriter{}
		handler = NewReconcileErrorHandler(
			logr.Discard(), // Use a discarding logger for tests
			mockStatus,
			testObject,
			"KonfluxRBAC",
		)
	})

	Describe("NewReconcileErrorHandler", func() {
		It("should create a handler with the correct fields", func() {
			Expect(handler).NotTo(BeNil())
			Expect(handler.cr).To(Equal(testObject))
			Expect(handler.crKind).To(Equal("KonfluxRBAC"))
			Expect(handler.statusClient).To(Equal(mockStatus))
		})
	})

	Describe("Handle", func() {
		It("should set a failed condition on the CR", func() {
			_, returnedErr := handler.Handle(ctx, testErr, "TestReason", "test operation")

			Expect(returnedErr).To(Equal(testErr))

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), TypeReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("TestReason"))
			Expect(condition.Message).To(ContainSubstring("test operation"))
			Expect(condition.Message).To(ContainSubstring("test error"))
		})

		It("should call status update", func() {
			_, _ = handler.Handle(ctx, testErr, "TestReason", "test operation")

			Expect(mockStatus.updateCalled).To(BeTrue())
			Expect(mockStatus.lastObject).To(Equal(testObject))
		})

		It("should return empty result and original error", func() {
			result, returnedErr := handler.Handle(ctx, testErr, "TestReason", "test operation")

			Expect(result.Requeue).To(BeFalse())
			Expect(result.RequeueAfter).To(BeZero())
			Expect(returnedErr).To(Equal(testErr))
		})

		It("should not fail if status update fails", func() {
			mockStatus.updateErr = errors.New("update failed")

			result, returnedErr := handler.Handle(ctx, testErr, "TestReason", "test operation")

			// Should still return the original error, not the update error
			Expect(returnedErr).To(Equal(testErr))
			Expect(result.Requeue).To(BeFalse())
		})

		It("should include operation in error message", func() {
			_, _ = handler.Handle(ctx, testErr, "TestReason", "apply custom resources")

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), TypeReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Message).To(Equal("apply custom resources: test error"))
		})
	})

	Describe("HandleApplyError", func() {
		It("should use ReasonApplyFailed and apply manifests operation", func() {
			_, returnedErr := handler.HandleApplyError(ctx, testErr)

			Expect(returnedErr).To(Equal(testErr))

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), TypeReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Reason).To(Equal(ReasonApplyFailed))
			Expect(condition.Message).To(ContainSubstring("apply manifests"))
		})
	})

	Describe("HandleCleanupError", func() {
		It("should use ReasonCleanupFailed and cleanup operation", func() {
			_, returnedErr := handler.HandleCleanupError(ctx, testErr)

			Expect(returnedErr).To(Equal(testErr))

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), TypeReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Reason).To(Equal(ReasonCleanupFailed))
			Expect(condition.Message).To(ContainSubstring("cleanup orphaned resources"))
		})
	})

	Describe("HandleStatusUpdateError", func() {
		It("should use ReasonStatusUpdateFailed and status update operation", func() {
			_, returnedErr := handler.HandleStatusUpdateError(ctx, testErr)

			Expect(returnedErr).To(Equal(testErr))

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), TypeReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Reason).To(Equal(ReasonStatusUpdateFailed))
			Expect(condition.Message).To(ContainSubstring("update component statuses"))
		})
	})

	Describe("HandleWithReason", func() {
		It("should use custom reason and operation", func() {
			_, returnedErr := handler.HandleWithReason(ctx, testErr, "CustomReason", "custom operation")

			Expect(returnedErr).To(Equal(testErr))

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), TypeReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Reason).To(Equal("CustomReason"))
			Expect(condition.Message).To(ContainSubstring("custom operation"))
		})
	})

	Describe("error message formatting", func() {
		It("should wrap the original error with operation context", func() {
			originalErr := errors.New("connection refused")
			_, _ = handler.Handle(ctx, originalErr, "NetworkError", "connect to API server")

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), TypeReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Message).To(Equal("connect to API server: connection refused"))
		})

		It("should handle wrapped errors correctly", func() {
			innerErr := errors.New("inner error")
			wrappedErr := errors.New("outer error: " + innerErr.Error())
			_, _ = handler.Handle(ctx, wrappedErr, "WrappedError", "process request")

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), TypeReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Message).To(Equal("process request: outer error: inner error"))
		})
	})

	Describe("observed generation", func() {
		It("should set observed generation from CR", func() {
			testObject.Generation = 5
			_, _ = handler.Handle(ctx, testErr, "TestReason", "test operation")

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), TypeReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.ObservedGeneration).To(Equal(int64(5)))
		})
	})
})
