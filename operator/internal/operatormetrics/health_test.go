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

package operatormetrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestSetKonfluxUp(t *testing.T) {
	SetKonfluxUp(true)
	if v := testutil.ToFloat64(konfluxUp); v != 1 {
		t.Errorf("SetKonfluxUp(true): expected gauge value 1, got %f", v)
	}

	SetKonfluxUp(false)
	if v := testutil.ToFloat64(konfluxUp); v != 0 {
		t.Errorf("SetKonfluxUp(false): expected gauge value 0, got %f", v)
	}
}
