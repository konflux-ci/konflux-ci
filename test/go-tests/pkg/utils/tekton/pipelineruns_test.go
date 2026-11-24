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

func TestFormatPipelineRunCondition(t *testing.T) {
	pr := &pipeline.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{Name: "build-run", Namespace: "default-tenant"},
		Status: pipeline.PipelineRunStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{
					{
						Type:    apis.ConditionSucceeded,
						Status:  corev1.ConditionFalse,
						Reason:  "Failed",
						Message: `"build-container" failed: step step-build exited with code 1`,
					},
				},
			},
		},
	}

	got := FormatPipelineRunCondition(pr)
	assert.Contains(t, got, "PipelineRun default-tenant/build-run")
	assert.Contains(t, got, "status=False")
	assert.Contains(t, got, "reason=Failed")
	assert.Contains(t, got, `"build-container" failed: step step-build exited with code 1`)
}

func TestFormatChildTaskStatus(t *testing.T) {
	t.Run("get error still names the task", func(t *testing.T) {
		got := FormatChildTaskStatus("build-container", "build-run-build-container", nil, assert.AnError)
		assert.Contains(t, got, "task=build-container")
		assert.Contains(t, got, "TaskRun=build-run-build-container")
		assert.Contains(t, got, "failed to get TaskRun")
	})

	t.Run("failed task with no pod keeps the condition message", func(t *testing.T) {
		tr := &pipeline.TaskRun{
			ObjectMeta: metav1.ObjectMeta{Name: "build-run-tpa-scan"},
			Status: pipeline.TaskRunStatus{
				Status: duckv1.Status{
					Conditions: duckv1.Conditions{
						{
							Type:    apis.ConditionSucceeded,
							Status:  corev1.ConditionFalse,
							Reason:  "CouldntGetTask",
							Message: "resolution took longer than global timeout",
						},
					},
				},
			},
		}
		got := FormatChildTaskStatus("tpa-scan", tr.Name, tr, nil)
		assert.Contains(t, got, "task=tpa-scan")
		assert.Contains(t, got, "pod=(none)")
		assert.Contains(t, got, "reason=CouldntGetTask")
		assert.Contains(t, got, "message=resolution took longer than global timeout")
	})

	t.Run("running step is named", func(t *testing.T) {
		tr := &pipeline.TaskRun{
			ObjectMeta: metav1.ObjectMeta{Name: "build-run-build-container"},
			Status: pipeline.TaskRunStatus{
				Status: duckv1.Status{
					Conditions: duckv1.Conditions{
						{
							Type:   apis.ConditionSucceeded,
							Status: corev1.ConditionUnknown,
							Reason: "Running",
						},
					},
				},
				TaskRunStatusFields: pipeline.TaskRunStatusFields{
					PodName: "build-pod",
					Steps: []pipeline.StepState{
						{
							Name:      "build",
							Container: "step-build",
							ContainerState: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{},
							},
						},
					},
				},
			},
		}
		got := FormatChildTaskStatus("build-container", tr.Name, tr, nil)
		assert.Contains(t, got, "pod=build-pod")
		assert.Contains(t, got, "reason=Running")
		assert.Contains(t, got, "step build container=step-build running")
	})

	t.Run("successful step omits the result message", func(t *testing.T) {
		tr := &pipeline.TaskRun{
			ObjectMeta: metav1.ObjectMeta{Name: "build-run-clone"},
			Status: pipeline.TaskRunStatus{
				TaskRunStatusFields: pipeline.TaskRunStatusFields{
					PodName: "clone-pod",
					Steps: []pipeline.StepState{
						{
							Name:      "clone",
							Container: "step-clone",
							ContainerState: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 0,
									Reason:   "Completed",
									Message:  `[{"key":"commit","value":"abc","type":1}]`,
								},
							},
						},
					},
				},
			},
		}
		got := FormatChildTaskStatus("clone-repository", tr.Name, tr, nil)
		assert.Contains(t, got, "step clone container=step-clone terminated reason=Completed exitCode=0\n")
		assert.NotContains(t, got, "commit")
		assert.NotContains(t, got, `"key"`)
	})

	t.Run("failed step keeps its message", func(t *testing.T) {
		tr := &pipeline.TaskRun{
			ObjectMeta: metav1.ObjectMeta{Name: "build-run-build"},
			Status: pipeline.TaskRunStatus{
				TaskRunStatusFields: pipeline.TaskRunStatusFields{
					Steps: []pipeline.StepState{
						{
							Name:      "build",
							Container: "step-build",
							ContainerState: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 1,
									Reason:   "Error",
									Message:  "dockerfile not found",
								},
							},
						},
					},
				},
			},
		}
		got := FormatChildTaskStatus("build-container", tr.Name, tr, nil)
		assert.Contains(t, got, "exitCode=1")
		assert.Contains(t, got, "message=dockerfile not found")
	})
}

func TestStatusShowsPullImageFailure(t *testing.T) {
	assert.True(t, StatusShowsPullImageFailure("PullImageFailed", ""))
	assert.True(t, StatusShowsPullImageFailure("TaskRunImagePullFailed", ""))
	assert.True(t, StatusShowsPullImageFailure("Failed", `task failed: TaskRunImagePullFailed`))
	assert.False(t, StatusShowsPullImageFailure("Running", "Tasks Completed: 1"))

	tr := &pipeline.TaskRun{
		Status: pipeline.TaskRunStatus{
			Status: duckv1.Status{
				Conditions: duckv1.Conditions{{
					Type:   apis.ConditionSucceeded,
					Status: corev1.ConditionUnknown,
					Reason: "PullImageFailed",
				}},
			},
		},
	}
	assert.True(t, TaskRunShowsPullImageFailure(tr))
	assert.False(t, TaskRunShowsPullImageFailure(nil))
}

func TestFormatSkippedTasks(t *testing.T) {
	pr := &pipeline.PipelineRun{
		Status: pipeline.PipelineRunStatus{
			PipelineRunStatusFields: pipeline.PipelineRunStatusFields{
				SkippedTasks: []pipeline.SkippedTask{
					{Name: "tpa-scan", Reason: pipeline.WhenExpressionsSkip},
				},
			},
		},
	}
	got := FormatSkippedTasks(pr)
	assert.Contains(t, got, "task=tpa-scan")
	assert.Empty(t, FormatSkippedTasks(&pipeline.PipelineRun{}))
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
