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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"knative.dev/pkg/apis"
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
	logs, _, err := t.collectPipelineRunLogs(prefix, namespace)
	return logs, err
}

// collectedContainerLogs records container logs already read for the artifact.
// A container is recorded only when its log was read without error.
// complete is set once every container of that pod was read.
type collectedContainerLogs struct {
	logs     map[string]map[string]string
	complete map[string]bool
}

func (c *collectedContainerLogs) remember(pod, container, log string) {
	if c.logs == nil {
		c.logs = map[string]map[string]string{}
	}
	if c.logs[pod] == nil {
		c.logs[pod] = map[string]string{}
	}
	c.logs[pod][container] = log
}

func (c *collectedContainerLogs) markComplete(pod string) {
	if c.complete == nil {
		c.complete = map[string]bool{}
	}
	c.complete[pod] = true
}

func (c *collectedContainerLogs) lookup(pod, container string) (string, bool) {
	if c == nil || c.logs == nil {
		return "", false
	}
	containers, ok := c.logs[pod]
	if !ok {
		return "", false
	}
	log, ok := containers[container]
	return log, ok
}

func (c *collectedContainerLogs) podHasLogText(pod string) bool {
	if c == nil {
		return false
	}
	for _, log := range c.logs[pod] {
		if strings.TrimSpace(log) != "" {
			return true
		}
	}
	return false
}

func (c *collectedContainerLogs) podComplete(pod string) bool {
	return c != nil && c.complete[pod]
}

func (t *Controller) collectPipelineRunLogs(prefix, namespace string) (string, *collectedContainerLogs, error) {
	collected := &collectedContainerLogs{}
	podClient := t.KubeInterface().CoreV1().Pods(namespace)
	podList, err := podClient.List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return "", collected, err
	}
	podLog := ""
	for _, pod := range podList.Items {
		if !strings.HasPrefix(pod.Name, prefix) {
			continue
		}
		for _, container := range pod.Spec.InitContainers {
			cLog, err := t.fetchContainerLog(pod.Name, container.Name, namespace)
			podLog = podLog + fmt.Sprintf("\n pod: %s | init container: %s\n", pod.Name, container.Name) + cLog
			if err != nil {
				return podLog, collected, err
			}
			collected.remember(pod.Name, container.Name, cLog)
		}
		for _, container := range pod.Spec.Containers {
			cLog, err := t.fetchContainerLog(pod.Name, container.Name, namespace)
			podLog = podLog + fmt.Sprintf("\npod: %s | container %s: \n", pod.Name, container.Name) + cLog
			if err != nil {
				return podLog, collected, err
			}
			collected.remember(pod.Name, container.Name, cLog)
		}
		collected.markComplete(pod.Name)
	}
	return podLog, collected, nil
}

// DescribePipelineRunTasks loads each child TaskRun and returns its status, including
// the current step. A TaskRun that cannot be loaded is still named.
func (t *Controller) DescribePipelineRunTasks(pr *pipeline.PipelineRun) string {
	var b strings.Builder
	if len(pr.Status.ChildReferences) == 0 {
		b.WriteString("Tasks: none recorded in status.childReferences\n")
	} else {
		b.WriteString("Tasks:\n")
		for _, chr := range pr.Status.ChildReferences {
			tr := &pipeline.TaskRun{}
			err := t.KubeRest().Get(context.Background(), types.NamespacedName{Namespace: pr.Namespace, Name: chr.Name}, tr)
			if err != nil {
				b.WriteString(tekton.FormatChildTaskStatus(chr.PipelineTaskName, chr.Name, nil, err))
				continue
			}
			b.WriteString(tekton.FormatChildTaskStatus(chr.PipelineTaskName, chr.Name, tr, nil))
		}
	}
	b.WriteString(tekton.FormatSkippedTasks(pr))
	return b.String()
}

// DescribePipelineRunFailure is the failure report: Succeeded condition, every child
// TaskRun, and container logs for tasks that have failed. Tasks with no pod keep
// their condition message so a failure before pod creation is still visible.
func (t *Controller) DescribePipelineRunFailure(pr *pipeline.PipelineRun) string {
	return t.describePipelineRunFailure(pr, nil)
}

// describePipelineRunFailure builds the failure report. collected holds container
// logs already stored in the artifact; those containers are not fetched again.
// Task status is always included.
func (t *Controller) describePipelineRunFailure(pr *pipeline.PipelineRun, collected *collectedContainerLogs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", tekton.FormatPipelineRunCondition(pr))
	b.WriteString(t.DescribePipelineRunTasks(pr))
	if tekton.HasPipelineRunFailed(pr) {
		b.WriteString(t.failedTaskPodLogs(pr, collected))
	}
	return b.String()
}

