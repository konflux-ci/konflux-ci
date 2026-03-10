package has

import (
	"context"
	"fmt"
	"time"

	appservice "github.com/konflux-ci/application-api/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/logs"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	rclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// GetApplication returns an application given a name and namespace from kubernetes cluster.
func (h *Controller) GetApplication(name string, namespace string) (*appservice.Application, error) {
	application := appservice.Application{
		Spec: appservice.ApplicationSpec{},
	}
	if err := h.KubeRest().Get(context.Background(), types.NamespacedName{Name: name, Namespace: namespace}, &application); err != nil {
		return nil, err
	}

	return &application, nil
}

// CreateApplication creates an application in the kubernetes cluster with 10 minutes default time for creation.
func (h *Controller) CreateApplication(name string, namespace string) (*appservice.Application, error) {
	return h.CreateApplicationWithTimeout(name, namespace, time.Minute*10)
}

// CreateHasApplicationWithTimeout creates an application in the kubernetes cluster with a custom default time for creation.
func (h *Controller) CreateApplicationWithTimeout(name string, namespace string, timeout time.Duration) (*appservice.Application, error) {
	application := &appservice.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appservice.ApplicationSpec{
			DisplayName: name,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*1)
	defer cancel()
	if err := h.KubeRest().Create(ctx, application); err != nil {
		return nil, err
	}

	return application, nil
}

// DeleteApplication delete a HAS Application resource from the namespace.
// Optionally, it can avoid returning an error if the resource did not exist:
// - specify 'false', if it's likely the Application has already been deleted (for example, because the Namespace was deleted)
func (h *Controller) DeleteApplication(name string, namespace string, reportErrorOnNotFound bool) error {
	application := appservice.Application{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	if err := h.KubeRest().Delete(context.Background(), &application); err != nil {
		if !k8sErrors.IsNotFound(err) || (k8sErrors.IsNotFound(err) && reportErrorOnNotFound) {
			return fmt.Errorf("error deleting an application: %+v", err)
		}
	}
	return utils.WaitUntil(h.ApplicationDeleted(&application), 1*time.Minute)
}

// ApplicationDeleted check if a given application object was deleted successfully from the kubernetes cluster.
func (h *Controller) ApplicationDeleted(application *appservice.Application) wait.ConditionFunc {
	return func() (bool, error) {
		_, err := h.GetApplication(application.Name, application.Namespace)
		return err != nil && k8sErrors.IsNotFound(err), nil
	}
}

// DeleteAllApplicationsInASpecificNamespace removes all application CRs from a specific namespace. Useful when creating a lot of resources and want to remove all of them
func (h *Controller) DeleteAllApplicationsInASpecificNamespace(namespace string, timeout time.Duration) error {
	if err := h.KubeRest().DeleteAllOf(context.Background(), &appservice.Application{}, rclient.InNamespace(namespace)); err != nil {
		return fmt.Errorf("error deleting applications from the namespace %s: %+v", namespace, err)
	}

	return utils.WaitUntil(func() (done bool, err error) {
		applicationList, err := h.ListAllApplications(namespace)
		if err != nil {
			return false, nil
		}
		return len(applicationList.Items) == 0, nil
	}, timeout)
}

// ListAllApplications returns a list of all Applications in a given namespace.
func (h *Controller) ListAllApplications(namespace string) (*appservice.ApplicationList, error) {
	applicationList := &appservice.ApplicationList{}
	err := h.KubeRest().List(context.Background(), applicationList, &rclient.ListOptions{Namespace: namespace})

	return applicationList, err
}

// StoreApplication stores a given Application as an artifact.
func (h *Controller) StoreApplication(application *appservice.Application) error {
	return logs.StoreResourceYaml(application, "application-"+application.Name)
}

// StoreAllApplications stores all Applications in a given namespace.
func (h *Controller) StoreAllApplications(namespace string) error {
	applicationList, err := h.ListAllApplications(namespace)
	if err != nil {
		return err
	}

	for _, application := range applicationList.Items {
		if err := h.StoreApplication(&application); err != nil {
			return err
		}
	}
	return nil
}
