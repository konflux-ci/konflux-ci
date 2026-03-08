package common

import (
	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewProxyPlugin gives the proxyplugin resource template
func NewProxyPlugin(proxyPluginName, proxyPluginNamespace, routeName, routeNamespace string) *toolchainv1alpha1.ProxyPlugin {
	return &toolchainv1alpha1.ProxyPlugin{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: proxyPluginNamespace,
			Name:      proxyPluginName,
		},
		Spec: toolchainv1alpha1.ProxyPluginSpec{
			OpenShiftRouteTargetEndpoint: &toolchainv1alpha1.OpenShiftRouteTarget{
				Namespace: routeNamespace,
				Name:      routeName,
			},
		},
	}
}