func (t *Controller) failedTaskPodLogs(pr *pipeline.PipelineRun, collected *collectedContainerLogs) string {
	var b strings.Builder
	for _, chr := range pr.Status.ChildReferences {
		tr := &pipeline.TaskRun{}
		if err := t.KubeRest().Get(context.Background(), types.NamespacedName{Namespace: pr.Namespace, Name: chr.Name}, tr); err != nil {
			fmt.Fprintf(&b, "Logs for task %s TaskRun %s: failed to get TaskRun: %v\n", chr.PipelineTaskName, chr.Name, err)
			continue
		}
		cond := tr.GetStatusCondition().GetCondition(apis.ConditionSucceeded)
		if !cond.IsFalse() {
			continue
		}
		if tr.Status.PodName == "" {
			fmt.Fprintf(&b, "Logs for task %s TaskRun %s: no pod (reason=%s message=%s)\n",
				chr.PipelineTaskName, tr.Name, cond.GetReason(), cond.GetMessage())
			continue
		}
		if collected.podComplete(tr.Status.PodName) {
			if !collected.podHasLogText(tr.Status.PodName) {
				fmt.Fprintf(&b, "Logs for task %s TaskRun %s: no container logs (reason=%s message=%s)\n",
					chr.PipelineTaskName, tr.Name, cond.GetReason(), cond.GetMessage())
			}
			continue
		}
		logs := t.podContainerLogs(tr.Status.PodName, pr.Namespace, collected)
		if strings.TrimSpace(logs) == "" {
			if collected.podHasLogText(tr.Status.PodName) {
				continue
			}
			fmt.Fprintf(&b, "Logs for task %s TaskRun %s: no container logs (reason=%s message=%s)\n",
				chr.PipelineTaskName, tr.Name, cond.GetReason(), cond.GetMessage())
			continue
		}
		fmt.Fprintf(&b, "Logs for failed task %s TaskRun %s pod %s:\n", chr.PipelineTaskName, tr.Name, tr.Status.PodName)
		b.WriteString(logs)
	}
	return b.String()
}

// ShowsPullImageFailure reports whether the PipelineRun or one of its child
// TaskRuns is currently failing to pull an image. PullImageFailed is set while
// the TaskRun is still running.
func (t *Controller) ShowsPullImageFailure(pr *pipeline.PipelineRun) bool {
	cond := pr.GetStatusCondition().GetCondition(apis.ConditionSucceeded)
	if tekton.StatusShowsPullImageFailure(cond.GetReason(), cond.GetMessage()) {
		return true
	}
	for _, chr := range pr.Status.ChildReferences {
		tr := &pipeline.TaskRun{}
		if err := t.KubeRest().Get(context.Background(), types.NamespacedName{Namespace: pr.Namespace, Name: chr.Name}, tr); err != nil {
			continue
		}
		if tekton.TaskRunShowsPullImageFailure(tr) {
			return true
		}
	}
	return false
}

// podContainerLogs returns logs for containers that are not already in collected.
// Containers recorded there are left to the existing artifact text.
func (t *Controller) podContainerLogs(podName, namespace string, collected *collectedContainerLogs) string {
	pod, err := t.KubeInterface().CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return fmt.Sprintf("  failed to get pod %s: %v\n", podName, err)
	}
	var b strings.Builder
	writeContainers := func(kind string, containers []corev1.Container) {
		for _, c := range containers {
			if _, ok := collected.lookup(podName, c.Name); ok {
				continue
			}
			cLog, logErr := t.fetchContainerLog(podName, c.Name, namespace)
			if logErr != nil {
				fmt.Fprintf(&b, "  %s %s: failed to get logs: %v\n", kind, c.Name, logErr)
				continue
			}
			if strings.TrimSpace(cLog) == "" {
				continue
			}
			fmt.Fprintf(&b, "  %s %s:\n", kind, c.Name)
			b.WriteString(cLog)
			if !strings.HasSuffix(cLog, "\n") {
				b.WriteString("\n")
			}
		}
	}
	writeContainers("init", pod.Spec.InitContainers)
	writeContainers("container", pod.Spec.Containers)
	return b.String()
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
	pipelineRunLog, collected, err := t.collectPipelineRunLogs(prefix, pipelineRun.Namespace)
	if err != nil {
		g.GinkgoWriter.Printf("an error happened during storing pipelineRun log %s:%s: %s\n", pipelineRun.GetNamespace(), pipelineRun.GetName(), err.Error())
	}
	if tekton.HasPipelineRunFailed(pipelineRun) {
		pipelineRunLog += "\n" + t.describePipelineRunFailure(pipelineRun, collected)
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
