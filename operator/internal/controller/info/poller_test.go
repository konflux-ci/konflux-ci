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

package info

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

func TestVersionPoller_FiresEventOnVersionChange(t *testing.T) {
	g := gomega.NewWithT(t)

	mockDiscovery := &MockDiscoveryClient{}
	mockDiscovery.SetVersion("v1.29.0")

	clusterInfo, err := clusterinfo.DetectWithClient(mockDiscovery)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(clusterInfo).NotTo(gomega.BeNil())

	eventCh := make(chan event.TypedGenericEvent[client.Object], 1)
	poller := &VersionPoller{
		ClusterInfo:  clusterInfo,
		Interval:     10 * time.Millisecond,
		EventChannel: eventCh,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = poller.Start(ctx)
	}()

	// No event yet (version unchanged). Consistently also gives the poller time to set lastVersion and run initial ticks.
	g.Consistently(eventCh, 30*time.Millisecond, 10*time.Millisecond).ShouldNot(gomega.Receive())

	// Simulate cluster upgrade
	mockDiscovery.SetVersion("v1.30.0")

	// Expect event within a short time (next poll)
	var e event.TypedGenericEvent[client.Object]
	g.Eventually(eventCh).WithTimeout(1 * time.Second).Should(gomega.Receive(&e))
	g.Expect(e.Object).NotTo(gomega.BeNil())
	g.Expect(e.Object.GetName()).To(gomega.Equal("cluster-upgrade"))
}

func TestVersionPoller_ExitsWhenClusterInfoNil(t *testing.T) {
	g := gomega.NewWithT(t)

	eventCh := make(chan event.TypedGenericEvent[client.Object], 1)
	poller := &VersionPoller{
		ClusterInfo:  nil,
		Interval:     time.Hour,
		EventChannel: eventCh,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- poller.Start(ctx)
	}()

	// Cancel immediately; poller should exit (it blocks on <-ctx.Done() when ClusterInfo is nil)
	cancel()
	g.Expect(<-done).ToNot(gomega.HaveOccurred())
}
