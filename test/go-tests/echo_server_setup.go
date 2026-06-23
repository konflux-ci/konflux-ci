package go_tests

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"

	"path/filepath"
	"runtime"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	echoServerImage = "registry.access.redhat.com/ubi10/go-toolset@sha256:d5d48915a31c7c774caf7568f7fbe3b25275e042f9f4de73d13fba39f9b2a987"

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

type echoInstance struct {
	Namespace      string
	DeploymentName string
	ServiceName    string
	ServicePort    int32
	ContainerPort  int32
}

var echoInstances = []echoInstance{
	{
		Namespace:      "konflux-kite",
		DeploymentName: "konflux-kite",
		ServiceName:    "konflux-kite",
		ServicePort:    443,
		ContainerPort:  8443,
	},
	{
		Namespace:      "kubearchive",
		DeploymentName: "kubearchive-api-server",
		ServiceName:    "kubearchive-api-server",
		ServicePort:    8081,
		ContainerPort:  8081,
	},
	{
		Namespace:      watsonEchoNamespace,
		DeploymentName: "echo-watson",
		ServiceName:    "echo-watson",
		ServicePort:    443,
		ContainerPort:  8443,
	},
}

func readEchoServerSource() string {
	_, thisFile, _, ok := runtime.Caller(0)
	Expect(ok).To(BeTrue(), "failed to resolve caller path")

	srcPath := filepath.Join(filepath.Dir(thisFile), "..", "echo-server", "main.go")
	data, err := os.ReadFile(srcPath)
	Expect(err).NotTo(HaveOccurred(), "failed to read echo-server/main.go from %s", srcPath)
	return string(data)
}

func deployEchoServers(ctx context.Context, cl crclient.Client) {
	src := readEchoServerSource()

	for _, inst := range echoInstances {
		By(fmt.Sprintf("Creating echo server in %s/%s", inst.Namespace, inst.DeploymentName))

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

	By("Waiting for echo server deployments to be ready")
	for _, inst := range echoInstances {
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
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: inst.Namespace}}
		_ = cl.Delete(ctx, ns)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      watsonSecretName,
			Namespace: proxyNamespace,
		},
	}
	_ = cl.Delete(ctx, secret)
}
