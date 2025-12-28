/*
Copyright 2025 Konflux CI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// SetEnvVar sets or updates an environment variable in the container.
// If the env var already exists, its value is updated. Otherwise, it's appended.
func SetEnvVar(container *corev1.Container, name, value string) {
	for i, env := range container.Env {
		if env.Name == name {
			container.Env[i].Value = value
			return
		}
	}
	container.Env = append(container.Env, corev1.EnvVar{Name: name, Value: value})
}

// SetContainerArg adds an argument to the container if not already present.
// Returns true if the arg was added, false if it already existed.
func SetContainerArg(container *corev1.Container, arg string) bool {
	for _, existingArg := range container.Args {
		if existingArg == arg {
			return false
		}
	}
	container.Args = append(container.Args, arg)
	return true
}

// SetResources sets the resource requirements for the container.
// This replaces any existing resource configuration.
func SetResources(container *corev1.Container, resources corev1.ResourceRequirements) {
	container.Resources = resources
}

// SetReplicas sets the replica count for the deployment.
func SetReplicas(deployment *appsv1.Deployment, replicas int32) {
	deployment.Spec.Replicas = &replicas
}

// GetFirstContainer returns a pointer to the first container in the deployment's pod spec.
// Returns nil if there are no containers.
func GetFirstContainer(deployment *appsv1.Deployment) *corev1.Container {
	if len(deployment.Spec.Template.Spec.Containers) == 0 {
		return nil
	}
	return &deployment.Spec.Template.Spec.Containers[0]
}

// GetContainerByName returns a pointer to the container with the given name.
// Returns nil if no container with that name exists.
func GetContainerByName(deployment *appsv1.Deployment, name string) *corev1.Container {
	for i := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[i].Name == name {
			return &deployment.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

// AddContainerPort adds a port to the container if a port with the same name doesn't exist.
// Returns true if the port was added, false if a port with that name already exists.
func AddContainerPort(container *corev1.Container, port corev1.ContainerPort) bool {
	for _, existingPort := range container.Ports {
		if existingPort.Name == port.Name {
			return false
		}
	}
	container.Ports = append(container.Ports, port)
	return true
}

