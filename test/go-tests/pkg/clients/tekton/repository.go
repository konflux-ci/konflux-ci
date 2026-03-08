package tekton

import (
	"context"
	"fmt"

	pacv1alpha1 "github.com/openshift-pipelines/pipelines-as-code/pkg/apis/pipelinesascode/v1alpha1"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// GetRepositoryParams returns the params for a PaC Repository CR owned by the
// given component. Build-service always sets the Component as an ownerReference
// on the Repository CR regardless of the CR naming scheme.
func (t *TektonController) GetRepositoryParams(componentName, namespace string) ([]pacv1alpha1.Params, error) {
	ctx := context.Background()
	repositoryList := &pacv1alpha1.RepositoryList{}
	if err := t.KubeRest().List(ctx, repositoryList, &rclient.ListOptions{Namespace: namespace}); err != nil {
		return nil, fmt.Errorf("list PaC repositories in namespace %s: %w", namespace, err)
	}

	if len(repositoryList.Items) == 0 {
		return nil, fmt.Errorf("no PaC Repository CRs found in namespace %s (component %s)", namespace, componentName)
	}

	for i := range repositoryList.Items {
		repo := &repositoryList.Items[i]
		for _, ref := range repo.OwnerReferences {
			if ref.Kind == "Component" && ref.Name == componentName {
				if repo.Spec.Params == nil {
					return []pacv1alpha1.Params{}, nil
				}
				return *repo.Spec.Params, nil
			}
		}
	}

	names := make([]string, 0, len(repositoryList.Items))
	for _, r := range repositoryList.Items {
		names = append(names, r.Name)
	}
	return nil, fmt.Errorf("no PaC Repository CR owned by component %q in namespace %s (%d repositories: %v)",
		componentName, namespace, len(repositoryList.Items), names)
}
