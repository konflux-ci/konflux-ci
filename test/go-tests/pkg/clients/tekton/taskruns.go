package tekton

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/logs"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	g "github.com/onsi/ginkgo/v2"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/pod"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/yaml"
	pointer "k8s.io/utils/ptr"

	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateTaskRunCopy creates a TaskRun that copies one image to a second image repository.
func (t *TektonController) CreateTaskRunCopy(name, namespace, serviceAccountName, srcImageURL, destImageURL string) (*pipeline.TaskRun, error) {
	taskRun := pipeline.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: pipeline.TaskRunSpec{
			ServiceAccountName: serviceAccountName,
			TaskRef: &pipeline.TaskRef{
				Name: "skopeo-copy",
				Kind: pipeline.TaskKind("ClusterTask"),
			},
			Params: []pipeline.Param{
				{
					Name: "srcImageURL",
					Value: pipeline.ParamValue{
						StringVal: srcImageURL,
						Type:      pipeline.ParamTypeString,
					},
				},
				{
					Name: "destImageURL",
					Value: pipeline.ParamValue{
						StringVal: destImageURL,
						Type:      pipeline.ParamTypeString,
					},
				},
			},
			// workaround to avoid the error "container has runAsNonRoot and image will run as root"
			PodTemplate: &pod.Template{
				SecurityContext: &corev1.PodSecurityContext{
					RunAsNonRoot: pointer.To[bool](true),
					RunAsUser:    pointer.To[int64](65532),
				},
			},
			Workspaces: []pipeline.WorkspaceBinding{
				{
					Name:     "images-url",
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		},
	}

	err := t.KubeRest().Create(context.Background(), &taskRun)
	if err != nil {
		return nil, err
	}
	return &taskRun, nil
}

// GetTaskRun returns the requested TaskRun object.
func (t *TektonController) GetTaskRun(name, namespace string) (*pipeline.TaskRun, error) {
	namespacedName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}

	taskRun := pipeline.TaskRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := t.KubeRest().Get(context.Background(), namespacedName, &taskRun)
	if err != nil {
		return nil, err
	}
	return &taskRun, nil
}

// StoreTaskRun stores a given TaskRun as an artifact.
func (t *TektonController) StoreTaskRun(prefix string, taskRun *pipeline.TaskRun) error {
	artifacts := make(map[string][]byte)

	taskRunYaml, err := yaml.Marshal(taskRun)
	if err != nil {
		g.GinkgoWriter.Printf("failed to store taskRun %s:%s: %s\n", taskRun.GetNamespace(), taskRun.GetName(), err.Error())
	}
	artifacts["taskRun-"+taskRun.Name+".yaml"] = taskRunYaml

	if err := logs.StoreArtifacts(artifacts); err != nil {
		return err
	}

	return nil
}

func (t *TektonController) StoreTaskRunsForPipelineRun(c crclient.Client, pr *pipeline.PipelineRun) error {
	for _, chr := range pr.Status.ChildReferences {
		taskRun := &pipeline.TaskRun{}
		taskRunKey := types.NamespacedName{Namespace: pr.Namespace, Name: chr.Name}
		if err := c.Get(context.Background(), taskRunKey, taskRun); err != nil {
			return err
		}
		if err := t.StoreTaskRun(taskRun.Name, taskRun); err != nil{
			g.GinkgoWriter.Printf("an error happened during storing taskRun %s:%s: %s\n", taskRun.GetNamespace(), taskRun.GetName(), err.Error())
		}
	}
	return nil
}

