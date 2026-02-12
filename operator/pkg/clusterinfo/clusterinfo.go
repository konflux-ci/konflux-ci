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
	"context"
	"errors"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// UnknownVersion is returned when a version cannot be determined.
	UnknownVersion = "unknown"
)

// Sentinel errors for OpenShift version retrieval
var (
	// ErrNoCompletedVersion is returned when ClusterVersion resource exists but no completed version is found in history
	ErrNoCompletedVersion = errors.New("ClusterVersion has no completed version in status history")
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

// Info holds cluster environment information including platform and capabilities.
type Info struct {
	client   DiscoveryClient
	platform Platform
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
func DetectWithClient(discoveryClient DiscoveryClient) (*Info, error) {
	info := &Info{
		client: discoveryClient,
	}

	// Detect platform
	isOpenShift, err := detectOpenShift(discoveryClient)
	if err != nil {
		return nil, fmt.Errorf("failed to detect platform: %w", err)
	}
	if isOpenShift {
		info.platform = OpenShift
	} else {
		info.platform = Default
	}

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
func (i *Info) K8sVersion() (*version.Info, error) {
	return i.client.ServerVersion()
}

// GetOpenShiftVersion retrieves the currently running OpenShift version using the provided client.
// Returns the version string or UnknownVersion with an error if the version cannot be determined.
// This function should only be called when running on OpenShift (check with Info.IsOpenShift() first).
// It fetches the ClusterVersion resource named "version" and searches through .status.history
// for the first entry with state == Completed, which represents the version actually running on the cluster.
// This ensures we don't report an upgrade target version before it's actually been rolled out.
func GetOpenShiftVersion(ctx context.Context, c client.Client) (string, error) {
	clusterVersion := &configv1.ClusterVersion{}
	if err := c.Get(ctx, types.NamespacedName{Name: "version"}, clusterVersion); err != nil {
		return UnknownVersion, err
	}

	// Find the first completed version in history
	// History is ordered with newest entries first, so we iterate to find the first Completed state
	for i, entry := range clusterVersion.Status.History {
		if entry.State == configv1.CompletedUpdate {
			if entry.Version == "" {
				// Found a Completed entry with empty version
				return UnknownVersion, fmt.Errorf("history[%d] has state=%s but empty version field", i, entry.State)
			}
			return entry.Version, nil
		}
	}

	// No completed version found
	return UnknownVersion, ErrNoCompletedVersion
}

// HasResource checks if a specific resource kind exists in the given API group version.
// Returns true if the resource exists, false if it doesn't exist.
// If an error occurs (e.g., RBAC, network issues), it returns false with the error
// to allow callers to distinguish between "not found" and "check failed".
func (i *Info) HasResource(groupVersion, kind string) (bool, error) {
	has, err := i.HasAllResources(groupVersion, []string{kind})
	if err != nil {
		return false, err
	}
	return has, nil
}

// HasAllResources checks if all specified resource kinds exist in the given API group version.
// Returns true only if ALL specified kinds exist, false if any are missing.
// If an error occurs (e.g., RBAC, network issues), it returns false with the error
// to allow callers to distinguish between "not found" and "check failed".
// This method makes a single API call, making it more efficient than calling HasResource
// multiple times for resources in the same group version.
func (i *Info) HasAllResources(groupVersion string, kinds []string) (bool, error) {
	resourceList, err := i.client.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		// If NotFound, the resources don't exist
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		// Other errors (RBAC, network) should be propagated
		return false, fmt.Errorf("failed to check %s resources: %w", groupVersion, err)
	}

	// Build a set of available resource kinds
	availableKinds := sets.New[string]()
	for _, resource := range resourceList.APIResources {
		availableKinds.Insert(resource.Kind)
	}

	// Check if all required kinds exist
	for _, kind := range kinds {
		if !availableKinds.Has(kind) {
			return false, nil
		}
	}

	return true, nil
}

// HasTekton checks if Tekton Pipelines is installed.
func (i *Info) HasTekton() (bool, error) {
	return i.HasResource("tekton.dev/v1", "Pipeline")
}

// HasCertManager checks if cert-manager is installed by verifying that all required
// cert-manager resources exist. Returns true only if ALL required resources are present:
// - Certificate (cert-manager.io/v1)
// - Issuer (cert-manager.io/v1)
// - ClusterIssuer (cert-manager.io/v1)
//
// This function uses HasAllResources to check all required resources in a single API call.
func (i *Info) HasCertManager() (bool, error) {
	return i.HasAllResources("cert-manager.io/v1", []string{"Certificate", "Issuer", "ClusterIssuer"})
}

// detectOpenShift checks if the operator is running on OpenShift by
// verifying that the ClusterVersion resource exists in the config.openshift.io API group.
func detectOpenShift(discoveryClient DiscoveryClient) (bool, error) {
	resourceList, err := discoveryClient.ServerResourcesForGroupVersion("config.openshift.io/v1")
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
