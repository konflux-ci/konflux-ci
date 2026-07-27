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

package kubernetes

const (
	// ServiceMonitorResyncAnnotation is a historical annotation key. Operand reconcilers
	// never write it; OpenShift contract tests and envtest controller tests assert it
	// is absent on operand ServiceMonitors.
	ServiceMonitorResyncAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync"
	// ServiceMonitorResyncReasonAnnotation is a historical annotation key. Operand
	// reconcilers never write it; OpenShift evidence helpers log its value.
	ServiceMonitorResyncReasonAnnotation = "konflux.konflux-ci.dev/metrics-scrape-resync-reason"

	// ServiceMonitorResyncReasonSettleRetry is a historical reason string retained for
	// OpenShift contract test evidence helpers.
	ServiceMonitorResyncReasonSettleRetry = "settle-retry"
)
