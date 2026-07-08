package go_tests

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"path/filepath"
	"runtime"
	"time"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	// renovate: datasource=docker depName=registry.access.redhat.com/ubi10/go-toolset
	echoServerImage = "registry.access.redhat.com/ubi10/go-toolset@sha256:120c56d15943f96f84c7d415432fc82a9212ea58f6cbebf516587fb854d8631a"

	echoConfigMapName  = "echo-server-src"
	echoDeployTimeout  = 120 * time.Second
	echoDeployInterval = 2 * time.Second

	watsonTestAPIKey    = "test-api-key"
	watsonSecretName    = "watson-config"
	watsonEchoNamespace = "test-echo-watson"

	proxyNamespace = "konflux-ui"

	// servingCertVolumeName is the volume name for OpenShift service-serving certs.
	// On OpenShift the service-ca-operator populates the Secret; on Kind the
	// optional Secret is absent and the echo server falls back to self-signed.
	servingCertVolumeName = "serving-cert"
	servingCertMountPath  = "/tls"

	// openShiftServingCertAnnotation triggers the service-ca-operator to generate
	// a TLS Secret for the annotated Service.
	openShiftServingCertAnnotation = "service.beta.openshift.io/serving-cert-secret-name"
)

type endpointTestMode string

const (
	modeEcho endpointTestMode = "echo"
	modeReal endpointTestMode = "real"
	modeSkip endpointTestMode = "skip"
)

type endpointModes struct {
	Kite        endpointTestMode
	KubeArchive endpointTestMode
	Watson      endpointTestMode
}

type echoInstance struct {
	endpointName   string
	mode           *endpointTestMode
	BasePath       string
	Namespace      string
	DeploymentName string
	ServiceName    string
	ServicePort    int32
	ContainerPort  int32
	isEnabled      func(*konfluxv1alpha1.ProxyEndpointsSpec) bool
}

var epModes endpointModes

var kiteEndpoint = echoInstance{
	endpointName:   "kite",
	mode:           &epModes.Kite,
	BasePath:       "/api/k8s/plugins/kite/",
	Namespace:      "konflux-kite",
	DeploymentName: "konflux-kite",
	ServiceName:    "konflux-kite",
	ServicePort:    443,
	ContainerPort:  8443,
	isEnabled:      func(ep *konfluxv1alpha1.ProxyEndpointsSpec) bool { return ep.Kite != nil && ep.Kite.Enabled },
}

var kubearchiveEndpoint = echoInstance{
	endpointName:   "kubearchive",
	mode:           &epModes.KubeArchive,
	BasePath:       "/api/k8s/plugins/kubearchive/",
	Namespace:      "product-kubearchive",
	DeploymentName: "kubearchive-api-server",
	ServiceName:    "kubearchive-api-server",
	ServicePort:    8081,
	ContainerPort:  8081,
	isEnabled: func(ep *konfluxv1alpha1.ProxyEndpointsSpec) bool {
		return ep.KubeArchive != nil && ep.KubeArchive.Enabled
	},
}

var watsonEndpoint = echoInstance{
	endpointName:   "watson",
	mode:           &epModes.Watson,
	BasePath:       "/api/chatbot/",
	Namespace:      watsonEchoNamespace,
	DeploymentName: "echo-watson",
	ServiceName:    "echo-watson",
	ServicePort:    443,
	ContainerPort:  8443,
	isEnabled:      func(ep *konfluxv1alpha1.ProxyEndpointsSpec) bool { return ep.Watson != nil && ep.Watson.Enabled },
}

var echoInstances = []*echoInstance{&kiteEndpoint, &kubearchiveEndpoint, &watsonEndpoint}

func readEchoServerSource() string {
	_, thisFile, _, ok := runtime.Caller(0)
	Expect(ok).To(BeTrue(), "failed to resolve caller path")

	srcPath := filepath.Join(filepath.Dir(thisFile), "..", "echo-server", "main.go")
	data, err := os.ReadFile(srcPath)
	Expect(err).NotTo(HaveOccurred(), "failed to read echo-server/main.go from %s", srcPath)
	return string(data)
}

func resolveEndpoints(k *konfluxv1alpha1.Konflux) *konfluxv1alpha1.ProxyEndpointsSpec {
	if k.Spec.KonfluxUI == nil || k.Spec.KonfluxUI.Spec == nil {
		return nil
	}
	return k.Spec.KonfluxUI.Spec.GetProxy().Endpoints
}

