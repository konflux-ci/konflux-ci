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

package clusterinfo

import (
	"errors"
	"testing"

	"github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
)

// mockDiscoveryClient implements DiscoveryClient for testing.
type mockDiscoveryClient struct {
	resources      map[string]*metav1.APIResourceList
	resourceErrors map[string]error
	serverVersion  *version.Info
	versionErr     error
}

func (m *mockDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	// Check for specific errors first
	if m.resourceErrors != nil {
		if err, ok := m.resourceErrors[groupVersion]; ok {
			return nil, err
		}
	}
	if r, ok := m.resources[groupVersion]; ok {
		return r, nil
	}
	// Return a NotFound error by default (simulates missing API group)
	return nil, apierrors.NewNotFound(schema.GroupResource{Group: groupVersion}, "")
}

func (m *mockDiscoveryClient) ServerVersion() (*version.Info, error) {
	if m.versionErr != nil {
		return nil, m.versionErr
	}
	return m.serverVersion, nil
}

func TestDetectWithClient_OpenShift(t *testing.T) {
	g := gomega.NewWithT(t)

	mock := &mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			"config.openshift.io/v1": {
				APIResources: []metav1.APIResource{
					{Kind: "ClusterVersion"},
					{Kind: "Infrastructure"},
				},
			},
		},
		serverVersion: &version.Info{
			GitVersion: "v1.29.4",
			Major:      "1",
			Minor:      "29",
		},
	}

	info, err := DetectWithClient(mock)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(info.Platform()).To(gomega.Equal(OpenShift))
	g.Expect(info.IsOpenShift()).To(gomega.BeTrue())
}

func TestDetectWithClient_Default(t *testing.T) {
	g := gomega.NewWithT(t)

	mock := &mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			// No OpenShift resources
		},
		serverVersion: &version.Info{
			GitVersion: "v1.30.0",
			Major:      "1",
			Minor:      "30",
		},
	}

	info, err := DetectWithClient(mock)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(info.Platform()).To(gomega.Equal(Default))
	g.Expect(info.IsOpenShift()).To(gomega.BeFalse())
}

func TestDetectWithClient_OpenShiftGroupWithoutClusterVersion(t *testing.T) {
	g := gomega.NewWithT(t)

	mock := &mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{
			"config.openshift.io/v1": {
				APIResources: []metav1.APIResource{
					{Kind: "Infrastructure"},
					// No ClusterVersion
				},
			},
		},
		serverVersion: &version.Info{GitVersion: "v1.29.0"},
	}

	info, err := DetectWithClient(mock)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(info.Platform()).To(gomega.Equal(Default), "should return Default when ClusterVersion is missing")
}

func TestDetectWithClient_ServerVersionError(t *testing.T) {
	g := gomega.NewWithT(t)

	mock := &mockDiscoveryClient{
		resources:  map[string]*metav1.APIResourceList{},
		versionErr: errors.New("connection refused"),
	}

	_, err := DetectWithClient(mock)
	g.Expect(err).To(gomega.HaveOccurred())
}

func TestDetectWithClient_PlatformDetectionError(t *testing.T) {
	g := gomega.NewWithT(t)

	// Simulate a non-NotFound error (e.g., network failure, unauthorized)
	mock := &mockDiscoveryClient{
		resources: map[string]*metav1.APIResourceList{},
		resourceErrors: map[string]error{
			"config.openshift.io/v1": apierrors.NewServiceUnavailable("API server unavailable"),
		},
		serverVersion: &version.Info{GitVersion: "v1.29.0"},
	}

	_, err := DetectWithClient(mock)
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("failed to detect platform"))
}

func TestInfo_K8sVersion(t *testing.T) {
	g := gomega.NewWithT(t)

	expectedVersion := &version.Info{
		GitVersion: "v1.29.4",
		Major:      "1",
		Minor:      "29",
		Platform:   "linux/amd64",
	}

	mock := &mockDiscoveryClient{
		resources:     map[string]*metav1.APIResourceList{},
		serverVersion: expectedVersion,
	}

	info, err := DetectWithClient(mock)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(info.K8sVersion().GitVersion).To(gomega.Equal(expectedVersion.GitVersion))
	g.Expect(info.K8sVersion().Major).To(gomega.Equal(expectedVersion.Major))
	g.Expect(info.K8sVersion().Minor).To(gomega.Equal(expectedVersion.Minor))
}

func TestInfo_HasResource(t *testing.T) {
	tests := []struct {
		name         string
		resources    map[string]*metav1.APIResourceList
		groupVersion string
		kind         string
		expected     bool
	}{
		{
			name: "resource exists",
			resources: map[string]*metav1.APIResourceList{
				"apps/v1": {
					APIResources: []metav1.APIResource{
						{Kind: "Deployment"},
						{Kind: "StatefulSet"},
					},
				},
			},
			groupVersion: "apps/v1",
			kind:         "Deployment",
			expected:     true,
		},
		{
			name: "resource does not exist in group",
			resources: map[string]*metav1.APIResourceList{
				"apps/v1": {
					APIResources: []metav1.APIResource{
						{Kind: "Deployment"},
					},
				},
			},
			groupVersion: "apps/v1",
			kind:         "DaemonSet",
			expected:     false,
		},
		{
			name:         "group does not exist",
			resources:    map[string]*metav1.APIResourceList{},
			groupVersion: "nonexistent.io/v1",
			kind:         "SomeResource",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)

			mock := &mockDiscoveryClient{
				resources:     tt.resources,
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			}

			info, err := DetectWithClient(mock)
			g.Expect(err).NotTo(gomega.HaveOccurred())

			result, err := info.HasResource(tt.groupVersion, tt.kind)
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(result).To(gomega.Equal(tt.expected))
		})
	}
}

func TestInfo_HasTekton(t *testing.T) {
	tests := []struct {
		name      string
		resources map[string]*metav1.APIResourceList
		expected  bool
	}{
		{
			name: "Tekton installed",
			resources: map[string]*metav1.APIResourceList{
				"tekton.dev/v1": {
					APIResources: []metav1.APIResource{
						{Kind: "Pipeline"},
						{Kind: "Task"},
						{Kind: "PipelineRun"},
					},
				},
			},
			expected: true,
		},
		{
			name:      "Tekton not installed",
			resources: map[string]*metav1.APIResourceList{},
			expected:  false,
		},
		{
			name: "Tekton group exists but no Pipeline",
			resources: map[string]*metav1.APIResourceList{
				"tekton.dev/v1": {
					APIResources: []metav1.APIResource{
						{Kind: "Task"},
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gomega.NewWithT(t)

			mock := &mockDiscoveryClient{
				resources:     tt.resources,
				serverVersion: &version.Info{GitVersion: "v1.29.0"},
			}

			info, err := DetectWithClient(mock)
			g.Expect(err).NotTo(gomega.HaveOccurred())

			result, err := info.HasTekton()
			g.Expect(err).NotTo(gomega.HaveOccurred())
			g.Expect(result).To(gomega.Equal(tt.expected))
		})
	}
}

func TestPlatform_IsOpenShift(t *testing.T) {
	tests := []struct {
		platform Platform
		expected bool
	}{
		{OpenShift, true},
		{Default, false},
		{Platform("unknown"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.platform), func(t *testing.T) {
			g := gomega.NewWithT(t)
			g.Expect(tt.platform.IsOpenShift()).To(gomega.Equal(tt.expected))
		})
	}
}
