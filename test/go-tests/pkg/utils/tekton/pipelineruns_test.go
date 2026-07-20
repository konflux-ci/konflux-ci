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
		conditionReason string
		stepTerminated  *corev1.ContainerStateTerminated
		expectContainer string
	}{
		{
			name:            "reason Failed with terminated Error step",
			conditionReason: "Failed",
			stepTerminated: &corev1.ContainerStateTerminated{
				Reason:   "Error",
				ExitCode: 1,
			},
			expectContainer: "step-test-output",
		},
		{
			name:            "reason StepFailed with terminated Error step",
			conditionReason: "StepFailed",
			stepTerminated: &corev1.ContainerStateTerminated{
				Reason:   "Error",
				ExitCode: 1,
			},
			expectContainer: "step-test-output",
		},
		{
			name:            "reason Succeeded has no failed container",
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
								Status: corev1.ConditionFalse,
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
			if tc.expectContainer == "" {
				assert.Contains(t, details.TaskRunConditionsText,
					tc.conditionReason, "condition text should contain the reason")
			}
		})
	}
}
