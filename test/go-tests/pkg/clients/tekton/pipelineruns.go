package tekton

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/logs"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils/tekton"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"

	g "github.com/onsi/ginkgo/v2"
)

// CreatePipelineRun creates a tekton pipelineRun and returns the pipelineRun or error
func (t *Controller) CreatePipelineRun(pipelineRun *pipeline.PipelineRun, ns string) (*pipeline.PipelineRun, error) {
	return t.PipelineClient().TektonV1().PipelineRuns(ns).Create(context.Background(), pipelineRun, metav1.CreateOptions{})
}

// createAndWait creates a pipelineRun and waits until it starts.
func (t *Controller) createAndWait(pr *pipeline.PipelineRun, namespace string, taskTimeout int) (*pipeline.PipelineRun, error) {
	pipelineRun, err := t.CreatePipelineRun(pr, namespace)
	if err != nil {
		return nil, err
	}
	g.GinkgoWriter.Printf("Creating Pipeline %q\n", pipelineRun.Name)
	return pipelineRun, utils.WaitUntil(t.CheckPipelineRunStarted(pipelineRun.Name, namespace), time.Duration(taskTimeout)*time.Second)
}

// RunPipeline creates a pipelineRun and waits for it to start.
func (t *Controller) RunPipeline(g tekton.PipelineRunGenerator, namespace string, taskTimeout int) (*pipeline.PipelineRun, error) {
	pr, err := g.Generate()
	if err != nil {
		return nil, err
	}
	pvcs := t.KubeInterface().CoreV1().PersistentVolumeClaims(pr.Namespace)
	for _, w := range pr.Spec.Workspaces {
		if w.PersistentVolumeClaim != nil {
			pvcName := w.PersistentVolumeClaim.ClaimName
			if _, err := pvcs.Get(context.Background(), pvcName, metav1.GetOptions{}); err != nil {
				if errors.IsNotFound(err) {
					err := tekton.CreatePVC(pvcs, pvcName)
					if err != nil {
						return nil, err
					}
				} else {
					return nil, err
				}
			}
		}
	}

	return t.createAndWait(pr, namespace, taskTimeout)
}

// GetPipelineRun returns a pipelineRun with a given name.
func (t *Controller) GetPipelineRun(pipelineRunName, namespace string) (*pipeline.PipelineRun, error) {
	return t.PipelineClient().TektonV1().PipelineRuns(namespace).Get(context.Background(), pipelineRunName, metav1.GetOptions{})
}

// GetPipelineRunLogs returns logs of a given pipelineRun.
func (t *Controller) GetPipelineRunLogs(prefix, pipelineRunName, namespace string) (string, error) {
	podClient := t.KubeInterface().CoreV1().Pods(namespace)
	podList, err := podClient.List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	podLog := ""
	for _, pod := range podList.Items {
		if !strings.HasPrefix(pod.Name, prefix) {
			continue
		}
		for _, c := range pod.Spec.InitContainers {
			var err error
			var cLog string
			cLog, err = t.fetchContainerLog(pod.Name, c.Name, namespace)
			podLog = podLog + fmt.Sprintf("\n pod: %s | init container: %s\n", pod.Name, c.Name) + cLog
			if err != nil {
				return podLog, err
			}
		}
		for _, c := range pod.Spec.Containers {
			var err error
			var cLog string
			cLog, err = t.fetchContainerLog(pod.Name, c.Name, namespace)
			podLog = podLog + fmt.Sprintf("\npod: %s | container %s: \n", pod.Name, c.Name) + cLog
			if err != nil {
				return podLog, err
			}
		}
	}
	return podLog, nil
}

// GetPipelineRunWatch returns pipelineRun watch interface.
func (t *Controller) GetPipelineRunWatch(ctx context.Context, namespace string) (watch.Interface, error) {
	return t.PipelineClient().TektonV1().PipelineRuns(namespace).Watch(ctx, metav1.ListOptions{})
}

// WatchPipelineRun waits until pipelineRun finishes.
func (t *Controller) WatchPipelineRun(pipelineRunName, namespace string, taskTimeout int) error {
	g.GinkgoWriter.Printf("Waiting for pipeline %q to finish\n", pipelineRunName)
	return utils.WaitUntil(t.CheckPipelineRunFinished(pipelineRunName, namespace), time.Duration(taskTimeout)*time.Second)
}

// WatchPipelineRunSucceeded waits until the pipelineRun succeeds.
func (t *Controller) WatchPipelineRunSucceeded(pipelineRunName, namespace string, taskTimeout int) error {
	g.GinkgoWriter.Printf("Waiting for pipeline %q to finish\n", pipelineRunName)
	return utils.WaitUntil(t.CheckPipelineRunSucceeded(pipelineRunName, namespace), time.Duration(taskTimeout)*time.Second)
}

// CheckPipelineRunStarted checks if pipelineRUn started.
func (t *Controller) CheckPipelineRunStarted(pipelineRunName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pr, err := t.GetPipelineRun(pipelineRunName, namespace)
		if err != nil {
			return false, nil
		}
		if pr.Status.StartTime != nil {
			return true, nil
		}
		return false, nil
	}
}

// CheckPipelineRunFinished checks if pipelineRun finished.
func (t *Controller) CheckPipelineRunFinished(pipelineRunName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pr, err := t.GetPipelineRun(pipelineRunName, namespace)
		if err != nil {
			return false, nil
		}
		if pr.Status.CompletionTime != nil {
			return true, nil
		}
		return false, nil
	}
}

