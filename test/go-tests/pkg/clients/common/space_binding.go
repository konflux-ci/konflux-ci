package common

import (
	"context"
	"fmt"
	"strings"

	toolchainApi "github.com/codeready-toolchain/api/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateSpaceBinding creates SpaceBinding resource for the given murName and spaceName
func (s *SuiteController) CreateSpaceBinding(murName, spaceName, spaceRole string) (*toolchainApi.SpaceBinding, error) {
	namePrefix := fmt.Sprintf("%s-%s", murName, spaceName)
	if len(namePrefix) > 50 {
		namePrefix = namePrefix[0:50]
	}
	spaceBinding := &toolchainApi.SpaceBinding{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: namePrefix + "-",
			Namespace:    "toolchain-host-operator",
			Labels: map[string]string{
				toolchainApi.SpaceBindingMasterUserRecordLabelKey: murName,
				toolchainApi.SpaceBindingSpaceLabelKey:            spaceName,
			},
		},
		Spec: toolchainApi.SpaceBindingSpec{
			MasterUserRecord: murName,
			Space:            spaceName,
			SpaceRole:        spaceRole,
		},
	}

	err := s.KubeRest().Create(context.Background(), spaceBinding)
	if err != nil {
		return &toolchainApi.SpaceBinding{}, err
	}

	return spaceBinding, nil
}

// CheckWorkspaceShare checks if the given user was added to given namespace
func (s *SuiteController) CheckWorkspaceShare(user, namespace string) error {
	ns, err := s.GetNamespace(namespace)
	if err != nil {
		return nil
	}

	annotation := "toolchain.dev.openshift.com/last-applied-space-roles"
	annotations := ns.Annotations

	if _, ok := annotations[annotation]; !ok {
		return fmt.Errorf("error finding annotation %s", annotation)
	}

	lastAppliedSpaceRoles := annotations[annotation]

	if !strings.Contains(lastAppliedSpaceRoles, user) {
		return fmt.Errorf("error finding user %s in annotation %s", user, annotation)
	}

	return nil
}
