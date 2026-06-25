package metricsauth

import (
	"context"
	"fmt"

	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func serviceEndpointSliceSelector(serviceName string) client.MatchingLabels {
	return client.MatchingLabels{discoveryv1.LabelServiceName: serviceName}
}

// WaitForServiceEndpointsReady returns true when an EndpointSlice for the service
// has a ready endpoint on the given port.
func WaitForServiceEndpointsReady(ctx context.Context, c client.Client, namespace, serviceName string, port int32) (bool, error) {
	slices := &discoveryv1.EndpointSliceList{}
	if err := c.List(ctx, slices, client.InNamespace(namespace), serviceEndpointSliceSelector(serviceName)); err != nil {
		return false, err
	}
	return endpointSlicesReadyOnPort(slices.Items, port), nil
}

func readyPodFromService(ctx context.Context, clientset kubernetes.Interface, namespace, serviceName string, port int32) (string, error) {
	slices, err := clientset.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: discoveryv1.LabelServiceName + "=" + serviceName,
	})
	if err != nil {
		return "", fmt.Errorf("list endpoint slices for %s/%s: %w", namespace, serviceName, err)
	}
	for _, slice := range slices.Items {
		if !endpointSliceHasPort(slice, port) {
			continue
		}
		for _, endpoint := range slice.Endpoints {
			if !endpointReady(endpoint) {
				continue
			}
			if endpoint.TargetRef != nil && endpoint.TargetRef.Kind == "Pod" && endpoint.TargetRef.Name != "" {
				return endpoint.TargetRef.Name, nil
			}
		}
	}
	return "", fmt.Errorf("no ready pod found in endpoint slices for %s/%s", namespace, serviceName)
}

func endpointSlicesReadyOnPort(slices []discoveryv1.EndpointSlice, port int32) bool {
	for _, slice := range slices {
		if !endpointSliceHasPort(slice, port) {
			continue
		}
		for _, endpoint := range slice.Endpoints {
			if endpointReady(endpoint) {
				return true
			}
		}
	}
	return false
}

func endpointSliceHasPort(slice discoveryv1.EndpointSlice, port int32) bool {
	for _, p := range slice.Ports {
		if p.Port != nil && *p.Port == port {
			return true
		}
	}
	return false
}

func endpointReady(endpoint discoveryv1.Endpoint) bool {
	return endpoint.Conditions.Ready != nil && *endpoint.Conditions.Ready
}
