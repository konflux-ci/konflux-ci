package tekton

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	tektonpipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"k8s.io/client-go/util/jsonpath"
	"knative.dev/pkg/apis"
)

type TaskRunResultMatcher struct {
	name        string
	jsonPath    *string
	value       *string
	jsonValue   *interface{}
	jsonMatcher types.GomegaMatcher
}

// FailureMessage returns failure message for a TaskRunResult matcher.
func (matcher *TaskRunResultMatcher) FailureMessage(actual interface{}) (message string) {
	if matcher.value != nil {
		return fmt.Sprintf("%v to equal %v", actual, tektonpipeline.TaskRunResult{
			Name:  matcher.name,
			Value: *tektonpipeline.NewStructuredValues(*matcher.value),
		})
	}

	return matcher.jsonMatcher.FailureMessage(actual)
}

// Match matches the matcher with a given taskRun.
func (matcher *TaskRunResultMatcher) Match(actual interface{}) (success bool, err error) {
	if tr, ok := actual.(tektonpipeline.TaskRunResult); !ok {
		return false, fmt.Errorf("not given TaskRunResult")
	} else {
		if tr.Name != matcher.name {
			return false, nil
		}

		given := tr.Value
		if matcher.jsonPath != nil {
			p := jsonpath.New("test")
			p.EnableJSONOutput(true)
			if err := p.Parse(*matcher.jsonPath); err != nil {
				return false, err
			}

			var v interface{}
			if err := json.Unmarshal([]byte(given.StringVal), &v); err != nil {
				return false, err
			}

			results, err := p.FindResults(v)
			if err != nil {
				return false, err
			}
			var values []interface{}
			for _, result := range results {
				var buffy bytes.Buffer
				if err := p.PrintResults(&buffy, result); err != nil {
					return false, err
				}

				var value interface{}
				if err := json.Unmarshal(buffy.Bytes(), &value); err != nil {
					return false, err
				}
				values = append(values, value)
			}
			if len(values) == 1 {
				if b, err := json.Marshal(values[0]); err != nil {
					return false, err
				} else {
					given = *tektonpipeline.NewStructuredValues(string(b))
				}
			} else if b, err := json.Marshal(values); err != nil {
				return false, err
			} else {
				given = *tektonpipeline.NewStructuredValues(string(b))
			}
		}

		if matcher.value != nil {
			return strings.TrimSpace(given.StringVal) == *matcher.value, nil
		} else {
			matcher.jsonMatcher = gomega.MatchJSON(*matcher.jsonValue)
			return matcher.jsonMatcher.Match(given.StringVal)
		}
	}
}

// NegatedFailureMessage returns negated failure message for a TaskRunResult matcher.
func (matcher *TaskRunResultMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	if matcher.jsonPath != nil && matcher.jsonValue != nil {
		return fmt.Sprintf("value `%s` for JSONPath `%s` not to equal `%s`", actual, *matcher.jsonPath, *matcher.jsonValue)
	}
	if matcher.value != nil {
		return fmt.Sprintf("%v not to equal %v", actual, tektonpipeline.TaskRunResult{
			Name:  matcher.name,
			Value: *tektonpipeline.NewStructuredValues(strings.TrimSpace(*matcher.value)),
		})
	}

	return matcher.jsonMatcher.NegatedFailureMessage(actual)
}

// MatchTaskRunResult returns a taskRunResult matcher.
func MatchTaskRunResult(name, value string) types.GomegaMatcher {
	return &TaskRunResultMatcher{name: name, value: &value}
}

// MatchTaskRunResultWithJSONValue returns a taskRunResult with a JSONValue matcher.
func MatchTaskRunResultWithJSONValue(name string, json interface{}) types.GomegaMatcher {
	return &TaskRunResultMatcher{name: name, jsonValue: &json}
}

// MatchTaskRunResultWithJSONPathValue returns a taskRunResult with a JSONPath and JSONvalue matcher.
func MatchTaskRunResultWithJSONPathValue(name, path string, json interface{}) types.GomegaMatcher {
	return &TaskRunResultMatcher{name: name, jsonPath: &path, jsonValue: &json}
}

func DidTaskSucceed(tr interface{}) bool {
	switch tr := tr.(type) {
	case *tektonpipeline.PipelineRunTaskRunStatus:
		return tr.Status.GetCondition(apis.ConditionSucceeded).IsTrue()
	case *tektonpipeline.TaskRunStatus:
		return tr.Status.GetCondition(apis.ConditionSucceeded).IsTrue()
	}
	return false
}
