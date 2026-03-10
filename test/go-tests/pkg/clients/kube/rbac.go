package kube

import (
	"context"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s *Controller) ListRoles(namespace string) (*rbacv1.RoleList, error) {
	listOptions := metav1.ListOptions{}
	return s.KubeInterface().RbacV1().Roles(namespace).List(context.Background(), listOptions)
}

func (s *Controller) ListRoleBindings(namespace string) (*rbacv1.RoleBindingList, error) {
	listOptions := metav1.ListOptions{}
	return s.KubeInterface().RbacV1().RoleBindings(namespace).List(context.Background(), listOptions)
}

func (s *Controller) GetRole(roleName, namespace string) (*rbacv1.Role, error) {
	return s.KubeInterface().RbacV1().Roles(namespace).Get(context.Background(), roleName, metav1.GetOptions{})
}

func (s *Controller) GetRoleBinding(rolebindingName, namespace string) (*rbacv1.RoleBinding, error) {
	return s.KubeInterface().RbacV1().RoleBindings(namespace).Get(context.Background(), rolebindingName, metav1.GetOptions{})
}

// CreateRole creates a role with the provided name and namespace using the given list of rules
func (s *Controller) CreateRole(roleName, namespace string, roleRules map[string][]string) (*rbacv1.Role, error) {
	rules := &rbacv1.PolicyRule{
		APIGroups: roleRules["apiGroupsList"],
		Resources: roleRules["roleResources"],
		Verbs:     roleRules["roleVerbs"],
	}
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			*rules,
		},
	}
	createdRole, err := s.KubeInterface().RbacV1().Roles(namespace).Create(context.Background(), role, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return createdRole, nil
}

// CreateRoleBinding creates an object of Role Binding in namespace with service account provided and role reference api group.
func (s *Controller) CreateRoleBinding(roleBindingName, namespace, subjectKind, serviceAccountName, serviceAccountNamespace, roleRefKind, roleRefName, roleRefApiGroup string) (*rbacv1.RoleBinding, error) {
	roleBindingSubjects := []rbacv1.Subject{
		{
			Kind:      subjectKind,
			Name:      serviceAccountName,
			Namespace: serviceAccountNamespace,
		},
	}

	roleBindingRoleRef := rbacv1.RoleRef{
		Kind:     roleRefKind,
		Name:     roleRefName,
		APIGroup: roleRefApiGroup,
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleBindingName,
			Namespace: namespace,
		},
		Subjects: roleBindingSubjects,
		RoleRef:  roleBindingRoleRef,
	}

	createdRoleBinding, err := s.KubeInterface().RbacV1().RoleBindings(namespace).Create(context.Background(), roleBinding, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return createdRoleBinding, nil
}
