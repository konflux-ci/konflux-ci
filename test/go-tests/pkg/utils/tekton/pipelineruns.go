package tekton

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/client-go/kubernetes"
	"knative.dev/pkg/apis"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"

	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const sslCertDir = "/var/run/secrets/kubernetes.io/serviceaccount"

type PipelineRunGenerator interface {
	Generate() (*pipeline.PipelineRun, error)
}

type BuildahDemo struct {
	Image     string
	Bundle    string
	Name      string
	Namespace string
}

type ECIntegrationTestScenario struct {
	Image                       string
	Name                        string
	Namespace                   string
	PipelineGitURL              string
	PipelineGitRevision         string
	PipelineGitPathInRepo       string
	PipelinePolicyConfiguration string
}

type FailedPipelineRunDetails struct {
	FailedTaskRunName   string
	PodName             string
	FailedContainerName string
	// TaskRunConditionsText is built while walking child TaskRuns; used when FailedContainerName is empty.
	TaskRunConditionsText string
}

// This is a demo pipeline to create test image and task signing
func (b BuildahDemo) Generate() (*pipeline.PipelineRun, error) {
	return &pipeline.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      b.Name,
			Namespace: b.Namespace,
		},
		Spec: pipeline.PipelineRunSpec{
			Params: []pipeline.Param{
				{
					Name:  "dockerfile",
					Value: *pipeline.NewStructuredValues("Containerfile"),
				},
				{
					Name:  "output-image",
					Value: *pipeline.NewStructuredValues(b.Image),
				},
				{
					Name:  "git-url",
					Value: *pipeline.NewStructuredValues("https://github.com/conforma/golden-container.git"),
				},
				{
					Name:  "skip-checks",
					Value: *pipeline.NewStructuredValues("true"),
				},
			},
			PipelineRef: NewBundleResolverPipelineRef("docker-build", b.Bundle),
			Workspaces: []pipeline.WorkspaceBinding{
				{
					Name: "workspace",
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: "app-studio-default-workspace",
					},
				},
			},
			TaskRunTemplate: pipeline.PipelineTaskRunTemplate{
				ServiceAccountName: constants.DefaultPipelineServiceAccount,
			},
		},
	}, nil
}

// Generates pipelineRun from VerifyEnterpriseContract.
func (p VerifyEnterpriseContract) Generate() (*pipeline.PipelineRun, error) {
	var applicationSnapshotJSON, err = json.Marshal(p.Snapshot)
	if err != nil {
		return nil, err
	}
	return &pipeline.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-run-", p.Name),
			Namespace:    p.Namespace,
			Labels: map[string]string{
				"appstudio.openshift.io/application": p.Snapshot.Application,
			},
		},
		Spec: pipeline.PipelineRunSpec{
			PipelineSpec: &pipeline.PipelineSpec{
				Tasks: []pipeline.PipelineTask{
					{
						Name: "verify-enterprise-contract",
						Params: []pipeline.Param{
							{
								Name: "IMAGES",
								Value: pipeline.ParamValue{
									Type:      pipeline.ParamTypeString,
									StringVal: string(applicationSnapshotJSON),
								},
							},
							{
								Name: "POLICY_CONFIGURATION",
								Value: pipeline.ParamValue{
									Type:      pipeline.ParamTypeString,
									StringVal: p.PolicyConfiguration,
								},
							},
							{
								Name: "PUBLIC_KEY",
								Value: pipeline.ParamValue{
									Type:      pipeline.ParamTypeString,
									StringVal: p.PublicKey,
								},
							},
							{
								Name: "SSL_CERT_DIR",
								Value: pipeline.ParamValue{
									Type:      pipeline.ParamTypeString,
									StringVal: sslCertDir,
								},
							},
							{
								Name: "STRICT",
								Value: pipeline.ParamValue{
									Type:      pipeline.ParamTypeString,
									StringVal: strconv.FormatBool(p.Strict),
								},
							},
							{
								Name: "EFFECTIVE_TIME",
								Value: pipeline.ParamValue{
									Type:      pipeline.ParamTypeString,
									StringVal: p.EffectiveTime,
								},
							},
							{
								Name: "IGNORE_REKOR",
								Value: pipeline.ParamValue{
									Type:      pipeline.ParamTypeString,
									StringVal: strconv.FormatBool(p.IgnoreRekor),
								},
							},
						},
						TaskRef: &pipeline.TaskRef{
							ResolverRef: pipeline.ResolverRef{
								Resolver: "bundles",
								Params: []pipeline.Param{
									{Name: "name", Value: pipeline.ParamValue{StringVal: "verify-enterprise-contract", Type: pipeline.ParamTypeString}},
									{Name: "bundle", Value: pipeline.ParamValue{StringVal: p.TaskBundle, Type: pipeline.ParamTypeString}},
									{Name: "kind", Value: pipeline.ParamValue{StringVal: "task", Type: pipeline.ParamTypeString}},
								},
							},
						},
					},
				},
			},
			TaskRunTemplate: pipeline.PipelineTaskRunTemplate{
				ServiceAccountName: constants.DefaultPipelineServiceAccount,
			},
		},
	}, nil
}

