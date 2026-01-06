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

// Package consolelink provides utilities for creating OpenShift ConsoleLink resources.
package consolelink

import (
	_ "embed"
	"net/url"
	"strings"

	consolev1 "github.com/openshift/api/console/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ConsoleLinkName is the name of the ConsoleLink resource.
	ConsoleLinkName = "konflux"
)

// konfluxLogoBase64 is the base64-encoded Konflux logo SVG for the OpenShift console.
// Source: https://konflux-ui-konflux-ui.apps.cluster-9tvtm.9tvtm.sandbox2680.opentlc.com/1b97f096290b78c4435b.svg
//
//go:embed konflux-logo.svg.base64
var konfluxLogoBase64 string

// Build creates a ConsoleLink resource for Konflux in the OpenShift console.
func Build(endpoint *url.URL) *consolev1.ConsoleLink {
	return &consolev1.ConsoleLink{
		TypeMeta: metav1.TypeMeta{
			APIVersion: consolev1.GroupVersion.String(),
			Kind:       "ConsoleLink",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: ConsoleLinkName,
		},
		Spec: consolev1.ConsoleLinkSpec{
			Link: consolev1.Link{
				Href: endpoint.String(),
				Text: "Konflux Console",
			},
			Location: consolev1.ApplicationMenu,
			ApplicationMenu: &consolev1.ApplicationMenuSpec{
				ImageURL: "data:image/svg+xml;base64," + strings.TrimSpace(konfluxLogoBase64),
				Section:  "Konflux",
			},
		},
	}
}