// CheckPipelineRunSucceeded checks if pipelineRun succeeded. Returns error if getting pipelineRun fails.
func (t *Controller) CheckPipelineRunSucceeded(pipelineRunName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pr, err := t.GetPipelineRun(pipelineRunName, namespace)
		if err != nil {
			return false, err
		}
		if len(pr.Status.Conditions) > 0 {
			for _, c := range pr.Status.Conditions {
				if c.Type == "Succeeded" && c.Status == "True" {
					return true, nil
				}
			}
		}
		return false, nil
	}
}

// ListAllPipelineRuns returns a list of all pipelineRuns in a namespace.
func (t *Controller) ListAllPipelineRuns(ns string) (*pipeline.PipelineRunList, error) {
	return t.PipelineClient().TektonV1().PipelineRuns(ns).List(context.Background(), metav1.ListOptions{})
}

// DeletePipelineRun deletes a pipelineRun form a given namespace.
func (t *Controller) DeletePipelineRun(name, ns string) error {
	return t.PipelineClient().TektonV1().PipelineRuns(ns).Delete(context.Background(), name, metav1.DeleteOptions{})
}

// DeletePipelineRunIgnoreFinalizers deletes PipelineRun (removing the finalizers field, first)
func (t *Controller) DeletePipelineRunIgnoreFinalizers(ns, name string) error {
	err := wait.PollUntilContextTimeout(context.Background(), time.Second, 30*time.Second, true, func(ctx context.Context) (done bool, err error) {
		pipelineRunCR := pipeline.PipelineRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: ns,
			},
		}
		patch := client.RawPatch(types.JSONPatchType, []byte(`[{"op":"remove","path":"/metadata/finalizers"}]`))
		if err := t.KubeRest().Patch(context.Background(), &pipelineRunCR, patch); err != nil {
			if errors.IsNotFound(err) {
				// PipelinerRun CR is already removed
				return true, nil
			}
			g.GinkgoWriter.Printf("unable to patch PipelineRun '%s' in '%s': %v\n", pipelineRunCR.Name, pipelineRunCR.Namespace, err)
			return false, nil

		}

		if err := t.KubeRest().Delete(context.Background(), &pipelineRunCR); err != nil {
			if strings.HasSuffix(err.Error(), " not found") {
				return true, nil
			} else {
				g.GinkgoWriter.Printf("unable to delete PipelineRun '%s' in '%s': %v\n", pipelineRunCR.Name, pipelineRunCR.Namespace, err)
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("deletion of PipelineRun '%s' in '%s' timed out", name, ns)
	}

	return nil
}

// DeleteAllPipelineRunsInASpecificNamespace deletes all PipelineRuns in a given namespace (removing the finalizers field, first)
func (t *Controller) DeleteAllPipelineRunsInASpecificNamespace(ns string) error {

	pipelineRunList, err := t.ListAllPipelineRuns(ns)
	if err != nil || pipelineRunList == nil {
		return fmt.Errorf("unable to delete all PipelineRuns in '%s': %v", ns, err)
	}

	for _, pipelineRun := range pipelineRunList.Items {
		err := t.DeletePipelineRunIgnoreFinalizers(ns, pipelineRun.Name)
		if err != nil {
			return err
		}
	}

	return nil
}

// StorePipelineRun stores a given PipelineRun as an artifact.
func (t *Controller) StorePipelineRun(prefix string, pipelineRun *pipeline.PipelineRun) error {
	artifacts := make(map[string][]byte)
	pipelineRunLog, err := t.GetPipelineRunLogs(prefix, pipelineRun.Name, pipelineRun.Namespace)
	if err != nil {
		g.GinkgoWriter.Printf("an error happened during storing pipelineRun log %s:%s: %s\n", pipelineRun.GetNamespace(), pipelineRun.GetName(), err.Error())
	}
	artifacts["pipelineRun-"+pipelineRun.Name+".log"] = []byte(pipelineRunLog)

	pipelineRunYaml, err := yaml.Marshal(pipelineRun)
	if err != nil {
		return err
	}
	artifacts["pipelineRun-"+pipelineRun.Name+".yaml"] = pipelineRunYaml

	if err := logs.StoreArtifacts(artifacts); err != nil {
		return err
	}

	return nil
}

// StoreAllPipelineRuns stores all PipelineRuns in a given namespace.
func (t *Controller) StoreAllPipelineRuns(namespace string) error {
	pipelineRuns, err := t.ListAllPipelineRuns(namespace)
	if err != nil {
		return fmt.Errorf("got error fetching PR list: %w", err)
	}

	for _, pipelineRun := range pipelineRuns.Items {
		pipelineRun := pipelineRun
		if err := t.StorePipelineRun(pipelineRun.GetName(), &pipelineRun); err != nil {
			return fmt.Errorf("got error storing PR: %w", err)
		}
	}

	return nil
}

func (t *Controller) AddFinalizerToPipelineRun(pipelineRun *pipeline.PipelineRun, finalizerName string) error {
	ctx := context.Background()
	kubeClient := t.KubeRest()
	patch := client.MergeFrom(pipelineRun.DeepCopy())
	if ok := controllerutil.AddFinalizer(pipelineRun, finalizerName); ok {
		err := kubeClient.Patch(ctx, pipelineRun, patch)
		if err != nil {
			return fmt.Errorf("error occurred while patching the updated PipelineRun after finalizer addition: %v", err)
		}
	}
	return nil
}

func (t *Controller) RemoveFinalizerFromPipelineRun(pipelineRun *pipeline.PipelineRun, finalizerName string) error {
	ctx := context.Background()
	kubeClient := t.KubeRest()
	patch := client.MergeFrom(pipelineRun.DeepCopy())
	if ok := controllerutil.RemoveFinalizer(pipelineRun, finalizerName); ok {
		err := kubeClient.Patch(ctx, pipelineRun, patch)
		if err != nil {
			return fmt.Errorf("error occurred while patching the updated PipelineRun after finalizer removal: %v", err)
		}
	}
	return nil
}
