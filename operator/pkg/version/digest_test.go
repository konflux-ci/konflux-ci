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

package version

import (
	"testing"
)

func TestGetBinaryDigest(t *testing.T) {
	digest, err := GetBinaryDigest()
	if err != nil {
		t.Fatalf("GetBinaryDigest() returned error: %v", err)
	}

	// Check that we got a non-empty digest
	if digest == "" {
		t.Error("GetBinaryDigest() returned empty digest")
	}

	// Check that the digest is exactly 16 hex characters
	if len(digest) != 16 {
		t.Errorf("GetBinaryDigest() returned digest of length %d, want 16", len(digest))
	}

	// Check that all characters are valid hex
	for _, c := range digest {
		isDigit := c >= '0' && c <= '9'
		isHexLower := c >= 'a' && c <= 'f'
		if !isDigit && !isHexLower {
			t.Errorf("GetBinaryDigest() returned invalid hex character: %c", c)
		}
	}
}

func TestGetBinaryDigestIsCached(t *testing.T) {
	// Call twice and verify we get the same result
	digest1, err1 := GetBinaryDigest()
	if err1 != nil {
		t.Fatalf("First GetBinaryDigest() returned error: %v", err1)
	}

	digest2, err2 := GetBinaryDigest()
	if err2 != nil {
		t.Fatalf("Second GetBinaryDigest() returned error: %v", err2)
	}

	if digest1 != digest2 {
		t.Errorf("GetBinaryDigest() not cached: got %q then %q", digest1, digest2)
	}
}
