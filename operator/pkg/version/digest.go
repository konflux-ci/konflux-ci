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

// Package version provides utilities for identifying the running operator version.
package version

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
)

var (
	binaryDigest     string
	binaryDigestOnce sync.Once
	binaryDigestErr  error
)

// GetBinaryDigest returns a truncated SHA-256 digest of the running operator binary.
// The digest is computed once at first call and cached for the lifetime of the process.
// The returned digest is 16 hex characters (64 bits), which is sufficient for uniqueness
// while staying well under Kubernetes label value limits (63 chars).
func GetBinaryDigest() (string, error) {
	binaryDigestOnce.Do(func() {
		binaryDigest, binaryDigestErr = computeBinaryDigest()
	})
	return binaryDigest, binaryDigestErr
}

// computeBinaryDigest computes the SHA-256 digest of the currently running executable.
func computeBinaryDigest() (string, error) {
	// Get the path to the currently running executable
	path, err := os.Executable()
	if err != nil {
		return "", err
	}

	// Open the file for reading
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	// Create a new SHA-256 hasher
	hash := sha256.New()

	// Stream the file content into the hasher
	// io.Copy is memory-efficient as it doesn't load the whole file into RAM
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	// Compute the final digest and return first 16 hex chars
	fullDigest := hex.EncodeToString(hash.Sum(nil))
	return fullDigest[:16], nil
}
