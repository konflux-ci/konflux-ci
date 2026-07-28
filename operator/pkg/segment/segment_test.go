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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ResolveWriteKey", func() {
	It("returns the CR key and source when the CR key is set", func() {
		key, source := ResolveWriteKey("cr-key")
		Expect(key).To(Equal("cr-key"))
		Expect(source).To(Equal("cr"))
	})

	It("returns empty key and source when the CR key is not set", func() {
		key, source := ResolveWriteKey("")
		Expect(key).To(BeEmpty())
		Expect(source).To(BeEmpty())
	})
})

var _ = Describe("LogWriteKeyResolution", func() {
	log := logr.Discard()

	It("returns false when the key is empty", func() {
		Expect(LogWriteKeyResolution(log, "", "")).To(BeFalse())
	})

	It("returns true when the key is present", func() {
		Expect(LogWriteKeyResolution(log, "some-key", "cr")).To(BeTrue())
	})
})
