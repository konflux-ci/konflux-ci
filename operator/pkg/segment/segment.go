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

// ResolveWriteKey determines the effective Segment write key.
// It uses crKey (from the CR spec) if non-empty.
// Returns the key and its source ("cr", or "" if unresolved).
func ResolveWriteKey(crKey string) (key, source string) {
	if crKey != "" {
		return crKey, "cr"
	}
	return "", ""
}

// LogWriteKeyResolution logs how the Segment write key was resolved.
// Returns true if a key is available, false if no key was configured.
func LogWriteKeyResolution(log logr.Logger, key, source string) bool {
	if key == "" {
		log.Info("No Segment write key configured in CR")
		return false
	}
	log.Info("Resolved Segment write key", "source", source)
	return true
}
