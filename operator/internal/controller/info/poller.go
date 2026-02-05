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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/konflux-ci/konflux-ci/operator/pkg/clusterinfo"
)

// VersionPoller is a Runnable that polls for cluster version changes and sends
// events to a channel so the KonfluxInfo controller can refresh the info ConfigMap.
type VersionPoller struct {
	ClusterInfo  *clusterinfo.Info
	Interval     time.Duration
	EventChannel chan<- event.TypedGenericEvent[client.Object]
}

// Start runs the poll loop until ctx is cancelled. It does not cache the version;
// each tick fetches the current version from the API server.
func (vp *VersionPoller) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("version-poller")

	if vp.ClusterInfo == nil {
		log.Info("ClusterInfo is nil, version poller exiting")
		<-ctx.Done()
		return nil
	}

	ticker := time.NewTicker(vp.Interval)
	defer ticker.Stop()

	var lastVersion string
	if v, err := vp.ClusterInfo.K8sVersion(); err == nil && v != nil {
		lastVersion = v.GitVersion
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			v, err := vp.ClusterInfo.K8sVersion()
			if err != nil {
				log.V(1).Info("Failed to check cluster version", "error", err)
				continue
			}
			if v == nil {
				continue
			}

			if v.GitVersion != lastVersion {
				log.Info("Cluster version change detected", "old", lastVersion, "new", v.GitVersion)
				lastVersion = v.GitVersion
				// Sentinel object only: channel requires client.Object; handler ignores it and enqueues all KonfluxInfo.
				select {
				case vp.EventChannel <- event.TypedGenericEvent[client.Object]{
					Object: &metav1.PartialObjectMetadata{
						TypeMeta:   metav1.TypeMeta{Kind: "ClusterVersion", APIVersion: "config.openshift.io/v1"},
						ObjectMeta: metav1.ObjectMeta{Name: "cluster-upgrade"},
					},
				}:
				default:
					// Channel full or closed; skip so we don't block the poller
					log.V(1).Info("Version change event channel full, skipping enqueue")
				}
			}
		}
	}
}