func setupEndpoints(ctx context.Context, cl crclient.Client, ep *konfluxv1alpha1.ProxyEndpointsSpec) {
	src := readEchoServerSource()

	for _, inst := range echoInstances {
		if ep == nil || !inst.isEnabled(ep) {
			*inst.mode = modeSkip
			By(fmt.Sprintf("Skipping %s endpoint (not enabled in Konflux CR)", inst.endpointName))
			continue
		}

		realDeploy := &appsv1.Deployment{}
		err := cl.Get(ctx, crclient.ObjectKey{
			Namespace: inst.Namespace,
			Name:      inst.DeploymentName,
		}, realDeploy)
		if err == nil {
			*inst.mode = modeReal
			By(fmt.Sprintf("Using real %s service at %s/%s", inst.endpointName, inst.Namespace, inst.DeploymentName))
			continue
		}
		Expect(errors.IsNotFound(err)).To(BeTrue(),
			"unexpected error checking for real %s deployment: %v", inst.endpointName, err)

		*inst.mode = modeEcho
		By(fmt.Sprintf("Deploying echo server for %s in %s/%s", inst.endpointName, inst.Namespace, inst.DeploymentName))

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: inst.Namespace}}
		Expect(cl.Create(ctx, ns)).To(Or(Succeed(), MatchError(ContainSubstring("already exists"))))

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      echoConfigMapName,
				Namespace: inst.Namespace,
			},
			Data: map[string]string{"main.go": src},
		}
		Expect(cl.Create(ctx, cm)).To(Or(Succeed(), MatchError(ContainSubstring("already exists"))))

		portEnv := fmt.Sprintf("%d", inst.ContainerPort)
		tlsSecretName := inst.ServiceName + "-tls"

		deploy := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      inst.DeploymentName,
				Namespace: inst.Namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: ptr.To(int32(1)),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": inst.DeploymentName},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"app": inst.DeploymentName},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name:    "echo",
							Image:   echoServerImage,
							Command: []string{"go", "run", "/src/main.go"},
							Env: []corev1.EnvVar{
								{Name: "PORT", Value: portEnv},
								{Name: "GOCACHE", Value: "/tmp/go-cache"},
							},
							Ports: []corev1.ContainerPort{{
								ContainerPort: inst.ContainerPort,
								Protocol:      corev1.ProtocolTCP,
							}},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt32(inst.ContainerPort),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       5,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "echo-src",
									MountPath: "/src",
									ReadOnly:  true,
								},
								{
									Name:      servingCertVolumeName,
									MountPath: servingCertMountPath,
									ReadOnly:  true,
								},
							},
						}},
						Volumes: []corev1.Volume{
							{
								Name: "echo-src",
								VolumeSource: corev1.VolumeSource{
									ConfigMap: &corev1.ConfigMapVolumeSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: echoConfigMapName,
										},
									},
								},
							},
							{
								Name: servingCertVolumeName,
								VolumeSource: corev1.VolumeSource{
									Secret: &corev1.SecretVolumeSource{
										SecretName: tlsSecretName,
										Optional:   ptr.To(true),
									},
								},
							},
						},
					},
				},
			},
		}
		Expect(cl.Create(ctx, deploy)).To(Or(Succeed(), MatchError(ContainSubstring("already exists"))))

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      inst.ServiceName,
				Namespace: inst.Namespace,
				Annotations: map[string]string{
					openShiftServingCertAnnotation: tlsSecretName,
				},
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": inst.DeploymentName},
				Ports: []corev1.ServicePort{{
					Port:       inst.ServicePort,
					TargetPort: intstr.FromInt32(inst.ContainerPort),
					Protocol:   corev1.ProtocolTCP,
				}},
			},
		}
		Expect(cl.Create(ctx, svc)).To(Or(Succeed(), MatchError(ContainSubstring("already exists"))))
	}

	if epModes.Watson == modeEcho {
		By("Creating watson-config secret")
		watsonBasicAuth := base64.StdEncoding.EncodeToString([]byte("apikey:" + watsonTestAPIKey))
		watsonSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      watsonSecretName,
				Namespace: proxyNamespace,
			},
			StringData: map[string]string{
				"API_KEY": watsonBasicAuth,
			},
		}
		Expect(cl.Create(ctx, watsonSecret)).To(Or(Succeed(), MatchError(ContainSubstring("already exists"))))
	}

	By("Waiting for echo server deployments to be ready")
	for _, inst := range echoInstances {
		if *inst.mode != modeEcho {
			continue
		}
		Eventually(func(g Gomega) {
			deploy := &appsv1.Deployment{}
			g.Expect(cl.Get(ctx, crclient.ObjectKey{
				Namespace: inst.Namespace,
				Name:      inst.DeploymentName,
			}, deploy)).To(Succeed())
			g.Expect(deploy.Status.AvailableReplicas).To(BeNumerically(">=", 1),
				"echo server %s/%s not ready", inst.Namespace, inst.DeploymentName)
		}).WithTimeout(echoDeployTimeout).WithPolling(echoDeployInterval).Should(Succeed())
	}
}

func cleanupEchoServers(ctx context.Context, cl crclient.Client) {
	for _, inst := range echoInstances {
		if *inst.mode != modeEcho {
			continue
		}
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: inst.Namespace}}
		_ = cl.Delete(ctx, ns)
	}

	if epModes.Watson == modeEcho {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      watsonSecretName,
				Namespace: proxyNamespace,
			},
		}
		_ = cl.Delete(ctx, secret)
	}
}
