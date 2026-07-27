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

// These variables are set at build time via ldflags.
// Default values are "unknown" and will be replaced during the build process.
//
// Version and GitCommit are immutable build identity: they describe which
// source produced this binary and are safe to bake in, since they carry no
// access and are identical for every consumer of a given image.
//
// This is deliberately different from the Segment write key, which is a
// runtime credential rather than build identity. Baking a shared credential
// into the binary would embed it in every published image and make it
// effectively public; instead it is supplied at runtime via the Konflux CR
// (see operator/pkg/segment). Don't add credentials to this file or wire
// them through ldflags — pass them through the CR/runtime config path
// instead.
var (
	// Version is the version of the operator (e.g., "v0.0.1")
	Version = "unknown"
	// GitCommit is the git commit SHA used to build the binary
	GitCommit = "unknown"
)
