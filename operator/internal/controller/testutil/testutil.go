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

	. "github.com/onsi/ginkgo/v2" //nolint:staticcheck // dot imports are standard for Ginkgo tests
	. "github.com/onsi/gomega"    //nolint:staticcheck // dot imports are standard for Gomega matchers

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"

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
}

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

	ctx, cancel := context.WithCancel(context.TODO())

	var err error
	err = konfluxv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = consolev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = securityv1.Install(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	objectStore, err := manifests.NewObjectStore(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	By("bootstrapping test environment")
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(basePath, "config", "crd", "bases"),
			filepath.Join(basePath, "test", "crds", "cert-manager"),
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
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", modulePath)
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
