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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

var _ = Describe("Conditions Helper Functions", func() {
	Describe("SetOverallReadyCondition", func() {
		var testObject *konfluxv1alpha1.KonfluxRBAC

		BeforeEach(func() {
			// Use KonfluxRBAC as a test object since it implements ConditionAccessor
			testObject = &konfluxv1alpha1.KonfluxRBAC{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-rbac",
					Namespace:  "default",
					Generation: 1,
				},
			}
		})

		Context("when components have deployments", func() {
			It("should set Ready condition with deployment count when all deployments are ready", func() {
				summary := DeploymentStatusSummary{
					AllReady:      true,
					TotalCount:    3,
					NotReadyNames: []string{},
				}

				SetOverallReadyCondition(testObject, "Ready", summary)

				condition := apimeta.FindStatusCondition(testObject.GetConditions(), "Ready")
				Expect(condition).NotTo(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				Expect(condition.Reason).To(Equal("AllComponentsReady"))
				Expect(condition.Message).To(Equal("All 3 deployments are ready"))
				Expect(condition.ObservedGeneration).To(Equal(int64(1)))
			})

			It("should set Ready condition with deployment count when multiple deployments are ready", func() {
				summary := DeploymentStatusSummary{
					AllReady:      true,
					TotalCount:    5,
					NotReadyNames: []string{},
				}

				SetOverallReadyCondition(testObject, "Ready", summary)

				condition := apimeta.FindStatusCondition(testObject.GetConditions(), "Ready")
				Expect(condition).NotTo(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				Expect(condition.Message).To(Equal("All 5 deployments are ready"))
			})

			It("should set NotReady condition when some deployments are not ready", func() {
				summary := DeploymentStatusSummary{
					AllReady:      false,
					TotalCount:    3,
					NotReadyNames: []string{"deployment-1", "deployment-2"},
				}

				SetOverallReadyCondition(testObject, "Ready", summary)

				condition := apimeta.FindStatusCondition(testObject.GetConditions(), "Ready")
				Expect(condition).NotTo(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				Expect(condition.Reason).To(Equal("ComponentsNotReady"))
				Expect(condition.Message).To(Equal("Deployments not ready: [deployment-1 deployment-2]"))
			})
		})

		Context("when components have no deployments", func() {
			It("should set Ready condition with descriptive message for zero deployments", func() {
				summary := DeploymentStatusSummary{
					AllReady:      true,
					TotalCount:    0,
					NotReadyNames: []string{},
				}

				SetOverallReadyCondition(testObject, "Ready", summary)

				condition := apimeta.FindStatusCondition(testObject.GetConditions(), "Ready")
				Expect(condition).NotTo(BeNil())
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				Expect(condition.Reason).To(Equal("AllComponentsReady"))
				Expect(condition.Message).To(Equal("Component ready (no deployments to track)"))
				Expect(condition.ObservedGeneration).To(Equal(int64(1)))
			})

			It("should use the correct message format instead of 'All 0 deployments are ready'", func() {
				summary := DeploymentStatusSummary{
					AllReady:   true,
					TotalCount: 0,
				}

				SetOverallReadyCondition(testObject, "Ready", summary)

				condition := apimeta.FindStatusCondition(testObject.GetConditions(), "Ready")
				Expect(condition).NotTo(BeNil())
				Expect(condition.Message).NotTo(Equal("All 0 deployments are ready"))
				Expect(condition.Message).To(Equal("Component ready (no deployments to track)"))
			})
		})

		Context("when using custom condition types", func() {
			It("should respect the custom ready condition type", func() {
				summary := DeploymentStatusSummary{
					AllReady:   true,
					TotalCount: 0,
				}

				SetOverallReadyCondition(testObject, "CustomReady", summary)

				condition := apimeta.FindStatusCondition(testObject.GetConditions(), "CustomReady")
				Expect(condition).NotTo(BeNil())
				Expect(condition.Type).To(Equal("CustomReady"))
				Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			})
		})
	})

	Describe("SetCondition", func() {
		var testObject *konfluxv1alpha1.KonfluxRBAC

		BeforeEach(func() {
			testObject = &konfluxv1alpha1.KonfluxRBAC{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-rbac",
					Namespace:  "default",
					Generation: 5,
				},
			}
		})

		It("should add a new condition", func() {
			SetCondition(testObject, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionTrue,
				Reason:  "TestReason",
				Message: "Test message",
			})

			conditions := testObject.GetConditions()
			Expect(conditions).To(HaveLen(1))
			Expect(conditions[0].Type).To(Equal("Ready"))
			Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue))
			Expect(conditions[0].Reason).To(Equal("TestReason"))
			Expect(conditions[0].Message).To(Equal("Test message"))
			Expect(conditions[0].ObservedGeneration).To(Equal(int64(5)))
		})

		It("should update an existing condition", func() {
			// Add initial condition
			SetCondition(testObject, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionFalse,
				Reason:  "InitialReason",
				Message: "Initial message",
			})

			// Update the condition
			SetCondition(testObject, metav1.Condition{
				Type:    "Ready",
				Status:  metav1.ConditionTrue,
				Reason:  "UpdatedReason",
				Message: "Updated message",
			})

			conditions := testObject.GetConditions()
			Expect(conditions).To(HaveLen(1))
			Expect(conditions[0].Status).To(Equal(metav1.ConditionTrue))
			Expect(conditions[0].Reason).To(Equal("UpdatedReason"))
			Expect(conditions[0].Message).To(Equal("Updated message"))
		})
	})

	Describe("IsConditionTrue", func() {
		var testObject *konfluxv1alpha1.KonfluxRBAC

		BeforeEach(func() {
			testObject = &konfluxv1alpha1.KonfluxRBAC{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-rbac",
					Namespace: "default",
				},
			}
		})

		It("should return true when condition exists and status is True", func() {
			SetCondition(testObject, metav1.Condition{
				Type:   "Ready",
				Status: metav1.ConditionTrue,
				Reason: "AllReady",
			})

			Expect(IsConditionTrue(testObject, "Ready")).To(BeTrue())
		})

		It("should return false when condition exists but status is False", func() {
			SetCondition(testObject, metav1.Condition{
				Type:   "Ready",
				Status: metav1.ConditionFalse,
				Reason: "NotReady",
			})

			Expect(IsConditionTrue(testObject, "Ready")).To(BeFalse())
		})

		It("should return false when condition does not exist", func() {
			Expect(IsConditionTrue(testObject, "NonExistent")).To(BeFalse())
		})
	})

	Describe("AggregateReadiness", func() {
		It("should return true when all sub-CRs are ready", func() {
			subCRStatuses := []SubCRStatus{
				{Name: "component-1", Ready: true},
				{Name: "component-2", Ready: true},
				{Name: "component-3", Ready: true},
			}

			allReady, notReadyReasons := AggregateReadiness(subCRStatuses)

			Expect(allReady).To(BeTrue())
			Expect(notReadyReasons).To(BeEmpty())
		})

		It("should return false and reasons when some sub-CRs are not ready", func() {
			subCRStatuses := []SubCRStatus{
				{Name: "component-1", Ready: true},
				{Name: "component-2", Ready: false},
				{Name: "component-3", Ready: false},
			}

			allReady, notReadyReasons := AggregateReadiness(subCRStatuses)

			Expect(allReady).To(BeFalse())
			Expect(notReadyReasons).To(HaveLen(2))
			Expect(notReadyReasons).To(ContainElement("component-2 is not ready"))
			Expect(notReadyReasons).To(ContainElement("component-3 is not ready"))
		})

		It("should handle empty sub-CR list", func() {
			subCRStatuses := []SubCRStatus{}

			allReady, notReadyReasons := AggregateReadiness(subCRStatuses)

			Expect(allReady).To(BeTrue())
			Expect(notReadyReasons).To(BeEmpty())
		})
	})

	Describe("SetAggregatedReadyCondition", func() {
		var testObject *konfluxv1alpha1.KonfluxRBAC

		BeforeEach(func() {
			testObject = &konfluxv1alpha1.KonfluxRBAC{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-rbac",
					Namespace:  "default",
					Generation: 1,
				},
			}
		})

		It("should set Ready condition when all components are ready", func() {
			subCRStatuses := []SubCRStatus{
				{Name: "component-1", Ready: true},
				{Name: "component-2", Ready: true},
			}

			SetAggregatedReadyCondition(testObject, "Ready", subCRStatuses)

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), "Ready")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal("AllComponentsReady"))
			Expect(condition.Message).To(Equal("All 2 components are ready"))
		})

		It("should set NotReady condition when some components are not ready", func() {
			subCRStatuses := []SubCRStatus{
				{Name: "component-1", Ready: true},
				{Name: "component-2", Ready: false},
			}

			SetAggregatedReadyCondition(testObject, "Ready", subCRStatuses)

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), "Ready")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("ComponentsNotReady"))
			Expect(condition.Message).To(ContainSubstring("component-2 is not ready"))
		})
	})

	Describe("SetFailedCondition", func() {
		var testObject *konfluxv1alpha1.KonfluxRBAC

		BeforeEach(func() {
			testObject = &konfluxv1alpha1.KonfluxRBAC{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-rbac",
					Namespace:  "default",
					Generation: 1,
				},
			}
		})

		It("should set a failed condition with error message", func() {
			testErr := fmt.Errorf("something went wrong")

			SetFailedCondition(testObject, "Ready", "ReconciliationFailed", testErr)

			condition := apimeta.FindStatusCondition(testObject.GetConditions(), "Ready")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal("ReconciliationFailed"))
			Expect(condition.Message).To(Equal("something went wrong"))
		})
	})
})
