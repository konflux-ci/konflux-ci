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

// Package testutil provides shared test utilities for controller tests.
package testutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck // dot imports are standard for Ginkgo tests
	. "github.com/onsi/gomega"    //nolint:staticcheck // dot imports are standard for Gomega matchers

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	securityv1 "github.com/openshift/api/security/v1"
)

// TestEnv holds the shared test environment resources.
type TestEnv struct {
	Ctx         context.Context
	Cancel      context.CancelFunc
	TestEnv     *envtest.Environment
	Cfg         *rest.Config
	K8sClient   client.Client
	ObjectStore *manifests.ObjectStore

	// stopManager is set by StartManager and called by TeardownTestEnv to
	// wait for the suite-level manager's goroutines to fully stop.
	stopManager func()
}

const (
	// EventuallyTimeout is the default timeout for Eventually blocks in controller tests.
	EventuallyTimeout = 30 * time.Second
	// EventuallyPolling is the default polling interval for Eventually blocks in controller tests.
	EventuallyPolling = time.Second
)

var (
	sharedObjectStore     *manifests.ObjectStore
	sharedObjectStoreOnce sync.Once
	sharedObjectStoreErr  error
)

// SetupTestEnv initializes the test environment for Ginkgo tests.
// Call this in BeforeSuite and store the returned TestEnv.
// The basePath should be the relative path from the test package to the operator root
// (e.g., "..", "..", ".." for internal/controller/buildservice/).
func SetupTestEnv(basePath string) *TestEnv {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel := context.WithCancel(context.TODO()) //nolint:gosec // cancel is stored in TestEnv and called by the test teardown

	var err error
	err = konfluxv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = consolev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = securityv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = apiextensionsv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = certmanagerv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = configv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	objectStore, err := manifests.NewObjectStore(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	By("bootstrapping test environment")
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(basePath, "config", "crd", "bases"),
			filepath.Join(basePath, "test", "crds", "cert-manager"),
			filepath.Join(basePath, "test", "crds", "prometheus"),
			filepath.Join(basePath, "test", "crds", "enterprise-contract"),
			filepath.Join(basePath, "test", "crds", "release"),
			filepath.Join(GetGoModuleDir("github.com/openshift/api"), "config", "v1", "zz_generated.crd-manifests"),
			filepath.Join(GetGoModuleDir("github.com/openshift/api"), "console", "v1", "zz_generated.crd-manifests"),
			filepath.Join(GetGoModuleDir("github.com/openshift/api"), "security", "v1", "zz_generated.crd-manifests"),
		},
		ErrorIfCRDPathMissing: true,
	}

	if binDir := GetFirstFoundEnvTestBinaryDir(basePath); binDir != "" {
		testEnv.BinaryAssetsDirectory = binDir
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	return &TestEnv{
		Ctx:         ctx,
		Cancel:      cancel,
		TestEnv:     testEnv,
		Cfg:         cfg,
		K8sClient:   k8sClient,
		ObjectStore: objectStore,
	}
}

// TeardownTestEnv tears down the test environment.
// Call this in AfterSuite.
func TeardownTestEnv(env *TestEnv) {
	By("tearing down the test environment")
	env.Cancel()
	if env.stopManager != nil {
		env.stopManager()
	}
	err := env.TestEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
}

// GetFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
func GetFirstFoundEnvTestBinaryDir(basePath string) string {
	binPath := filepath.Join(basePath, "bin", "k8s")
	entries, err := os.ReadDir(binPath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", binPath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(binPath, entry.Name())
		}
	}
	return ""
}

// GetGoModuleDir returns the directory path of a Go module in the module cache.
func GetGoModuleDir(modulePath string) string {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", modulePath) //nolint:gosec // modulePath is developer-provided at compile time
	output, err := cmd.Output()
	if err != nil {
		logf.Log.Error(err, "Failed to get Go module directory", "module", modulePath)
		return ""
	}
	return strings.TrimSpace(string(output))
}

// GetTestObjectStore returns a shared ObjectStore for non-Ginkgo tests.
// It is initialized once and reused across all tests.
func GetTestObjectStore(t *testing.T) *manifests.ObjectStore {
	t.Helper()
	sharedObjectStoreOnce.Do(func() {
		err := konfluxv1alpha1.AddToScheme(scheme.Scheme)
		if err != nil {
			sharedObjectStoreErr = err
			return
		}
		sharedObjectStore, sharedObjectStoreErr = manifests.NewObjectStore(scheme.Scheme)
	})
	if sharedObjectStoreErr != nil {
		t.Fatalf("failed to create ObjectStore: %v", sharedObjectStoreErr)
	}
	return sharedObjectStore
}

// FindContainer returns a pointer to the container with the given name, or nil if not found.
func FindContainer(containers []corev1.Container, name string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == name {
			return &containers[i]
		}
	}
	return nil
}

