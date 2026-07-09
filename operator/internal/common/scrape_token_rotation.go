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
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
)

// TokenRotationBroadcaster ticks at a fixed interval and notifies subscribers to
// reconcile Prometheus scrape tokens. A single manager Runnable broadcasts to all
// operand controllers that receive a subscribed channel from main.go.
type TokenRotationBroadcaster struct {
	Interval time.Duration

	subscribers []chan event.TypedGenericEvent[client.Object]
}

// NewTokenRotationBroadcaster returns a broadcaster with the default poll interval.
func NewTokenRotationBroadcaster() *TokenRotationBroadcaster {
	return &TokenRotationBroadcaster{
		Interval: kubernetes.DefaultScrapeTokenRotationInterval,
	}
}

// Subscribe registers a buffered channel for rotation events. Call from main.go
// for every subscriber before the manager starts the broadcaster Runnable.
func (b *TokenRotationBroadcaster) Subscribe() chan event.TypedGenericEvent[client.Object] {
	ch := make(chan event.TypedGenericEvent[client.Object], 1)
	b.subscribers = append(b.subscribers, ch)
	return ch
}

// NeedLeaderElection ensures only the elected replica broadcasts rotation events.
func (b *TokenRotationBroadcaster) NeedLeaderElection() bool {
	return true
}

// Start runs until ctx is cancelled.
func (b *TokenRotationBroadcaster) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("scrape-token-rotation")
	interval := b.Interval
	if interval <= 0 {
		interval = kubernetes.DefaultScrapeTokenRotationInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	sentinel := event.TypedGenericEvent[client.Object]{
		Object: &metav1.PartialObjectMetadata{
			TypeMeta:   metav1.TypeMeta{Kind: "ScrapeTokenRotation", APIVersion: "konflux.konflux-ci.dev/v1"},
			ObjectMeta: metav1.ObjectMeta{Name: "rotation"},
		},
	}

	for {
		select {
		case <-ctx.Done():
			b.closeSubscribers()
			return nil
		case <-ticker.C:
			b.broadcast(log, sentinel)
		}
	}
}

func (b *TokenRotationBroadcaster) closeSubscribers() {
	for _, ch := range b.subscribers {
		close(ch)
	}
}

func (b *TokenRotationBroadcaster) broadcast(
	log logr.Logger,
	evt event.TypedGenericEvent[client.Object],
) {
	for _, ch := range b.subscribers {
		select {
		case ch <- evt:
		default:
			log.V(1).Info("Scrape token rotation channel full, skipping enqueue")
		}
	}
}
