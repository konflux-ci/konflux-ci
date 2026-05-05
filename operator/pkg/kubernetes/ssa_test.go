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

package kubernetes

import (
	"encoding/json"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestSSAPatch(t *testing.T) {
	t.Run("Type returns ApplyPatchType", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(SSAPatch.Type()).To(gomega.Equal(types.ApplyPatchType))
	})

	t.Run("Data returns valid JSON", func(t *testing.T) {
		g := gomega.NewWithT(t)
		cm := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cm",
				Namespace: "default",
			},
			Data: map[string]string{
				"key": "value",
			},
		}

		data, err := SSAPatch.Data(cm)
		g.Expect(err).NotTo(gomega.HaveOccurred())
		g.Expect(json.Valid(data)).To(gomega.BeTrue())
	})
}
