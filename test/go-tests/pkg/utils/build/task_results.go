package build

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/oras"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"k8s.io/apimachinery/pkg/types"

	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var taskNames = []string{"clair-scan", "clamav-scan", "deprecated-base-image-check", "validate-fbc"}

// tasksGatedBySkipChecks are pipeline tasks that run only when params.skip-checks is "false".
// When skip-checks is "true", Tekton skips them and they have no TaskRun in ChildReferences.
var tasksGatedBySkipChecks = map[string]bool{
	"clair-scan":                   true,
	"clamav-scan":                  true,
	"deprecated-base-image-check":  true,
}

type TestOutput struct {
	Result    string `json:"result"`
	Timestamp string `json:"timestamp"`
	Note      string `json:"note"`
	Namespace string `json:"namespace"`
	Successes int    `json:"successes"`
	Failures  int    `json:"failures"`
	Warnings  int    `json:"warnings"`
}

type ClairScanResult struct {
	Vulnerabilities Vulnerabilities `json:"vulnerabilities"`
}

type ClairScanReports map[string]string

type Vulnerabilities struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
}

// isSkipChecksEnabled returns true if the PipelineRun has skip-checks set to "true".
// When true, the pipeline skips security/check tasks (clair-scan, clamav-scan, deprecated-base-image-check).
// It checks spec.params first (runtime value), then status.pipelineSpec.params default.
func isSkipChecksEnabled(pr *pipeline.PipelineRun) bool {
	for _, p := range pr.Spec.Params {
		if p.Name == "skip-checks" {
			return strings.EqualFold(strings.TrimSpace(p.Value.StringVal), "true")
		}
	}
	if pr.Status.PipelineSpec != nil {
		for _, p := range pr.Status.PipelineSpec.Params {
			if p.Name == "skip-checks" {
				return strings.EqualFold(strings.TrimSpace(p.Default.StringVal), "true")
			}
		}
	}
	return false
}

func ValidateBuildPipelineTestResults(pipelineRun *pipeline.PipelineRun, c crclient.Client, isFBCBuild bool) error {
	var imageURL string
	for _, result := range pipelineRun.Status.Results {
		if result.Name == "IMAGE_URL" {
			imageURL = strings.TrimSpace(result.Value.StringVal)
			break
		}
	}
	if imageURL == "" {
		return fmt.Errorf("unable to find IMAGE_URL result from PipelineRun %s", pipelineRun.Name)
	}

	skipChecks := isSkipChecksEnabled(pipelineRun)
	for _, taskName := range taskNames {
		// The validate-fbc task is only required for FBC pipelines which we can infer by the component name
		if !isFBCBuild && taskName == "validate-fbc" {
			continue
		}
		if isFBCBuild && (taskName == "clair-scan" || taskName == "clamav-scan") {
			continue
		}
		// When skip-checks is true, Tekton skips the check tasks; they have no TaskRun. Skip validating them.
		if skipChecks && tasksGatedBySkipChecks[taskName] {
			continue
		}
		results, err := fetchTaskRunResults(c, pipelineRun, taskName)
		if err != nil {
			return err
		}

		resultsToValidate := []string{constants.TektonTaskTestOutputName}

		switch taskName {
		case "clair-scan":
			resultsToValidate = append(resultsToValidate, "SCAN_OUTPUT")
			resultsToValidate = append(resultsToValidate, "REPORTS")
		case "deprecated-image-check":
			resultsToValidate = append(resultsToValidate, "PYXIS_HTTP_CODE")
		}

		if err := validateTaskRunResult(imageURL, results, resultsToValidate, taskName); err != nil {
			return err
		}

	}
	return nil
}

func fetchTaskRunResults(c crclient.Client, pr *pipeline.PipelineRun, pipelineTaskName string) ([]pipeline.TaskRunResult, error) {
	for _, chr := range pr.Status.ChildReferences {
		if chr.PipelineTaskName != pipelineTaskName {
			continue
		}
		taskRun := &pipeline.TaskRun{}
		taskRunKey := types.NamespacedName{Namespace: pr.Namespace, Name: chr.Name}
		if err := c.Get(context.Background(), taskRunKey, taskRun); err != nil {
			return nil, err
		}
		return taskRun.Status.Results, nil
	}
	return nil, fmt.Errorf(
		"pipelineTaskName %q not found in PipelineRun %s/%s", pipelineTaskName, pr.GetName(), pr.GetNamespace())
}

func validateTaskRunResult(imageURL string, trResults []pipeline.TaskRunResult, expectedResultNames []string, taskName string) error {
	for _, rn := range expectedResultNames {
		found := false
		for _, r := range trResults {
			if rn == r.Name {
				found = true
				switch r.Name {
				case constants.TektonTaskTestOutputName:
					var testOutput = &TestOutput{}
					err := json.Unmarshal([]byte(r.Value.StringVal), &testOutput)
					if err != nil {
						return fmt.Errorf("cannot parse %q result: %+v", constants.TektonTaskTestOutputName, err)
					}
				case "SCAN_OUTPUT":
					var testOutput = &ClairScanResult{}
					err := json.Unmarshal([]byte(r.Value.StringVal), &testOutput)
					if err != nil {
						return fmt.Errorf("cannot parse SCAN_OUTPUT result: %+v", err)
					}
				case "REPORTS":
					var reports = ClairScanReports{}
					err := json.Unmarshal([]byte(r.Value.StringVal), &reports)
					if err != nil {
						return fmt.Errorf("cannot parse REPORTS result: %w", err)
					}
					for _, reportDigest := range reports {
						reportRef := fmt.Sprintf("%s@%s", imageURL, reportDigest)

						imageDir, err := oras.PullArtifacts(reportRef)
						if err != nil {
							return fmt.Errorf("cannot fetch report from ref %s: %w", reportRef, err)
						}

						var hasNonEmptyReport bool
						if err := filepath.Walk(imageDir, func(p string, info fs.FileInfo, err error) error {
							if err != nil {
								return err
							}
							if info.IsDir() {
								return nil
							}
							if info.Size() == 0 {
								return fmt.Errorf("report %s from %s is empty", p, reportRef)
							}
							hasNonEmptyReport = true
							return nil
						}); err != nil {
							return err
						}

						if !hasNonEmptyReport {
							return fmt.Errorf("no report files were found in %s", reportRef)
						}
					}
				case "PYXIS_HTTP_CODE", "BASE_IMAGE", "BASE_IMAGE_REPOSITORY":
					if len(r.Value.StringVal) < 1 {
						return fmt.Errorf("value of %q result is empty", r.Name)
					}
				}
			}
		}
		if !found {
			return fmt.Errorf("expected result name %q not found in Task %q result", rn, taskName)
		}
	}
	return nil
}
