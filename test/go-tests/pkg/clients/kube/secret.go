package kube

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

// Creates a new secret in a specified namespace
func (s *Controller) CreateSecret(ns string, secret *corev1.Secret) (*corev1.Secret, error) {
	return s.KubeInterface().CoreV1().Secrets(ns).Create(context.Background(), secret, metav1.CreateOptions{})
}

// Check if a secret exists, return secret and error
func (s *Controller) GetSecret(ns string, name string) (*corev1.Secret, error) {
	return s.KubeInterface().CoreV1().Secrets(ns).Get(context.Background(), name, metav1.GetOptions{})
}

// Update a secret in a specified namespace
func (s *Controller) UpdateSecret(ns string, secret *corev1.Secret) (*corev1.Secret, error) {
	return s.KubeInterface().CoreV1().Secrets(ns).Update(context.Background(), secret, metav1.UpdateOptions{})
}

// Delete a secret in a specified namespace
func (s *Controller) DeleteSecret(ns string, name string) error {
	return s.KubeInterface().CoreV1().Secrets(ns).Delete(context.Background(), name, metav1.DeleteOptions{})
}

// ListSecrets return a list of secrets from a namespace by label and selection limits
func (s *Controller) ListSecrets(ns string, labelKey string, labelValue string, selectionLimit int64) (*corev1.SecretList, error) {
	labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{labelKey: labelValue}}
	listOptions := metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
		Limit:         selectionLimit,
	}
	return s.KubeInterface().CoreV1().Secrets(ns).List(context.Background(), listOptions)
}

// Delete all secrets in a specified namespace matching to label
func (s *Controller) DeleteSecretsByLabel(ns string, labelKey string, labelValue string) error {
	secretList, err := s.ListSecrets(ns, labelKey, labelValue, 1024)
	if err != nil {
		return err
	}

	for _, secret := range secretList.Items {
		err = s.DeleteSecret(ns, secret.Name)
		if err != nil {
			return err
		}
	}
	return nil
}

