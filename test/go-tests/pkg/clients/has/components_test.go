package has

import "testing"

func TestBuildPipelineRunIsTransient(t *testing.T) {
	tests := []struct {
		name     string
		reason   string
		message  string
		expected bool
	}{
		{
			name:     "CouldntGetTask is transient",
			reason:   "CouldntGetTask",
			expected: true,
		},
		{
			name:     "CouldntGetPipeline is transient",
			reason:   "CouldntGetPipeline",
			message:  "Error retrieving pipeline for pipelinerun: resolver failed",
			expected: true,
		},
		{
			name:     "TaskRunImagePullFailed is transient",
			reason:   "TaskRunImagePullFailed",
			expected: true,
		},
		{
			name:     "resolution timeout in message is transient",
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
			got := buildPipelineRunIsTransient(tc.reason, tc.message)
			if got != tc.expected {
				t.Errorf("buildPipelineRunIsTransient(%q, %q) = %v, want %v",
					tc.reason, tc.message, got, tc.expected)
			}
		})
	}
}
