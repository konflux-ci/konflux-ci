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

package common

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
	"github.com/konflux-ci/konflux-ci/operator/internal/constant"
)

// ComponentMetricsEnabled reads the cluster-wide component metrics knob from the Konflux CR.
// Defaults to true when the Konflux CR is absent or the field is unset.
func ComponentMetricsEnabled(ctx context.Context, reader client.Reader) (bool, error) {
	konflux := &konfluxv1alpha1.Konflux{}
	if err := reader.Get(ctx, client.ObjectKey{Name: constant.KonfluxSingletonName}, konflux); err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	return konflux.Spec.IsComponentMetricsEnabled(), nil
}
