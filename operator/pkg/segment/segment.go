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

// DefaultWriteKey is the Segment write key baked into production builds.
// Empty in development; set via -ldflags during container image builds.
// Used as a fallback when the KonfluxSegmentBridge CR does not specify a key.
//
// Exported only because Go's -ldflags -X requires it in hermetic builds.
// External packages should use GetDefaultWriteKey() instead.
var DefaultWriteKey = ""

// GetDefaultWriteKey returns the build-time Segment write key.
func GetDefaultWriteKey() string {
	return DefaultWriteKey
}
