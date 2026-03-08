package tekton

import (
	"context"

	ecp "github.com/conforma/crds/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateEnterpriseContractPolicy creates an EnterpriseContractPolicy in a specified namespace.
func (t *TektonController) CreateEnterpriseContractPolicy(name, namespace string, ecpolicy ecp.EnterpriseContractPolicySpec) (*ecp.EnterpriseContractPolicy, error) {
	ec := &ecp.EnterpriseContractPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: ecpolicy,
	}
	return ec, t.KubeRest().Create(context.Background(), ec)
}

// CreateOrUpdatePolicyConfiguration creates new policy if it doesn't exist, otherwise updates the existing one, in a specified namespace.
func (t *TektonController) CreateOrUpdatePolicyConfiguration(namespace string, policy ecp.EnterpriseContractPolicySpec) error {
	ecPolicy := ecp.EnterpriseContractPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ec-policy",
			Namespace: namespace,
		},
	}

	// fetch to see if it exists
	err := t.KubeRest().Get(context.Background(), crclient.ObjectKey{
		Namespace: namespace,
		Name:      "ec-policy",
	}, &ecPolicy)

	exists := true
	if err != nil {
		if errors.IsNotFound(err) {
			exists = false
		} else {
			return err
		}
	}

	ecPolicy.Spec = policy
	if !exists {
		// it doesn't, so create
		if err := t.KubeRest().Create(context.Background(), &ecPolicy); err != nil {
			return err
		}
	} else {
		// it does, so update
		if err := t.KubeRest().Update(context.Background(), &ecPolicy); err != nil {
			return err
		}
	}

	return nil
}

// GetEnterpriseContractPolicy gets an EnterpriseContractPolicy from specified a namespace
func (t *TektonController) GetEnterpriseContractPolicy(name, namespace string) (*ecp.EnterpriseContractPolicy, error) {
	defaultEcPolicy := ecp.EnterpriseContractPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := t.KubeRest().Get(context.Background(), crclient.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, &defaultEcPolicy)

	return &defaultEcPolicy, err
}

// DeleteEnterpriseContractPolicy deletes enterprise contract policy.
func (t *TektonController) DeleteEnterpriseContractPolicy(name string, namespace string, failOnNotFound bool) error {
	ecPolicy := ecp.EnterpriseContractPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	err := t.KubeRest().Delete(context.Background(), &ecPolicy)
	if err != nil && !failOnNotFound && errors.IsNotFound(err) {
		err = nil
	}
	return err
}
