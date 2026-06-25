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
	"testing"
	"time"

	"github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

func TestTokenRotationBroadcaster_FiresEventsOnInterval(t *testing.T) {
	g := gomega.NewWithT(t)

	broadcaster := &TokenRotationBroadcaster{Interval: 10 * time.Millisecond}
	ch1 := broadcaster.Subscribe()
	ch2 := broadcaster.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = broadcaster.Start(ctx)
		close(done)
	}()

	var evt1, evt2 event.TypedGenericEvent[client.Object]
	g.Eventually(ch1).WithTimeout(time.Second).Should(gomega.Receive(&evt1))
	g.Eventually(ch2).WithTimeout(time.Second).Should(gomega.Receive(&evt2))
	g.Expect(evt1.Object.GetName()).To(gomega.Equal("rotation"))
	g.Expect(evt2.Object.GetName()).To(gomega.Equal("rotation"))

	cancel()
	g.Eventually(done).WithTimeout(time.Second).Should(gomega.BeClosed())

	_, ok := <-ch1
	g.Expect(ok).To(gomega.BeFalse())
	_, ok = <-ch2
	g.Expect(ok).To(gomega.BeFalse())
}

func TestTokenRotationBroadcaster_NeedLeaderElection(t *testing.T) {
	t.Parallel()
	b := NewTokenRotationBroadcaster()
	if !b.NeedLeaderElection() {
		t.Fatal("expected leader election")
	}
}

func TestNewTokenRotationBroadcaster_DefaultInterval(t *testing.T) {
	t.Parallel()
	b := NewTokenRotationBroadcaster()
	if b.Interval != kubernetes.DefaultScrapeTokenRotationInterval {
		t.Fatalf("interval: got %s want %s", b.Interval, kubernetes.DefaultScrapeTokenRotationInterval)
	}
}
