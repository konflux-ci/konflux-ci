package tekton

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
)

// fetchContainerLog fetches logs of a given container.
func (t *TektonController) fetchContainerLog(podName, containerName, namespace string) (string, error) {
	podClient := t.KubeInterface().CoreV1().Pods(namespace)
	req := podClient.GetLogs(podName, &corev1.PodLogOptions{Container: containerName})
	readCloser, err := req.Stream(context.Background())
	log := ""
	if err != nil {
		return log, err
	}
	defer readCloser.Close()
	b, err := io.ReadAll(readCloser)
	if err != nil {
		return log, err
	}
	return string(b[:]), nil
}
