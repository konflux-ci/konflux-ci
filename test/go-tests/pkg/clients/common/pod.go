package common

import (
	"context"
	"fmt"
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/logs"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	ginkgo "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
)

// GetPod returns the pod object from a given namespace and pod name
func (s *SuiteController) GetPod(namespace, podName string) (*corev1.Pod, error) {
	return s.KubeInterface().CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{})
}

func (s *SuiteController) IsPodRunning(podName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pod, err := s.GetPod(namespace, podName)
		if err != nil {
			return false, nil
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			return true, nil
		case corev1.PodFailed, corev1.PodSucceeded:
			return false, fmt.Errorf("pod %q ran to completion", pod.Name)
		}
		return false, nil
	}
}

// Checks phases of a given pod name in a given namespace
func (s *SuiteController) IsPodSuccessful(podName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pod, err := s.GetPod(namespace, podName)
		if err != nil {
			return false, nil
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			return true, nil
		case corev1.PodFailed:
			return false, fmt.Errorf("pod %q has failed", pod.Name)
		}
		return false, nil
	}
}

// ListPods return a list of pods from a namespace by labels and selection limits
func (s *SuiteController) ListPods(namespace, labelKey, labelValue string, selectionLimit int64) (*corev1.PodList, error) {
	labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{labelKey: labelValue}}
	listOptions := metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
		Limit:         selectionLimit,
	}
	return s.KubeInterface().CoreV1().Pods(namespace).List(context.Background(), listOptions)
}

// wait for a pod based on a condition. cond can be IsPodSuccessful for example
func (s *SuiteController) WaitForPod(cond wait.ConditionFunc, timeout int) error {
	if err := utils.WaitUntil(cond, time.Duration(timeout)*time.Second); err != nil {
		return err
	}
	return nil
}

// Wait for a pod selector until exists
func (s *SuiteController) WaitForPodSelector(
	fn func(podName, namespace string) wait.ConditionFunc, namespace, labelKey string, labelValue string,
	timeout int, selectionLimit int64) error {
	podList, err := s.ListPods(namespace, labelKey, labelValue, selectionLimit)
	if err != nil {
		return err
	}
	if len(podList.Items) == 0 {
		return fmt.Errorf("no pods in %s with label key %s and label value %s", namespace, labelKey, labelValue)
	}

	for i := range podList.Items {
		if err := utils.WaitUntil(fn(podList.Items[i].Name, namespace), time.Duration(timeout)*time.Second); err != nil {
			return err
		}
	}
	return nil
}

// ListAllPods returns a list of all pods in a namespace.
func (s *SuiteController) ListAllPods(namespace string) (*corev1.PodList, error) {
	return s.KubeInterface().CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
}

func (s *SuiteController) GetPodLogs(pod *corev1.Pod) map[string][]byte {
	podLogs := make(map[string][]byte)

	var containers []corev1.Container
	containers = append(containers, pod.Spec.InitContainers...)
	containers = append(containers, pod.Spec.Containers...)
	for _, c := range containers {
		log, err := utils.GetContainerLogs(s.KubeInterface(), pod.Name, c.Name, pod.Namespace)
		if err != nil {
			ginkgo.GinkgoWriter.Printf("error getting logs for pod/container %s/%s: %v\n", pod.Name, c.Name, err.Error())
			continue
		}

		podLogs["pod-"+pod.Name+"-"+c.Name+".log"] = []byte(log)
	}

	return podLogs
}

// StorePod stores a given pod as an artifact.
func (s *SuiteController) StorePod(pod *corev1.Pod) error {
	podLogs := s.GetPodLogs(pod)
	return logs.StoreArtifacts(podLogs)
}

// StoreAllPods stores all pods in a given namespace.
func (s *SuiteController) StoreAllPods(namespace string) error {
	podList, err := s.ListAllPods(namespace)
	if err != nil {
		return err
	}

	for _, pod := range podList.Items {
		if err := s.StorePod(&pod); err != nil {
			return err
		}
	}
	return nil
}

func (s *SuiteController) DeletePod(podName string, namespace string) error {
	if err := s.KubeInterface().CoreV1().Pods(namespace).Delete(context.Background(), podName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to restart pod '%s' in '%s' namespace: %+v", podName, namespace, err)
	}
	return nil
}

func (s *SuiteController) CreatePod(pod *corev1.Pod, namespace string) (*corev1.Pod, error) {
	return s.KubeInterface().CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})
}

func (s *SuiteController) GetPodLogsByName(podName, namespace string) (map[string][]byte, error) {
	pod, err := s.GetPod(namespace, podName)
	if err != nil {
		return map[string][]byte{}, err
	}
	logs := s.GetPodLogs(pod)
	return logs, nil
}
