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

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	securityv1 "github.com/openshift/api/security/v1"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/applicationapi"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/buildservice"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/certmanager"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/defaulttenant"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/enterprisecontract"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/imagecontroller"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/info"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/integrationservice"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/internalregistry"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/konflux"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/namespacelister"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/rbac"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/releaseservice"
	"github.com/konflux-ci/konflux-ci/operator/internal/controller/ui"
	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
	"github.com/konflux-ci/konflux-ci/operator/pkg/manifests"
	"github.com/konflux-ci/konflux-ci/operator/pkg/version"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	utilruntime.Must(certmanagerv1.AddToScheme(scheme))
	utilruntime.Must(configv1.Install(scheme))
	utilruntime.Must(consolev1.AddToScheme(scheme))
	utilruntime.Must(securityv1.Install(scheme))

	utilruntime.Must(konfluxv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// runDumpManifests dumps all embedded manifests to stdout.
func runDumpManifests() {
	allManifests, err := manifests.GetAllManifests()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting manifests: %v\n", err)
		os.Exit(1)
	}

	for component, content := range allManifests {
		fmt.Printf("# Component: %s\n", component)
		fmt.Printf("---\n")
		fmt.Print(string(content))
		fmt.Printf("\n")
	}
}

// nolint:gocyclo
func main() {
	// Handle subcommands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "dump-manifests":
			runDumpManifests()
			return
		}
	}

	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog.Info(
		"Konflux Operator",
		"version", version.Version,
		"gitCommit", version.GitCommit,
	)

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Create watchers for metrics and webhooks certificates
	var metricsCertWatcher, webhookCertWatcher *certwatcher.CertWatcher

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		var err error
		webhookCertWatcher, err = certwatcher.New(
			filepath.Join(webhookCertPath, webhookCertName),
			filepath.Join(webhookCertPath, webhookCertKey),
		)
		if err != nil {
			setupLog.Error(err, "Failed to initialize webhook certificate watcher")
			os.Exit(1)
		}

		webhookTLSOpts = append(webhookTLSOpts, func(config *tls.Config) {
			config.GetCertificate = webhookCertWatcher.GetCertificate
		})
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: webhookTLSOpts,
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		var err error
		metricsCertWatcher, err = certwatcher.New(
			filepath.Join(metricsCertPath, metricsCertName),
			filepath.Join(metricsCertPath, metricsCertKey),
		)
		if err != nil {
			setupLog.Error(err, "to initialize metrics certificate watcher", "error", err)
			os.Exit(1)
		}

		metricsServerOptions.TLSOpts = append(metricsServerOptions.TLSOpts, func(config *tls.Config) {
			config.GetCertificate = metricsCertWatcher.GetCertificate
		})
	}

	cfg := ctrl.GetConfigOrDie()

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "ab36bc00.konflux-ci.dev",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Parse all embedded manifests into an ObjectStore
	objectStore, err := manifests.NewObjectStore(scheme)
	if err != nil {
		setupLog.Error(err, "unable to parse embedded manifests")
		os.Exit(1)
	}
	// Detect cluster information (platform, version, capabilities)
	clusterInfo, err := clusterinfo.Detect(cfg)
	if err != nil {
		setupLog.Error(err, "unable to detect cluster info")
		os.Exit(1)
	}
	k8sVer := clusterinfo.UnknownVersion
	if v, err := clusterInfo.K8sVersion(); err == nil && v != nil {
		k8sVer = v.GitVersion
	}
	logFields := []any{
		"platform", clusterInfo.Platform(),
		"k8sVersion", k8sVer,
	}

	if clusterInfo.IsOpenShift() {
		osVersion, err := clusterinfo.GetOpenShiftVersion(context.Background(), mgr.GetClient())
		if err != nil {
			setupLog.V(1).Info("Could not retrieve OpenShift version", "error", err.Error())

		}
		logFields = append(logFields, "openShiftVersion", osVersion)

	}
	setupLog.Info("Detected cluster info", logFields...)

	if err := (&konflux.KonfluxReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ClusterInfo: clusterInfo,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Konflux")
		os.Exit(1)
	}
	if err = (&buildservice.KonfluxBuildServiceReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
		ClusterInfo: clusterInfo,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxBuildService")
		os.Exit(1)
	}
	if err = (&integrationservice.KonfluxIntegrationServiceReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxIntegrationService")
		os.Exit(1)
	}
	if err = (&releaseservice.KonfluxReleaseServiceReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxReleaseService")
		os.Exit(1)
	}
	if err = (&ui.KonfluxUIReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
		ClusterInfo: clusterInfo,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxUI")
		os.Exit(1)
	}
	if err = (&rbac.KonfluxRBACReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxRBAC")
		os.Exit(1)
	}
	if err = (&namespacelister.KonfluxNamespaceListerReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxNamespaceLister")
		os.Exit(1)
	}
	if err = (&enterprisecontract.KonfluxEnterpriseContractReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxEnterpriseContract")
		os.Exit(1)
	}
	if err = (&imagecontroller.KonfluxImageControllerReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxImageController")
		os.Exit(1)
	}
	if err = (&applicationapi.KonfluxApplicationAPIReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxApplicationAPI")
		os.Exit(1)
	}
	if err = (&info.KonfluxInfoReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
		ClusterInfo: clusterInfo,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxInfo")
		os.Exit(1)
	}
	if err = (&certmanager.KonfluxCertManagerReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxCertManager")
		os.Exit(1)
	}
	if err := (&internalregistry.KonfluxInternalRegistryReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxInternalRegistry")
		os.Exit(1)
	}
	if err = (&defaulttenant.KonfluxDefaultTenantReconciler{
		Client:      mgr.GetClient(),
		Scheme:      mgr.GetScheme(),
		ObjectStore: objectStore,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KonfluxDefaultTenant")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if metricsCertWatcher != nil {
		setupLog.Info("Adding metrics certificate watcher to manager")
		if err := mgr.Add(metricsCertWatcher); err != nil {
			setupLog.Error(err, "unable to add metrics certificate watcher to manager")
			os.Exit(1)
		}
	}

	if webhookCertWatcher != nil {
		setupLog.Info("Adding webhook certificate watcher to manager")
		if err := mgr.Add(webhookCertWatcher); err != nil {
			setupLog.Error(err, "unable to add webhook certificate watcher to manager")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