// GetTaskRunLogs returns logs of a specified taskRun.
func (t *TektonController) GetTaskRunLogs(pipelineRunName, pipelineTaskName, namespace string) (map[string]string, error) {
	tektonClient := t.PipelineClient().TektonV1beta1().PipelineRuns(namespace)
	pipelineRun, err := tektonClient.Get(context.Background(), pipelineRunName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	podName := ""
	for _, childStatusReference := range pipelineRun.Status.ChildReferences {
		if childStatusReference.PipelineTaskName == pipelineTaskName {
			taskRun := &pipeline.TaskRun{}
			taskRunKey := types.NamespacedName{Namespace: pipelineRun.Namespace, Name: childStatusReference.Name}
			if err := t.KubeRest().Get(context.Background(), taskRunKey, taskRun); err != nil {
				return nil, err
			}
			podName = taskRun.Status.PodName
			break
		}
	}
	if podName == "" {
		return nil, fmt.Errorf("task with %s name doesn't exist in %s pipelinerun", pipelineTaskName, pipelineRunName)
	}

	podClient := t.KubeInterface().CoreV1().Pods(namespace)
	pod, err := podClient.Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	logs := make(map[string]string)
	for _, container := range pod.Spec.Containers {
		containerName := container.Name
		if containerLogs, err := t.fetchContainerLog(podName, containerName, namespace); err == nil {
			logs[containerName] = containerLogs
		} else {
			logs[containerName] = "failed to get logs"
		}
	}
	return logs, nil
}

func (t *TektonController) GetTaskRunFromPipelineRun(c crclient.Client, pr *pipeline.PipelineRun, pipelineTaskName string) (*pipeline.TaskRun, error) {
	for _, chr := range pr.Status.ChildReferences {
		if chr.PipelineTaskName != pipelineTaskName {
			continue
		}

		taskRun := &pipeline.TaskRun{}
		taskRunKey := types.NamespacedName{Namespace: pr.Namespace, Name: chr.Name}
		if err := c.Get(context.Background(), taskRunKey, taskRun); err != nil {
			return nil, err
		}
		return taskRun, nil
	}

	return nil, fmt.Errorf("task %q not found in PipelineRun %q/%q", pipelineTaskName, pr.Namespace, pr.Name)
}

func (t *TektonController) GetTaskRunResult(c crclient.Client, pr *pipeline.PipelineRun, pipelineTaskName string, result string) (string, error) {
	taskRun, err := t.GetTaskRunFromPipelineRun(c, pr, pipelineTaskName)
	if err != nil {
		return "", err
	}

	for _, trResult := range taskRun.Status.Results {
		if trResult.Name == result {
			// for some reason the result might contain \n suffix
			return strings.TrimSuffix(trResult.Value.StringVal, "\n"), nil
		}
	}
	return "", fmt.Errorf(
		"result %q not found in TaskRuns of PipelineRun %s/%s", result, pr.Namespace, pr.Name)
}

// GetTaskRunStatus returns the status of a specified taskRun.
func (t *TektonController) GetTaskRunStatus(c crclient.Client, pr *pipeline.PipelineRun, pipelineTaskName string) (*pipeline.PipelineRunTaskRunStatus, error) {
	for _, chr := range pr.Status.ChildReferences {
		if chr.PipelineTaskName == pipelineTaskName {
			taskRun := &pipeline.TaskRun{}
			taskRunKey := types.NamespacedName{Namespace: pr.Namespace, Name: chr.Name}
			if err := c.Get(context.Background(), taskRunKey, taskRun); err != nil {
				return nil, err
			}
			return &pipeline.PipelineRunTaskRunStatus{PipelineTaskName: chr.PipelineTaskName, Status: &taskRun.Status}, nil
		}
	}
	return nil, fmt.Errorf(
		"TaskRun status for pipeline task name %q not found in the status of PipelineRun %s/%s", pipelineTaskName, pr.Namespace, pr.Name)
}

// DeleteAllTaskRunsInASpecificNamespace removes all TaskRuns from a given repository. Useful when creating a lot of resources and wanting to remove all of them.
func (t *TektonController) DeleteAllTaskRunsInASpecificNamespace(namespace string) error {
	return t.KubeRest().DeleteAllOf(context.Background(), &pipeline.TaskRun{}, crclient.InNamespace(namespace))
}

// GetTaskRunParam gets value of a TaskRun param.
func (t *TektonController) GetTaskRunParam(c crclient.Client, pr *pipeline.PipelineRun, pipelineTaskName, paramName string) (string, error) {
	taskRun, err := t.GetTaskRunFromPipelineRun(c, pr, pipelineTaskName)
	if err != nil {
		return "", err
	}
	for _, param := range taskRun.Spec.Params {
		if param.Name == paramName {
			return strings.TrimSpace(param.Value.StringVal), nil
		}
	}
	return "", fmt.Errorf("cannot find param %s from TaskRun %s", paramName, pipelineTaskName)
}

func (t *TektonController) GetResultFromTaskRun(tr *pipeline.TaskRun, result string) (string, error) {
	for _, trResult := range tr.Status.Results {
		if trResult.Name == result {
			// for some reason the result might contain \n suffix
			return strings.TrimSuffix(trResult.Value.StringVal, "\n"), nil
		}
	}
	return "", fmt.Errorf(
		"result %q not found in TaskRun %s/%s", result, tr.Namespace, tr.Name)
}

func (t *TektonController) GetEnvVariable(tr *pipeline.TaskRun, envVar string) (string, error) {
	if tr.Status.TaskSpec != nil {
		for _, trEnv := range tr.Status.TaskSpec.StepTemplate.Env {
			if trEnv.Name == envVar {
				return strings.TrimSuffix(trEnv.Value, "\n"), nil
			}
		}
	}
	return "", fmt.Errorf(
		"env var %q not found in TaskRun %s/%s", envVar, tr.Namespace, tr.Name,
	)
}

func (t *TektonController) WatchTaskRun(taskRunName, namespace string, taskTimeout int) error {
	g.GinkgoWriter.Printf("Waiting for pipeline %q to finish\n", taskRunName)
	return utils.WaitUntil(t.CheckTaskRunFinished(taskRunName, namespace), time.Duration(taskTimeout)*time.Second)
}

// CheckTaskRunFinished checks if taskRun finished.
func (t *TektonController) CheckTaskRunFinished(taskRunName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		tr, err := t.GetTaskRun(taskRunName, namespace)
		if err != nil {
			return false, nil
		}
		if tr.Status.CompletionTime != nil {
			return true, nil
		}
		return false, nil
	}
}

// CheckTaskRunSucceeded checks if taskRun succeeded. Returns error if getting taskRun fails.
func (t *TektonController) CheckTaskRunSucceeded(taskRunName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		tr, err := t.GetTaskRun(taskRunName, namespace)
		if err != nil {
			return false, err
		}
		if len(tr.Status.Conditions) > 0 {
			for _, c := range tr.Status.Conditions {
				if c.Type == "Succeeded" && c.Status == "True" {
					return true, nil
				}
			}
		}
		return false, nil
	}
}

func (t *TektonController) RunTaskAndWait(trSpec *pipeline.TaskRun, namespace string) (*pipeline.TaskRun, error) {
	tr, err := t.CreateTaskRun(trSpec, namespace)
	if err != nil {
		return nil, err
	}
	err = t.WatchTaskRun(tr.Name, namespace, 100)
	if err != nil {
		return nil, err
	}
	return t.GetTaskRun(tr.Name, namespace)
}

func (t *TektonController) CreateTaskRun(taskRun *pipeline.TaskRun, ns string) (*pipeline.TaskRun, error) {
	return t.PipelineClient().TektonV1().TaskRuns(ns).Create(context.Background(), taskRun, metav1.CreateOptions{})
}
