package release

import (
	"testing"

	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

func TestPipelineRunHasTransientFailure(t *testing.T) {
	tests := []struct {
		name     string
		pr       *pipeline.PipelineRun
		expected bool
	}{
		{
			name:     "nil condition returns false",
			pr:       &pipeline.PipelineRun{},
			expected: false,
		},
		{
			name:     "TaskRunImagePullFailed reason",
			pr:       prWithCondition("TaskRunImagePullFailed", ""),
			expected: true,
		},
		{
			name:     "CouldntGetPipeline reason",
			pr:       prWithCondition("CouldntGetPipeline", "Error retrieving pipeline for pipelinerun: resolver failed"),
			expected: true,
		},
		{
			name:     "CouldntGetTask reason",
			pr:       prWithCondition("CouldntGetTask", "Error retrieving task for taskrun"),
			expected: true,
		},
		{
			name:     "TaskRunImagePullFailed in message",
			pr:       prWithCondition("Failed", "task failed: TaskRunImagePullFailed"),
			expected: true,
		},
		{
			name:     "unexpected EOF in message",
			pr:       prWithCondition("Failed", "connection error: unexpected EOF"),
			expected: true,
		},
		{
			name:     "resolution timeout in message",
			pr:       prWithCondition("Failed", "resolution took longer than global timeout of 1m0s"),
			expected: true,
		},
		{
			name:     "generic Failed reason is not transient",
			pr:       prWithCondition("Failed", "task X failed with exit code 1"),
			expected: false,
		},
		{
			name:     "Succeeded condition is not transient",
			pr:       prWithSucceededCondition(),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Pass nil client — no ChildReferences so client.Get is never called.
			got := pipelineRunHasTransientFailure(tc.pr, nil)
			if got != tc.expected {
				t.Errorf("pipelineRunHasTransientFailure() = %v, want %v", got, tc.expected)
			}
		})
	}
}

// prWithCondition builds a PipelineRun whose Succeeded condition has the
// given reason and message (status False).
func prWithCondition(reason, message string) *pipeline.PipelineRun {
	pr := &pipeline.PipelineRun{}
	pr.Status.Status = duckv1.Status{
		Conditions: duckv1.Conditions{
			{
				Type:    apis.ConditionSucceeded,
				Status:  corev1.ConditionFalse,
				Reason:  reason,
				Message: message,
			},
		},
	}
	return pr
}

// prWithSucceededCondition builds a PipelineRun that succeeded (status True,
// reason "Succeeded"). This must not be considered a transient failure.
func prWithSucceededCondition() *pipeline.PipelineRun {
	pr := &pipeline.PipelineRun{}
	pr.Status.Status = duckv1.Status{
		Conditions: duckv1.Conditions{
			{
				Type:   apis.ConditionSucceeded,
				Status: corev1.ConditionTrue,
				Reason: "Succeeded",
			},
		},
	}
	return pr
}
