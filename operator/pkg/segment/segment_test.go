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

package segment

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/onsi/gomega"
)

func TestGetDefaultWriteKey(t *testing.T) {
	g := gomega.NewWithT(t)
	g.Expect(GetDefaultWriteKey()).To(gomega.BeEmpty(), "should return empty in source (no ldflags)")
}

func TestResolveWriteKey(t *testing.T) {
	tests := []struct {
		name       string
		crKey      string
		defaultKey string
		wantKey    string
		wantSource string
	}{
		{
			name:       "CR key takes precedence",
			crKey:      "cr-key",
			wantKey:    "cr-key",
			wantSource: "cr",
		},
		{
			name:       "CR key takes precedence over default",
			crKey:      "cr-key",
			defaultKey: "build-key",
			wantKey:    "cr-key",
			wantSource: "cr",
		},
		{
			name:       "falls back to default when CR key is empty",
			crKey:      "",
			defaultKey: "build-key",
			wantKey:    "build-key",
			wantSource: "build-time-default",
		},
		{
			name:       "returns empty when neither is set",
			crKey:      "",
			defaultKey: "",
			wantKey:    "",
			wantSource: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)

			gotKey, gotSource := ResolveWriteKey(tt.crKey, tt.defaultKey)
			g.Expect(gotKey).To(gomega.Equal(tt.wantKey))
			g.Expect(gotSource).To(gomega.Equal(tt.wantSource))
		})
	}
}

func TestLogWriteKeyResolution(t *testing.T) {
	log := logr.Discard()

	t.Run("returns false when key is empty", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(LogWriteKeyResolution(log, "", "")).To(gomega.BeFalse())
	})

	t.Run("returns true when key is present", func(t *testing.T) {
		g := gomega.NewWithT(t)
		g.Expect(LogWriteKeyResolution(log, "some-key", "cr")).To(gomega.BeTrue())
	})
}
