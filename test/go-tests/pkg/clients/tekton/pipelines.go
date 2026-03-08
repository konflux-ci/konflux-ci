package tekton

import (
	"context"

	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreatePipeline creates a tekton pipeline and returns the pipeline or an error
func (t *TektonController) CreatePipeline(pipeline *pipeline.Pipeline, ns string) (*pipeline.Pipeline, error) {
	return t.PipelineClient().TektonV1().Pipelines(ns).Create(context.Background(), pipeline, metav1.CreateOptions{})
}

// DeletePipeline removes the pipeline from given namespace.
func (t *TektonController) DeletePipeline(name, ns string) error {
	return t.PipelineClient().TektonV1().Pipelines(ns).Delete(context.Background(), name, metav1.DeleteOptions{})
}