// Links a secret to a specified serviceaccount, if argument addImagePullSecrets is true secret will be added also to ImagePullSecrets of SA.
func (s *Controller) LinkSecretToServiceAccount(ns, secret, serviceaccount string, addImagePullSecrets bool) error {
	timeout := 20 * time.Second
	return wait.PollUntilContextTimeout(context.Background(), time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		serviceAccountObject, err := s.KubeInterface().CoreV1().ServiceAccounts(ns).Get(context.Background(), serviceaccount, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, credentialSecret := range serviceAccountObject.Secrets {
			if credentialSecret.Name == secret {
				// The secret is present in the service account, no updates needed
				return true, nil
			}
		}
		serviceAccountObject.Secrets = append(serviceAccountObject.Secrets, corev1.ObjectReference{Name: secret})
		if addImagePullSecrets {
			serviceAccountObject.ImagePullSecrets = append(serviceAccountObject.ImagePullSecrets, corev1.LocalObjectReference{Name: secret})
		}
		_, err = s.KubeInterface().CoreV1().ServiceAccounts(ns).Update(context.Background(), serviceAccountObject, metav1.UpdateOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	})
}

// UnlinkSecretFromServiceAccount unlinks secret from service account
func (s *Controller) UnlinkSecretFromServiceAccount(namespace, secretName, serviceAccount string, rmImagePullSecrets bool) error {
	serviceAccountObject, err := s.KubeInterface().CoreV1().ServiceAccounts(namespace).Get(context.Background(), serviceAccount, metav1.GetOptions{})
	if err != nil {
		return err
	}

	for index, secret := range serviceAccountObject.Secrets {
		if secret.Name == secretName {
			serviceAccountObject.Secrets = append(serviceAccountObject.Secrets[:index], serviceAccountObject.Secrets[index+1:]...)
			break
		}
	}

	if rmImagePullSecrets {
		for index, secret := range serviceAccountObject.ImagePullSecrets {
			if secret.Name == secretName {
				serviceAccountObject.ImagePullSecrets = append(serviceAccountObject.ImagePullSecrets[:index], serviceAccountObject.ImagePullSecrets[index+1:]...)
				break
			}
		}
	}
	_, err = s.KubeInterface().CoreV1().ServiceAccounts(namespace).Update(context.Background(), serviceAccountObject, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

// CreateRegistryAuthSecret create a docker registry secret in a given ns
func (s *Controller) CreateRegistryAuthSecret(secretName, namespace, secretStringData string) (*corev1.Secret, error) {
	rawDecodedTextStringData, err := base64.StdEncoding.DecodeString(secretStringData)
	if err != nil {
		return nil, err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Type:       corev1.SecretTypeDockerConfigJson,
		StringData: map[string]string{corev1.DockerConfigJsonKey: string(rawDecodedTextStringData)},
	}
	er := s.KubeRest().Create(context.Background(), secret)
	if er != nil {
		return nil, er
	}
	return secret, nil
}

// CreateRegistryJsonSecret creates a secret for registry repository in namespace given with key passed.
func (s *Controller) CreateRegistryJsonSecret(name, namespace, authKey, keyName string) (*corev1.Secret, error) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{".dockerconfigjson": []byte(fmt.Sprintf("{\"auths\":{\"quay.io\":{\"username\":\"%s\",\"password\":\"%s\",\"auth\":\"dGVzdDp0ZXN0\",\"email\":\"\"}}}", keyName, authKey))},
	}
	err := s.KubeRest().Create(context.Background(), secret)
	if err != nil {
		return nil, err
	}
	return secret, nil
}

// AddRegistryAuthSecretToSA adds registry auth secret to service account
func (s *Controller) AddRegistryAuthSecretToSA(registryAuth, namespace string) error {
	quayToken := utils.GetEnv(registryAuth, "")
	if quayToken == "" {
		return fmt.Errorf("%s", "failed to get registry auth secret")
	}

	_, err := s.CreateRegistryAuthSecret(constants.RegistryAuthSecretName, namespace, quayToken)
	if err != nil {
		return err
	}

	err = s.LinkSecretToServiceAccount(namespace, constants.RegistryAuthSecretName, constants.DefaultPipelineServiceAccount, true)
	if err != nil {
		return err
	}

	return nil
}

// copy the quay secret to a user defined namespace
func (s *Controller) CreateQuayRegistrySecret(namespace string) error {
	var dockerConfigJsonData []byte
	sharedSecret, err := s.GetSecret(constants.QuayRepositorySecretNamespace, constants.QuayRepositorySecretName)
	if err != nil {
		klog.Warningf("couldn't find a secret %s/%s, will try to retrieve it from QUAY_TOKEN env var", constants.QuayRepositorySecretNamespace, constants.QuayRepositorySecretName)
		quayToken := os.Getenv("QUAY_TOKEN")
		if quayToken == "" {
			return fmt.Errorf("failed to obtain quay token from 'QUAY_TOKEN' env; make sure the env var exists")
		}

		dockerConfigJsonData, err = base64.StdEncoding.DecodeString(quayToken)
		if err != nil {
			return fmt.Errorf("failed to decode quay token. Make sure that QUAY_TOKEN env contain a base64 token")
		}
	} else {
		dockerConfigJsonData = sharedSecret.Data[".dockerconfigjson"]
	}

	_, err = s.GetSecret(namespace, constants.QuayRepositorySecretName)
	if err != nil {
		if !k8sErrors.IsNotFound(err) {
			return err
		}
	} else {
		err = s.DeleteSecret(namespace, constants.QuayRepositorySecretName)
		if err != nil {
			return err
		}
	}

	repositorySecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: constants.QuayRepositorySecretName, Namespace: namespace},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{corev1.DockerConfigJsonKey: dockerConfigJsonData}}
	_, err = s.CreateSecret(namespace, repositorySecret)
	if err != nil {
		return err
	}

	err = s.LinkSecretToServiceAccount(namespace, constants.QuayRepositorySecretName, constants.DefaultPipelineServiceAccount, true)
	if err != nil {
		return err
	}

	return nil
}
