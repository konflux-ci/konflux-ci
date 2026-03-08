package common

import (
	"context"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetCronJob returns cronjob if found in namespace with the given name, else an error will be returned
func (s *SuiteController) GetCronJob(namespace, name string) (*batchv1.CronJob, error) {
	return s.KubeInterface().BatchV1().CronJobs(namespace).Get(context.TODO(), name, metav1.GetOptions{})
}
