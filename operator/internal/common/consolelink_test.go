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
		installed, err := ConsoleLinkCRDInstalled(nil)
		if err != nil {
			t.Fatalf("expected no error for a nil mapper, got %v", err)
		}
		if installed {
			t.Fatal("expected nil mapper to report the CRD absent")
		}
	})

	// A cluster installed with the console capability disabled: ConsoleLink is
	// absent even though IsOpenShift() is true. NoMatch is a definitive answer,
	// so it is absence, not an error.
	t.Run("CRD absent", func(t *testing.T) {
		t.Parallel()
		installed, err := ConsoleLinkCRDInstalled(meta.NewDefaultRESTMapper(nil))
		if err != nil {
			t.Fatalf("expected NoMatch to be reported as absence, got error %v", err)
		}
		if installed {
			t.Fatal("expected absent ConsoleLink CRD to report the CRD absent")
		}
	})

	t.Run("CRD present", func(t *testing.T) {
		t.Parallel()
		gvk := consolev1.GroupVersion.WithKind("ConsoleLink")
		mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{consolev1.GroupVersion})
		mapper.Add(gvk, meta.RESTScopeRoot)
		installed, err := ConsoleLinkCRDInstalled(mapper)
		if err != nil {
			t.Fatalf("expected no error when the CRD is mapped, got %v", err)
		}
		if !installed {
			t.Fatal("expected a mapped ConsoleLink CRD to report the CRD present")
		}
	})

	// Anything other than NoMatch means the lookup did not answer the question.
	// Returning absence here would drop the watch for the process lifetime and,
	// mid-reconcile, make an applied ConsoleLink look like an orphan, so the
	// error has to reach the caller.
	t.Run("non-NoMatch mapping error", func(t *testing.T) {
		t.Parallel()
		discoveryErr := errors.New("discovery unavailable")
		installed, err := ConsoleLinkCRDInstalled(errRESTMapper{err: discoveryErr})
		if !errors.Is(err, discoveryErr) {
			t.Fatalf("expected the mapping error to be returned, got %v", err)
		}
		if installed {
			t.Fatal("expected a failed lookup not to report the CRD present")
		}
	})
}
