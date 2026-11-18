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

package ui

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	configv1 "github.com/openshift/api/config/v1"
	appsv1 "k8s.io/api/apps/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/condition"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/testutil"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/ingress"
)

// The OpenShift web console is an optional cluster capability. When it is disabled
// the console.openshift.io CRDs are absent while ClusterVersion — and therefore
// ClusterInfo.IsOpenShift — is still present, so the controller cannot treat
// IsOpenShift() as implying the ConsoleLink CRD exists. This container runs the
// reconciler against a dedicated environment built without that CRD.
//
// The environment, manager and CRs are owned by BeforeAll/AfterAll rather than
// DeferCleanup: the API server is torn down wholesale at the end, so per-object
// cleanup would race the shutdown instead of guarding anything.
var _ = Describe("KonfluxUI on OpenShift without the console capability", Ordered, func() {
	const (
		consoleLinkCRDFile = "consolelinks.console.openshift.io.yaml"
		consoleLessDomain  = "apps.console-less.example.com"
	)

	var (
		absentEnv   *testutil.TestEnv
		stopManager func()
		cancelMgr   context.CancelFunc
	)

	BeforeAll(func() {
		absentEnv = testutil.SetupTestEnv("../../..", testutil.WithoutCRDFiles(consoleLinkCRDFile))

		By("building OpenShift cluster info with the console capability disabled")
		openShiftInfo, err := clusterinfo.DetectWithClient(&mockDiscoveryClient{
			resources: map[string]*metav1.APIResourceList{
				"config.openshift.io/v1": {
					APIResources: []metav1.APIResource{{Kind: "ClusterVersion"}},
				},
			},
			serverVersion: &version.Info{GitVersion: "v1.29.0"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(openShiftInfo.IsOpenShift()).To(BeTrue())

		By("seeding the OpenShift Ingress cluster domain")
		Expect(absentEnv.K8sClient.Create(absentEnv.Ctx, &configv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       configv1.IngressSpec{Domain: consoleLessDomain},
		})).To(Succeed())

		By("starting the reconciler against the console-less cluster")
		mgr := testutil.NewTestManager(absentEnv)
		Expect((&KonfluxUIReconciler{
			Client:      mgr.GetClient(),
			Scheme:      mgr.GetScheme(),
			ObjectStore: absentEnv.ObjectStore,
			ClusterInfo: openShiftInfo,
		}).SetupWithManager(mgr)).To(Succeed())
		var mgrCtx context.Context
		mgrCtx, cancelMgr = context.WithCancel(absentEnv.Ctx)
		stopManager = testutil.StartManagerWithContext(mgrCtx, mgr)
	})

	AfterAll(func() {
		cancelMgr()
		stopManager()
		testutil.TeardownTestEnv(absentEnv)
	})

	It("reconciles and applies the Ingress without the ConsoleLink", func(ctx context.Context) {
		c := absentEnv.K8sClient

		By("creating the KonfluxUI CR")
		Expect(c.Create(ctx, &konfluxv1alpha1.KonfluxUI{
			ObjectMeta: metav1.ObjectMeta{Name: CRName},
		})).To(Succeed())

		// Fails if the ConsoleLink watch keeps the controller from starting:
		// controller-runtime waits for every source to sync before starting
		// workers, so no reconcile runs and no Deployment is ever applied.
		By("waiting for the UI Deployments, proving the controller reconciles at all")
		Eventually(func(g Gomega) {
			g.Expect(c.Get(ctx, types.NamespacedName{
				Name: dexDeploymentName, Namespace: uiNamespace,
			}, &appsv1.Deployment{})).To(Succeed())
			g.Expect(c.Get(ctx, types.NamespacedName{
				Name: proxyDeploymentName, Namespace: uiNamespace,
			}, &appsv1.Deployment{})).To(Succeed())
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

		// Fails if the ConsoleLink apply is attempted: it returns a NoMatch error
		// and reconcileIngress ends the reconcile on ReasonIngressReconcileFailed.
		By("checking the reconcile does not fail on the absent ConsoleLink")
		Eventually(func(g Gomega) {
			got := &konfluxv1alpha1.KonfluxUI{}
			g.Expect(c.Get(ctx, types.NamespacedName{Name: CRName}, got)).To(Succeed())
			ready := apimeta.FindStatusCondition(got.Status.Conditions, constant.ConditionTypeReady)
			g.Expect(ready).NotTo(BeNil())
			g.Expect(ready.Reason).NotTo(Equal(condition.ReasonIngressReconcileFailed))
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())

		By("confirming the Ingress was applied")
		Eventually(func(g Gomega) {
			g.Expect(c.Get(ctx, types.NamespacedName{
				Name: ingress.IngressName, Namespace: uiNamespace,
			}, &networkingv1.Ingress{})).To(Succeed())
		}).WithTimeout(testutil.EventuallyTimeout).WithPolling(testutil.EventuallyPolling).Should(Succeed())
	})
})