// Generates pipelineRun from ECIntegrationTestScenario.
func (p ECIntegrationTestScenario) Generate() (*pipeline.PipelineRun, error) {

	snapshot := `{"components": [
		{"containerImage": "` + p.Image + `"}
	]}`

	return &pipeline.PipelineRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "ec-integration-test-scenario-run-",
			Namespace:    p.Namespace,
		},
		Spec: pipeline.PipelineRunSpec{
			PipelineRef: &pipeline.PipelineRef{
				ResolverRef: pipeline.ResolverRef{
					Resolver: "git",
					Params: []pipeline.Param{
						{Name: "url", Value: *pipeline.NewStructuredValues(p.PipelineGitURL)},
						{Name: "revision", Value: *pipeline.NewStructuredValues(p.PipelineGitRevision)},
						{Name: "pathInRepo", Value: *pipeline.NewStructuredValues(p.PipelineGitPathInRepo)},
						{Name: "policyConfiguration", Value: *pipeline.NewStructuredValues(p.PipelinePolicyConfiguration)},
					},
				},
			},
			Params: []pipeline.Param{
				{Name: "SNAPSHOT", Value: *pipeline.NewStructuredValues(snapshot)},
				{Name: "POLICY_CONFIGURATION", Value: *pipeline.NewStructuredValues(p.PipelinePolicyConfiguration)},
			},
			TaskRunTemplate: pipeline.PipelineTaskRunTemplate{
				ServiceAccountName: constants.DefaultPipelineServiceAccount,
			},
		},
	}, nil
}

// GetFailedPipelineRunLogs gets the logs of the pipelinerun failed task
func GetFailedPipelineRunLogs(c crclient.Client, ki kubernetes.Interface, pipelineRun *pipeline.PipelineRun) (string, error) {
	var d *FailedPipelineRunDetails
	var err error

	failMessage := fmt.Sprintf("Pipelinerun '%s' didn't succeed\n", pipelineRun.Name)

	for _, cond := range pipelineRun.Status.Conditions {
		if cond.Reason == "CouldntGetPipeline" {
			failMessage += fmt.Sprintf("CouldntGetPipeline message: %s", cond.Message)
		}
	}
	if d, err = GetFailedPipelineRunDetails(c, pipelineRun); err != nil {
		return "", err
	}

	if d != nil && d.FailedContainerName != "" {
		logs, err := utils.GetContainerLogs(ki, d.PodName, d.FailedContainerName, pipelineRun.Namespace)

		switch {
		// Sometimes the log of failed container can't be caught in time, it's to avoid panic
		case logs != "":
			// Adding the FailedTaskRunName can help to know which task the container belongs to
			failMessage += fmt.Sprintf("Logs from failed container '%s/%s': \n%s",
				d.FailedTaskRunName, d.FailedContainerName, logs)
		case err != nil:
			failMessage += fmt.Sprintf("Failed to get logs for container '%s/%s': %v",
				d.FailedTaskRunName, d.FailedContainerName, err)
		default:
			failMessage += fmt.Sprintf("Failed container '%s/%s' (no logs available)",
				d.FailedTaskRunName, d.FailedContainerName)
		}
	} else if d != nil && d.FailedContainerName == "" && d.TaskRunConditionsText != "" {
		failMessage += "TaskRun status.conditions (no failed container logs available):\n" + d.TaskRunConditionsText
	}
	return failMessage, nil
}

func HasPipelineRunSucceeded(pr *pipeline.PipelineRun) bool {
	return pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded).IsTrue()
}

func HasPipelineRunFailed(pr *pipeline.PipelineRun) bool {
	return pr.IsDone() && pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded).IsFalse()
}

func GetFailedPipelineRunDetails(c crclient.Client, pipelineRun *pipeline.PipelineRun) (*FailedPipelineRunDetails, error) {
	d := &FailedPipelineRunDetails{}
	var condText string
	for _, chr := range pipelineRun.Status.ChildReferences {
		taskRun := &pipeline.TaskRun{}
		taskRunKey := types.NamespacedName{Namespace: pipelineRun.Namespace, Name: chr.Name}
		if err := c.Get(context.Background(), taskRunKey, taskRun); err != nil {
			return nil, fmt.Errorf("failed to get details for PR %s: %+v", pipelineRun.GetName(), err)
		}
		condText += fmt.Sprintf("- TaskRun %s (pipeline task %q):\n", taskRun.Name, chr.PipelineTaskName)
		for _, tc := range taskRun.Status.Conditions {
			if tc.Reason == "Failed" {
				d.FailedTaskRunName = taskRun.Name
				d.PodName = taskRun.Status.PodName
				for _, s := range taskRun.Status.Steps {
					if s.Terminated != nil && (s.Terminated.Reason == "Error" || strings.Contains(s.Terminated.Reason, "Failed")) {
						d.FailedContainerName = s.Container
						return d, nil
					}
				}
			}
			condText += fmt.Sprintf("  type=%s status=%s reason=%s message=%s\n",
				tc.Type, tc.Status, tc.Reason, tc.Message)
		}
	}
	d.TaskRunConditionsText = condText
	return d, nil
}
