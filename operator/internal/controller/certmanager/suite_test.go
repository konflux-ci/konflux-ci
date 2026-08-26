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

package certmanager

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
)

var (
	ctx         context.Context
	k8sClient   client.Client
	objectStore *manifests.ObjectStore
	testEnv     *testutil.TestEnv
)

func TestCertManager(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CertManager Controller Suite")
}

var _ = BeforeSuite(func() {
	testEnv = testutil.SetupTestEnv("../../..")
	ctx = testEnv.Ctx
	k8sClient = testEnv.K8sClient
	objectStore = testEnv.ObjectStore
})

// startManager creates a per-test manager with the given ClusterInfo
// and registers a DeferCleanup to cancel it after the test.
// A per-test manager is required because each test may wire the reconciler with a different
// ClusterInfo (e.g. OpenShift vs vanilla Kubernetes).
func startManager(clusterInfo *clusterinfo.Info) {
	mgr := testutil.NewTestManager(testEnv)
	Expect((&KonfluxCertManagerReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
		ClusterInfo: clusterInfo,
	}).SetupWithManager(mgr)).To(Succeed())
	mgrCtx, cancel := context.WithCancel(testEnv.Ctx)
	waitForStop := testutil.StartManagerWithContext(mgrCtx, mgr)
	DeferCleanup(func() {
		cancel()
		waitForStop()
	})
}

// createNonOpenShiftClusterInfo returns a ClusterInfo that reports a non-OpenShift cluster.
// This means distributeClusterCABundle defaults to true (the Bundle will be attempted).
func createNonOpenShiftClusterInfo() *clusterinfo.Info {
	ci, _ := clusterinfo.DetectWithClient(&mockDiscoveryClient{
		resources:     map[string]*metav1.APIResourceList{},
		serverVersion: &version.Info{GitVersion: "v1.30.0"},
	})
	return ci
}

// createOpenShiftClusterInfo returns a ClusterInfo that reports an OpenShift cluster.
// This means distributeClusterCABundle defaults to false (native CA injection is used).
func createOpenShiftClusterInfo() *clusterinfo.Info {
	ci, _ := clusterinfo.DetectWithClient(&mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			"config.openshift.io/v1": {
				APIResources: []metav1.APIResource{
					{Kind: "ClusterVersion"},
					{Kind: "Infrastructure"},
				},
			},
		},
		serverVersion: &version.Info{GitVersion: "v4.16.0"},
	})
	return ci
}

type mockDiscoveryClient struct {
	resources     map[string]*metav1.APIResourceList
	serverVersion *version.Info
}

func (m *mockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if r, ok := m.resources[groupVersion]; ok {
		return r, nil
	}
	return nil, errors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *mockDiscoveryClient) ServerVersion() (*version.Info, error) {
	return m.serverVersion, nil
}

var _ = AfterSuite(func() {
	testutil.TeardownTestEnv(testEnv)
})