// NewTestManager creates a controller-runtime manager suitable for use in envtest suites.
// Metrics server, health probes, and leader election are disabled so the manager
// can be started without claiming ports or acquiring locks.
// SkipNameValidation is set to true to prevent "controller already registered" panics
// that can occur when the same controller name is registered across test runs in the
// same process (consistent with the pattern used in notification-service).
func NewTestManager(env *TestEnv) ctrl.Manager {
	skipNameValidation := true
	mgr, err := ctrl.NewManager(env.Cfg, ctrl.Options{
		Scheme:                 scheme.Scheme,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		LeaderElection:         false,
		Controller: config.Controller{
			SkipNameValidation: &skipNameValidation,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	return mgr
}

// StartManager starts mgr in a goroutine tied to env.Ctx and blocks until the
// informer cache has synced. Must be called after all SetupWithManager calls.
// The returned stop function is stored on env and called automatically by
// TeardownTestEnv to wait for the manager's goroutines to fully stop.
//
// Note: use the suite-level k8sClient (direct API server client) for test assertions,
// not mgr.GetClient() (cache-backed). Asserting against the live API server state
// avoids cache staleness and keeps test setup and assertions on the same client.
// See: https://github.com/konflux-ci/notification-service/tree/main/internal/controller
func StartManager(env *TestEnv, mgr ctrl.Manager) {
	env.stopManager = StartManagerWithContext(env.Ctx, mgr)
}

// DeleteAndWait deletes obj from the cluster if it exists and blocks until it is fully gone.
// It is a no-op when the object is already absent. Any unexpected error fails the test immediately.
func DeleteAndWait(ctx context.Context, c client.Client, obj client.Object) {
	key := client.ObjectKeyFromObject(obj)
	err := c.Get(ctx, key, obj)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	Expect(c.Delete(ctx, obj)).To(Succeed())
	Eventually(func(g Gomega) {
		err := c.Get(ctx, key, obj)
		if errors.IsNotFound(err) {
			return // object is gone: success
		}
		// Propagate unexpected errors (e.g. network failure, unauthorized) immediately
		// rather than masking them as a generic timeout.
		g.Expect(err).NotTo(HaveOccurred(), "unexpected error while waiting for deletion of %s", key)
		// Re-issue delete in case an in-flight reconcile recreated the object.
		// After the parent CR is gone no new reconciles trigger, so this converges.
		_ = c.Delete(ctx, obj) //nolint:errcheck
		g.Expect(false).To(BeTrue(), "object %s still exists, waiting for deletion", key)
	}).WithTimeout(20 * time.Second).WithPolling(250 * time.Millisecond).Should(Succeed())
}

// DeferCleanupParentAndChildren registers a single DeferCleanup that deletes the parent
// CR first (stopping the reconciler), then deletes orphaned cluster-scoped children
// that envtest's missing GC won't cascade-delete. This avoids the Ginkgo DeferCleanup
// LIFO ordering issue where separate DeferCleanup calls would delete children first
// while the reconciler is still active and recreating them.
func DeferCleanupParentAndChildren(c client.Client, parent client.Object, children ...client.Object) {
	DeferCleanup(func(ctx context.Context) {
		DeleteAndWait(ctx, c, parent)
		for _, child := range children {
			DeleteAndWait(ctx, c, child)
		}
	})
}

// StartManagerWithContext starts mgr in a goroutine tied to the provided context and
// blocks until the informer cache has synced. It returns a stop function that waits
// (with a bounded timeout of EventuallyTimeout) for mgr.Start() to fully return
// (i.e. all informer goroutines have drained). If the manager does not stop within
// the timeout, the stop function fails the test with a diagnostic message.
// Callers must cancel ctx and then call the returned stop function to ensure clean
// shutdown — typically via DeferCleanup. Use this for per-test managers whose
// lifecycle should be shorter than the suite (e.g. stop them via DeferCleanup when
// different tests need different reconciler configurations).
func StartManagerWithContext(ctx context.Context, mgr ctrl.Manager) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
	// Use a dedicated timeout context for cache sync so that WaitForCacheSync —
	// a blocking call — has a hard upper bound. The outer ctx has no deadline
	// (it is a WithCancel of context.TODO()), so without this the call could
	// block indefinitely and Eventually's timeout would never fire.
	syncCtx, syncCancel := context.WithTimeout(ctx, EventuallyTimeout)
	defer syncCancel()
	Expect(mgr.GetCache().WaitForCacheSync(syncCtx)).To(BeTrue())

	return func() {
		select {
		case <-done:
		case <-time.After(EventuallyTimeout):
			Fail("manager did not stop within " + EventuallyTimeout.String() + " after context cancellation")
		}
	}
}
