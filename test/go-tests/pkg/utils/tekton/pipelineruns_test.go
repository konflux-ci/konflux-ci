package tekton

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetFailedPipelineRunDetails(t *testing.T) {
	s := runtime.NewScheme()
	utilruntime.Must(pipeline.AddToScheme(s))

	tests := []struct {
		name            string
		conditionStatus corev1.ConditionStatus
		conditionReason string
		stepTerminated  *corev1.ContainerStateTerminated
		expectContainer string
	}{
		{
			name:            "reason Failed with terminated Error step",
			conditionStatus: corev1.ConditionFalse,
			conditionReason: "Failed",
			stepTerminated: &corev1.ContainerStateTerminated{
				Reason:   "Error",
				ExitCode: 1,
			},
			expectContainer: "step-test-output",
		},
		{
			name:            "reason StepFailed with terminated Error step",
			conditionStatus: corev1.ConditionFalse,
			conditionReason: "StepFailed",
			stepTerminated: &corev1.ContainerStateTerminated{
				Reason:   "Error",
				ExitCode: 1,
			},
			expectContainer: "step-test-output",
		},
		{
			name:            "reason Succeeded has no failed container",
			conditionStatus: corev1.ConditionTrue,
			conditionReason: "Succeeded",
			stepTerminated:  nil,
			expectContainer: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			taskRun := &pipeline.TaskRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-taskrun",
					Namespace: "default",
				},
				Status: pipeline.TaskRunStatus{
					Status: duckv1.Status{
						Conditions: duckv1.Conditions{
							{
								Type:   apis.ConditionSucceeded,
								Status: tc.conditionStatus,
								Reason: tc.conditionReason,
							},
						},
					},
					TaskRunStatusFields: pipeline.TaskRunStatusFields{
						PodName: "test-pod",
					},
				},
			}

			if tc.stepTerminated != nil {
				taskRun.Status.Steps = []pipeline.StepState{
					{
						Name:      "test-output",
						Container: "step-test-output",
						ContainerState: corev1.ContainerState{
							Terminated: tc.stepTerminated,
						},
					},
				}
			}

			pipelineRun := &pipeline.PipelineRun{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pipelinerun",
					Namespace: "default",
				},
				Status: pipeline.PipelineRunStatus{
					PipelineRunStatusFields: pipeline.PipelineRunStatusFields{
						ChildReferences: []pipeline.ChildStatusReference{
							{
								Name:             "test-taskrun",
								PipelineTaskName: "build",
							},
						},
					},
				},
			}

			client := fake.NewClientBuilder().WithScheme(s).WithObjects(taskRun).Build()

			details, err := GetFailedPipelineRunDetails(client, pipelineRun)
			require.NoError(t, err)

			assert.Equal(t, tc.expectContainer, details.FailedContainerName)
			if tc.expectContainer != "" {
				assert.Equal(t, "test-taskrun", details.FailedTaskRunName)
				assert.Equal(t, "test-pod", details.PodName)
			} else {
				assert.Contains(t, details.TaskRunConditionsText,
					tc.conditionReason, "condition text should contain the reason")
			}
		})
	}
}

func TestIsTransientPipelineRunFailure(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		message  string
		expected bool
	}{
		{
			name:     "CouldntGetTask reason",
			reason:   "CouldntGetTask",
			expected: true,
		},
		{
			name:     "CouldntGetPipeline reason",
			reason:   "CouldntGetPipeline",
			message:  "Error retrieving pipeline for pipelinerun: resolver failed",
			expected: true,
		},
		{
			name:     "TaskRunImagePullFailed reason",
			reason:   "TaskRunImagePullFailed",
			expected: true,
		},
		{
			name:     "TaskRunImagePullFailed in message",
			reason:   "Failed",
			message:  "task failed: TaskRunImagePullFailed",
			expected: true,
		},
		{
			name:     "unexpected EOF in message",
			reason:   "Failed",
			message:  "connection error: unexpected EOF",
			expected: true,
		},
		{
			name:     "resolution timeout in message",
			reason:   "Failed",
			message:  "resolution took longer than global timeout of 1m0s",
			expected: true,
		},
		{
			name:     "generic Failed reason is not transient",
			reason:   "Failed",
			message:  "task X failed with exit code 1",
			expected: false,
		},
		{
			name:     "empty reason is not transient",
			reason:   "",
			expected: false,
		},
		{
			name:     "Succeeded is not transient",
			reason:   "Succeeded",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsTransientPipelineRunFailure(tc.reason, tc.message)
			assert.Equal(t, tc.expected, got)
		})
	}
}
