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

// Package contenthash provides deterministic hash-suffix generators for
// Kubernetes resource names. A content-based suffix ensures that the
// resource name changes whenever its payload changes, which triggers a
// rollout when the resource is referenced by a Deployment volume.
package contenthash

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strings"
)

// suffixLength is the number of hex characters kept from the SHA-256 digest.
const suffixLength = 10

// String returns a short deterministic hash suffix for the given string.
func String(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])[:suffixLength]
}

// Map returns a short deterministic hash suffix for the given map.
// Keys are sorted before hashing so that iteration order does not affect
// the result.
func Map(data map[string]string) string {
	keys := slices.Sorted(maps.Keys(data))

	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+data[k])
	}

	hash := sha256.Sum256([]byte(strings.Join(pairs, "\n")))
	return hex.EncodeToString(hash[:])[:suffixLength]
}
