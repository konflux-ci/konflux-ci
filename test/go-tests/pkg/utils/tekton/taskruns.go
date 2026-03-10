package tekton

import (
	"knative.dev/pkg/apis"

	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
)

// DidTaskRunSucceed checks if task succeeded.
func DidTaskRunSucceed(tr interface{}) bool {
	switch tr := tr.(type) {
	case *pipeline.PipelineRunTaskRunStatus:
		return tr.Status.GetCondition(apis.ConditionSucceeded).IsTrue()
	case *pipeline.TaskRunStatus:
		return tr.Status.GetCondition(apis.ConditionSucceeded).IsTrue()
	}
	return false
}
