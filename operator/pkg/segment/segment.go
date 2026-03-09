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
	"github.com/go-logr/logr"
)

// defaultWriteKey is the Segment write key baked into production builds.
// Empty in development; set via -ldflags during container image builds.
// Used as a fallback when the KonfluxSegmentBridge CR does not specify a key.
var defaultWriteKey = ""

// GetDefaultWriteKey returns the build-time Segment write key.
func GetDefaultWriteKey() string {
	return defaultWriteKey
}

// ResolveWriteKey determines the effective Segment write key.
// It prefers crKey (from the CR spec); if empty, falls back to defaultKey
// (typically the build-time value injected via ldflags).
// Returns the key and its source ("cr", "build-time-default", or "" if unresolved).
func ResolveWriteKey(crKey, defaultKey string) (key, source string) {
	if crKey != "" {
		return crKey, "cr"
	}
	if defaultKey != "" {
		return defaultKey, "build-time-default"
	}
	return "", ""
}

// LogWriteKeyResolution logs how the Segment write key was resolved.
// Returns true if a key is available, false if no key was configured.
func LogWriteKeyResolution(log logr.Logger, key, source string) bool {
	if key == "" {
		log.Info("No Segment write key configured (neither CR nor build-time default)")
		return false
	}
	log.Info("Resolved Segment write key", "source", source)
	return true
}
