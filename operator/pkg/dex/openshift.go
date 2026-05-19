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

package dex

import (
	corev1 "k8s.io/api/core/v1"
)

const (
	// OpenShiftLoginEnabledEnvVar is the environment variable name to state if OpenShift Login is enabled.
	OpenShiftLoginEnabledEnvVar = "OPENSHIFT_LOGIN_ENABLED"
	// DexCallbackPath is the OAuth callback path used by Dex.
	DexCallbackPath = "/idp/callback"
	// OpenShiftRedirectURIAnnotation annotation to configure the OpenShift's OAuth RedirectURI
	OpenShiftRedirectURIAnnotation = "serviceaccounts.openshift.io/oauth-redirecturi.dex"
)

// OpenShiftLoginEnabledEnv returns the EnvVar that injects
// the OPENSHIFT_LOGIN_ENABLED environment variable from.
func OpenShiftLoginEnabledEnv() corev1.EnvVar {
	return corev1.EnvVar{
		Name:  OpenShiftLoginEnabledEnvVar,
		Value: "true",
	}
}
