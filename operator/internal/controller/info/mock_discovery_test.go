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

package info

import (
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"

	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

// MockDiscoveryClient implements clusterinfo.DiscoveryClient and allows changing
// the reported server version during a test (e.g. to simulate a cluster upgrade).
type MockDiscoveryClient struct {
	lock          sync.Mutex
	serverVersion *version.Info
}

// ServerVersion returns the currently set version. Safe for concurrent use.
func (m *MockDiscoveryClient) ServerVersion() (*version.Info, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.serverVersion, nil
}

// SetVersion updates the reported server version. Use this during a test to
// simulate a version change and assert the VersionPoller sends an event.
func (m *MockDiscoveryClient) SetVersion(v string) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.serverVersion = &version.Info{GitVersion: v}
}

// ServerResourcesForGroupVersion returns NotFound for config.openshift.io/v1 so
// clusterinfo.DetectWithClient treats the cluster as non-OpenShift and succeeds.
func (m *MockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	return nil, apierrors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

var _ clusterinfo.DiscoveryClient = (*MockDiscoveryClient)(nil)
