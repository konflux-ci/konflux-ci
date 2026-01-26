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

package dependencies

import (
	"context"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CertManagerCRDNames is a list of cert-manager CRD names that must be present
// for cert-manager to function properly. All CRDs in this list must be installed
// for cert-manager to be considered available.
var CertManagerCRDNames = []string{
	"certificates.cert-manager.io",
	"issuers.cert-manager.io",
	"clusterissuers.cert-manager.io",
}

// IsCertManagerInstalled checks if cert-manager CRDs are installed in the cluster.
// It returns true only if ALL required cert-manager CRDs exist.
// This function uses the Kubernetes client to check for CRD existence directly,
// which is more reliable than trying to create resources and catching errors.
func IsCertManagerInstalled(ctx context.Context, k8sClient client.Client) (bool, error) {
	for _, crdName := range CertManagerCRDNames {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := k8sClient.Get(ctx, client.ObjectKey{Name: crdName}, crd); err != nil {
			if errors.IsNotFound(err) {
				// At least one required CRD is missing
				return false, nil
			}
			// Some other error occurred (e.g., RBAC, network issue)
			return false, err
		}
		// This CRD exists, continue checking the rest
	}
	// All CRDs exist
	return true, nil
}
