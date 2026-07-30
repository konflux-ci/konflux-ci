/*
Copyright 2026 Konflux CI.

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

package tracking

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
)

// These tests exercise real API-server Server-Side Apply for operator-upgrade-style
// applies: shape A then shape B with the same field manager via ApplyOwned.
// That matches operand Deployments the operator previously applied and later
// upgrades when upstream manifests rename or drop merge-keyed list items
// (containers, volumes, env, volumeMounts, ports, initContainers). See #8569.
//
// Fake clients do not model SSA merge-key semantics and are not sufficient here.

const ssaUpgradeNamespace = "ssa-upgrade-orphans"

var (
	ssaEnvOnce   sync.Once
	ssaEnv       *envtest.Environment
	ssaEnvClient client.Client
	ssaEnvErr    error
)

// TestMain only stops envtest if a test started it. Startup stays lazy in
// getSSAEnvtestClient so fake-client unit tests do not require binaries.
func TestMain(m *testing.M) {
	code := m.Run()
	if ssaEnv != nil {
		_ = ssaEnv.Stop()
	}
	os.Exit(code)
}

// getSSAEnvtestClient starts envtest on first use so fake-client unit tests in this
// package do not pay control-plane startup cost or require envtest binaries.
func getSSAEnvtestClient(t *testing.T) client.Client {
	t.Helper()
	ssaEnvOnce.Do(func() {
		scheme := runtime.NewScheme()
		if err := corev1.AddToScheme(scheme); err != nil {
			ssaEnvErr = fmt.Errorf("scheme: %w", err)
			return
		}
		if err := appsv1.AddToScheme(scheme); err != nil {
			ssaEnvErr = fmt.Errorf("scheme: %w", err)
			return
		}

		// Same discovery as controller suites: operator/bin/k8s when present,
		// otherwise envtest uses KUBEBUILDER_ASSETS (set by `make test`).
		testEnv := &envtest.Environment{}
		if binDir := testutil.GetFirstFoundEnvTestBinaryDir("../.."); binDir != "" {
			testEnv.BinaryAssetsDirectory = binDir
		}

		cfg, err := testEnv.Start()
		if err != nil {
			ssaEnvErr = fmt.Errorf("start envtest: %w", err)
			return
		}

		c, err := client.New(cfg, client.Options{Scheme: scheme})
		if err != nil {
			_ = testEnv.Stop()
			ssaEnvErr = fmt.Errorf("client: %w", err)
			return
		}
		ssaEnv = testEnv
		ssaEnvClient = c
	})
	if ssaEnvErr != nil {
		t.Fatalf("%v", ssaEnvErr)
	}
	return ssaEnvClient
}

func newOwnedTrackingClient(c client.Client, owner client.Object) *Client {
	return NewClientWithOwnership(c, OwnershipConfig{
		Owner:             owner,
		OwnerLabelKey:     testOwnerLabel,
		ComponentLabelKey: testComponentLabel,
		Component:         testComponent,
		FieldManager:      testFieldManager,
	})
}

// applyOwnedDeploymentAThenB applies Deployment shape A, then shape B, using
// ApplyOwned for both (same production field manager + ForceOwnership SSA).
// Models an operator upgrade that changes owned manifests. Returns the live
// Deployment after B.
func applyOwnedDeploymentAThenB(
	ctx context.Context,
	c client.Client,
	owner client.Object,
	shapeA, shapeB *appsv1.Deployment,
) (*appsv1.Deployment, error) {
	tc := newOwnedTrackingClient(c, owner)

	if err := tc.ApplyOwned(ctx, shapeA.DeepCopy()); err != nil {
		return nil, fmt.Errorf("apply shape A via ApplyOwned: %w", err)
	}
	if err := tc.ApplyOwned(ctx, shapeB.DeepCopy()); err != nil {
		return nil, fmt.Errorf("apply shape B via ApplyOwned: %w", err)
	}

	live := &appsv1.Deployment{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(shapeB), live); err != nil {
		return nil, fmt.Errorf("get live after B: %w", err)
	}
	return live, nil
}

func setupSSAOwner(t *testing.T, ctx context.Context, nameSuffix string) (client.Client, *corev1.ConfigMap) {
	t.Helper()
	g := NewWithT(t)
	c := getSSAEnvtestClient(t)

	// Shared namespace (created once). Avoids per-test Namespace create/delete:
	// envtest has no namespace controller, so deleted namespaces stick Terminating.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ssaUpgradeNamespace}}
	err := c.Create(ctx, ns)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		g.Expect(err).NotTo(HaveOccurred())
	}

	owner := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ssa-owner-" + nameSuffix,
			Namespace: ssaUpgradeNamespace,
		},
	}
	g.Expect(c.Create(ctx, owner)).To(Succeed())
	t.Cleanup(func() {
		_ = c.Delete(context.Background(), owner)
	})
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(owner), owner)).To(Succeed())
	return c, owner
}

func cleanupDeployment(t *testing.T, c client.Client, name string) {
	t.Helper()
	t.Cleanup(func() {
		_ = c.Delete(context.Background(), &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ssaUpgradeNamespace},
		})
	})
}

func baseDeployment(name string, containers []corev1.Container) *appsv1.Deployment {
	labels := map[string]string{"app": name}
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ssaUpgradeNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: containers,
				},
			},
		},
	}
}

func containerNames(containers []corev1.Container) []string {
	names := make([]string, len(containers))
	for i := range containers {
		names[i] = containers[i].Name
	}
	return names
}

func volumeNames(volumes []corev1.Volume) []string {
	names := make([]string, len(volumes))
	for i := range volumes {
		names[i] = volumes[i].Name
	}
	return names
}

func envNames(env []corev1.EnvVar) []string {
	names := make([]string, len(env))
	for i := range env {
		names[i] = env[i].Name
	}
	return names
}

func mountPaths(mounts []corev1.VolumeMount) []string {
	paths := make([]string, len(mounts))
	for i := range mounts {
		paths[i] = mounts[i].MountPath
	}
	return paths
}

func portKeys(ports []corev1.ContainerPort) []string {
	keys := make([]string, len(ports))
	for i := range ports {
		proto := ports[i].Protocol
		if proto == "" {
			proto = corev1.ProtocolTCP
		}
		keys[i] = fmt.Sprintf("%d/%s", ports[i].ContainerPort, proto)
	}
	return keys
}

// --- Operator-upgrade A→B (same ApplyOwned field manager): ticket merge keys ---

func TestApplyOwnedAThenB_ContainerRename_NginxToCaddy(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	c, owner := setupSSAOwner(t, ctx, "container-rename")
	cleanupDeployment(t, c, "proxy-container-rename")

	shapeA := baseDeployment("proxy-container-rename", []corev1.Container{
		{Name: "nginx", Image: "nginx:1.25"},
	})
	shapeB := baseDeployment("proxy-container-rename", []corev1.Container{
		{Name: "caddy", Image: "caddy:2.8"},
	})

	live, err := applyOwnedDeploymentAThenB(ctx, c, owner, shapeA, shapeB)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(containerNames(live.Spec.Template.Spec.Containers)).To(Equal([]string{"caddy"}),
		"nginx→caddy rename must leave only caddy")
}

func TestApplyOwnedAThenB_ReplaceContainerSet(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	c, owner := setupSSAOwner(t, ctx, "replace-containers")
	cleanupDeployment(t, c, "app-replace-containers")

	shapeA := baseDeployment("app-replace-containers", []corev1.Container{
		{Name: "a", Image: "a:1"},
		{Name: "b", Image: "b:1"},
	})
	shapeB := baseDeployment("app-replace-containers", []corev1.Container{
		{Name: "c", Image: "c:1"},
	})

	live, err := applyOwnedDeploymentAThenB(ctx, c, owner, shapeA, shapeB)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(containerNames(live.Spec.Template.Spec.Containers)).To(Equal([]string{"c"}),
		"replacing container set a,b→c must leave only c")
}

func TestApplyOwnedAThenB_DropOneOfTwoContainers(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	c, owner := setupSSAOwner(t, ctx, "drop-container")
	cleanupDeployment(t, c, "app-drop-container")

	shapeA := baseDeployment("app-drop-container", []corev1.Container{
		{Name: "proxy", Image: "proxy:1"},
		{Name: "sidecar", Image: "sidecar:1"},
	})
	shapeB := baseDeployment("app-drop-container", []corev1.Container{
		{Name: "proxy", Image: "proxy:1"},
	})

	live, err := applyOwnedDeploymentAThenB(ctx, c, owner, shapeA, shapeB)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(containerNames(live.Spec.Template.Spec.Containers)).To(Equal([]string{"proxy"}),
		"dropping sidecar must leave only proxy")
}

func TestApplyOwnedAThenB_VolumeRename(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	c, owner := setupSSAOwner(t, ctx, "volume-rename")
	cleanupDeployment(t, c, "app-volume-rename")

	shapeA := baseDeployment("app-volume-rename", []corev1.Container{
		{Name: "app", Image: "app:v1"},
	})
	shapeA.Spec.Template.Spec.Volumes = []corev1.Volume{
		{Name: "old-vol", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	shapeB := baseDeployment("app-volume-rename", []corev1.Container{
		{Name: "app", Image: "app:v1"},
	})
	shapeB.Spec.Template.Spec.Volumes = []corev1.Volume{
		{Name: "new-vol", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	live, err := applyOwnedDeploymentAThenB(ctx, c, owner, shapeA, shapeB)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(volumeNames(live.Spec.Template.Spec.Volumes)).To(Equal([]string{"new-vol"}),
		"old-vol→new-vol must leave only new-vol")
}

func TestApplyOwnedAThenB_EnvRenameOrDrop(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	c, owner := setupSSAOwner(t, ctx, "env-rename")
	cleanupDeployment(t, c, "app-env-rename")

	shapeA := baseDeployment("app-env-rename", []corev1.Container{
		{
			Name:  "app",
			Image: "app:v1",
			Env: []corev1.EnvVar{
				{Name: "FOO", Value: "a"},
				{Name: "KEEP", Value: "yes"},
			},
		},
	})
	shapeB := baseDeployment("app-env-rename", []corev1.Container{
		{
			Name:  "app",
			Image: "app:v1",
			Env: []corev1.EnvVar{
				{Name: "BAR", Value: "b"},
				{Name: "KEEP", Value: "yes"},
			},
		},
	})

	live, err := applyOwnedDeploymentAThenB(ctx, c, owner, shapeA, shapeB)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(live.Spec.Template.Spec.Containers).To(HaveLen(1))
	g.Expect(envNames(live.Spec.Template.Spec.Containers[0].Env)).To(Equal([]string{"BAR", "KEEP"}),
		"FOO→BAR must leave BAR+KEEP with no stale FOO")
}

func TestApplyOwnedAThenB_VolumeMountPathChange(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	c, owner := setupSSAOwner(t, ctx, "volumemount")
	cleanupDeployment(t, c, "app-volumemount")

	shapeA := baseDeployment("app-volumemount", []corev1.Container{
		{
			Name:  "app",
			Image: "app:v1",
			VolumeMounts: []corev1.VolumeMount{
				{Name: "data", MountPath: "/old"},
			},
		},
	})
	shapeA.Spec.Template.Spec.Volumes = []corev1.Volume{
		{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	shapeB := baseDeployment("app-volumemount", []corev1.Container{
		{
			Name:  "app",
			Image: "app:v1",
			VolumeMounts: []corev1.VolumeMount{
				{Name: "data", MountPath: "/new"},
			},
		},
	})
	shapeB.Spec.Template.Spec.Volumes = []corev1.Volume{
		{Name: "data", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	live, err := applyOwnedDeploymentAThenB(ctx, c, owner, shapeA, shapeB)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(live.Spec.Template.Spec.Containers).To(HaveLen(1))
	g.Expect(mountPaths(live.Spec.Template.Spec.Containers[0].VolumeMounts)).To(Equal([]string{"/new"}),
		"/old→/new mountPath must leave only /new")
}

func TestApplyOwnedAThenB_ContainerPortIdentityChange(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	c, owner := setupSSAOwner(t, ctx, "port")
	cleanupDeployment(t, c, "app-port")

	shapeA := baseDeployment("app-port", []corev1.Container{
		{
			Name:  "app",
			Image: "app:v1",
			Ports: []corev1.ContainerPort{
				{ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
			},
		},
	})
	shapeB := baseDeployment("app-port", []corev1.Container{
		{
			Name:  "app",
			Image: "app:v1",
			Ports: []corev1.ContainerPort{
				{ContainerPort: 8443, Protocol: corev1.ProtocolTCP},
			},
		},
	})

	live, err := applyOwnedDeploymentAThenB(ctx, c, owner, shapeA, shapeB)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(live.Spec.Template.Spec.Containers).To(HaveLen(1))
	g.Expect(portKeys(live.Spec.Template.Spec.Containers[0].Ports)).To(Equal([]string{"8443/TCP"}),
		"8080/TCP→8443/TCP must leave only 8443/TCP")
}

func TestApplyOwnedAThenB_InitContainerRename(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	c, owner := setupSSAOwner(t, ctx, "init-rename")
	cleanupDeployment(t, c, "app-init-rename")

	shapeA := baseDeployment("app-init-rename", []corev1.Container{
		{Name: "app", Image: "app:v1"},
	})
	shapeA.Spec.Template.Spec.InitContainers = []corev1.Container{
		{Name: "old-init", Image: "init:1"},
	}

	shapeB := baseDeployment("app-init-rename", []corev1.Container{
		{Name: "app", Image: "app:v1"},
	})
	shapeB.Spec.Template.Spec.InitContainers = []corev1.Container{
		{Name: "new-init", Image: "init:2"},
	}

	live, err := applyOwnedDeploymentAThenB(ctx, c, owner, shapeA, shapeB)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(containerNames(live.Spec.Template.Spec.InitContainers)).To(Equal([]string{"new-init"}),
		"old-init→new-init must leave only new-init")
	g.Expect(containerNames(live.Spec.Template.Spec.Containers)).To(Equal([]string{"app"}))
}

// --- Controls ---

func TestApplyOwnedAThenB_SameContainerNameInPlaceUpdate(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	c, owner := setupSSAOwner(t, ctx, "inplace")
	cleanupDeployment(t, c, "proxy-inplace")

	shapeA := baseDeployment("proxy-inplace", []corev1.Container{
		{Name: "proxy", Image: "nginx:1.25", Args: []string{"-g", "daemon off;"}},
	})
	shapeB := baseDeployment("proxy-inplace", []corev1.Container{
		{Name: "proxy", Image: "caddy:2.8", Args: []string{"run", "--config", "/etc/caddy/Caddyfile"}},
	})

	live, err := applyOwnedDeploymentAThenB(ctx, c, owner, shapeA, shapeB)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(containerNames(live.Spec.Template.Spec.Containers)).To(Equal([]string{"proxy"}))
	g.Expect(live.Spec.Template.Spec.Containers[0].Image).To(Equal("caddy:2.8"))
	g.Expect(live.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"run", "--config", "/etc/caddy/Caddyfile"}))
}

func TestApplyOwned_EmptyToBOnly(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	c, owner := setupSSAOwner(t, ctx, "empty-to-b")
	cleanupDeployment(t, c, "proxy-empty-to-b")

	tc := newOwnedTrackingClient(c, owner)
	shapeB := baseDeployment("proxy-empty-to-b", []corev1.Container{
		{Name: "caddy", Image: "caddy:2.8"},
	})
	g.Expect(tc.ApplyOwned(ctx, shapeB.DeepCopy())).To(Succeed())

	live := &appsv1.Deployment{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(shapeB), live)).To(Succeed())
	g.Expect(containerNames(live.Spec.Template.Spec.Containers)).To(Equal([]string{"caddy"}))
}

func TestApplyOwnedATwiceThenB(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	c, owner := setupSSAOwner(t, ctx, "a-twice-then-b")
	cleanupDeployment(t, c, "proxy-a-twice-then-b")

	tc := newOwnedTrackingClient(c, owner)
	shapeA := baseDeployment("proxy-a-twice-then-b", []corev1.Container{
		{Name: "nginx", Image: "nginx:1.25"},
	})
	shapeB := baseDeployment("proxy-a-twice-then-b", []corev1.Container{
		{Name: "caddy", Image: "caddy:2.8"},
	})

	g.Expect(tc.ApplyOwned(ctx, shapeA.DeepCopy())).To(Succeed())
	g.Expect(tc.ApplyOwned(ctx, shapeA.DeepCopy())).To(Succeed())
	g.Expect(tc.ApplyOwned(ctx, shapeB.DeepCopy())).To(Succeed())

	live := &appsv1.Deployment{}
	g.Expect(c.Get(ctx, client.ObjectKeyFromObject(shapeB), live)).To(Succeed())
	g.Expect(containerNames(live.Spec.Template.Spec.Containers)).To(Equal([]string{"caddy"}),
		"idempotent ApplyOwned A then B must still equal B")
}
