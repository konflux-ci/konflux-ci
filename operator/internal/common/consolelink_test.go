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
	"errors"
	"testing"

	consolev1 "github.com/openshift/api/console/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestConsoleLinkCRDInstalled(t *testing.T) {
	t.Parallel()

	t.Run("nil mapper", func(t *testing.T) {
		t.Parallel()
		if ConsoleLinkCRDInstalled(nil) {
			t.Fatal("expected nil mapper to skip the watch")
		}
	})

	// A cluster installed with the console capability disabled: ConsoleLink is
	// absent even though IsOpenShift() is true, so the watch must be skipped.
	t.Run("CRD absent", func(t *testing.T) {
		t.Parallel()
		if ConsoleLinkCRDInstalled(meta.NewDefaultRESTMapper(nil)) {
			t.Fatal("expected absent ConsoleLink CRD to skip the watch")
		}
	})

	t.Run("CRD present", func(t *testing.T) {
		t.Parallel()
		gvk := consolev1.GroupVersion.WithKind("ConsoleLink")
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{consolev1.GroupVersion})
		mapper.Add(gvk, meta.RESTScopeRoot)
		if !ConsoleLinkCRDInstalled(mapper) {
			t.Fatal("expected watch when the ConsoleLink CRD is mapped")
		}
	})

	t.Run("non-NoMatch mapping error", func(t *testing.T) {
		t.Parallel()
		if ConsoleLinkCRDInstalled(errRESTMapper{err: errors.New("discovery unavailable")}) {
			t.Fatal("expected non-NoMatch mapping error to skip the watch")
		}
	})
}
