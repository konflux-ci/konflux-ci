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

package clusterinfo

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// DiscoveryClient is an interface for discovering server resources and version.
// This interface is implemented by the Kubernetes discovery client and can be mocked for testing.
type DiscoveryClient interface {
	ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error)
	ServerVersion() (*version.Info, error)
}

// Platform represents the Kubernetes distribution type.
type Platform string

const (
	// OpenShift represents the OpenShift platform.
	OpenShift Platform = "openshift"
	// Default represents any non-OpenShift Kubernetes platform.
	Default Platform = "default"
)

// IsOpenShift returns true if the platform is OpenShift.
func (p Platform) IsOpenShift() bool {
	return p == OpenShift
}

// Info holds cluster environment information including platform, version, and capabilities.
type Info struct {
	platform   Platform
	k8sVersion *version.Info
	client     DiscoveryClient
}

// Detect discovers cluster information by querying the Kubernetes API.
func Detect(cfg *rest.Config) (*Info, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}
	return DetectWithClient(discoveryClient)
}

// DetectWithClient discovers cluster information using the provided discovery client.
// This function is useful for testing with a mock client.
func DetectWithClient(client DiscoveryClient) (*Info, error) {
	info := &Info{
		client: client,
	}

	// Detect platform
	isOpenShift, err := detectOpenShift(client)
	if err != nil {
		return nil, fmt.Errorf("failed to detect platform: %w", err)
	}
	if isOpenShift {
		info.platform = OpenShift
	} else {
		info.platform = Default
	}

	// Get K8s version
	serverVersion, err := client.ServerVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get server version: %w", err)
	}
	info.k8sVersion = serverVersion

	return info, nil
}

// Platform returns the detected Kubernetes distribution.
func (i *Info) Platform() Platform {
	return i.platform
}

// IsOpenShift returns true if running on OpenShift.
func (i *Info) IsOpenShift() bool {
	return i.platform.IsOpenShift()
}

// K8sVersion returns the Kubernetes version info.
func (i *Info) K8sVersion() *version.Info {
	return i.k8sVersion
}

// HasResource checks if a specific resource kind exists in the given API group version.
func (i *Info) HasResource(groupVersion, kind string) (bool, error) {
	resourceList, err := i.client.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		// If the group doesn't exist, the resource doesn't exist
		return false, nil
	}

	for _, resource := range resourceList.APIResources {
		if resource.Kind == kind {
			return true, nil
		}
	}

	return false, nil
}

// HasTekton checks if Tekton Pipelines is installed.
func (i *Info) HasTekton() (bool, error) {
	return i.HasResource("tekton.dev/v1", "Pipeline")
}

// detectOpenShift checks if the operator is running on OpenShift by
// verifying that the ClusterVersion resource exists in the config.openshift.io API group.
func detectOpenShift(client DiscoveryClient) (bool, error) {
	resourceList, err := client.ServerResourcesForGroupVersion("config.openshift.io/v1")
	if err != nil {
		// If the group doesn't exist, we're not on OpenShift
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		// For other errors (network issues, etc.), propagate them
		return false, fmt.Errorf("failed to check for OpenShift API group: %w", err)
	}

	for _, resource := range resourceList.APIResources {
		if resource.Kind == "ClusterVersion" {
			return true, nil
		}
	}

	return false, nil
}
