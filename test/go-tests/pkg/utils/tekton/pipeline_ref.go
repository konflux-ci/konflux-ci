package tekton

import (
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
)

// GetPipelineNameAndBundleRef returns the pipeline name and bundle reference from a pipelineRef
// https://tekton.dev/docs/pipelines/pipelineruns/#tekton-bundles
func GetPipelineNameAndBundleRef(pipelineRef *pipeline.PipelineRef) (string, string) {
	var name string
	var bundleRef string

	// Prefer the v1 style
	if pipelineRef.Resolver != "" {
		for _, param := range pipelineRef.Params {
			switch param.Name {
			case "name":
				name = param.Value.StringVal
			case "bundle":
				bundleRef = param.Value.StringVal
			}
		}
	}

	return name, bundleRef
}

func NewBundleResolverPipelineRef(name string, bundleRef string) *pipeline.PipelineRef {
	return &pipeline.PipelineRef{
		ResolverRef: pipeline.ResolverRef{
			Resolver: "bundles",
			Params: []pipeline.Param{
				{Name: "name", Value: pipeline.ParamValue{StringVal: name, Type: pipeline.ParamTypeString}},
				{Name: "bundle", Value: pipeline.ParamValue{StringVal: bundleRef, Type: pipeline.ParamTypeString}},
				{Name: "kind", Value: pipeline.ParamValue{StringVal: "pipeline", Type: pipeline.ParamTypeString}},
			},
		},
	}
}
