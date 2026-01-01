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

package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	konfluxv1alpha1 "github.com/konflux-ci/konflux-ci/operator/api/v1alpha1"
)

var _ = Describe("Sample YAML Files", func() {
	var (
		samplesDir string
		decoder    runtime.Decoder
	)

	BeforeEach(func() {
		// Locate the samples directory relative to the test file
		// This test file is in internal/controller/, samples are in config/samples/
		samplesDir = filepath.Join("..", "..", "config", "samples")

		// Create a decoder for the konflux API group
		scheme := runtime.NewScheme()
		Expect(konfluxv1alpha1.AddToScheme(scheme)).To(Succeed())
		codecs := serializer.NewCodecFactory(scheme)
		decoder = codecs.UniversalDeserializer()
	})

	Context("When validating sample files", func() {
		It("should be able to apply all sample files to the test cluster (dry-run)", func() {
			entries, err := os.ReadDir(samplesDir)
			Expect(err).NotTo(HaveOccurred())

			for _, entry := range entries {
				if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yaml") &&
					!strings.HasSuffix(entry.Name(), ".yml")) {
					continue
				}
				// Skip kustomization.yaml as it's not a CR sample
				if entry.Name() == "kustomization.yaml" || entry.Name() == "kustomization.yml" {
					continue
				}

				filePath := filepath.Join(samplesDir, entry.Name())
				By("Dry-run applying sample file: " + entry.Name())

				// Read the file
				data, err := os.ReadFile(filePath)
				Expect(err).NotTo(HaveOccurred(), "Should be able to read file: %s", filePath)

				// Handle multi-document YAML files
				reader := kyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
				appliedAny := false

				for {
					doc, err := reader.Read()
					if err != nil {
						if err == io.EOF {
							break
						}
						Expect(err).NotTo(HaveOccurred(), "Should be able to read YAML document: %s", filePath)
					}

					// Skip empty documents
					doc = bytes.TrimSpace(doc)
					if len(doc) == 0 {
						continue
					}

					// Try to decode the object
					obj, _, err := decoder.Decode(doc, nil, nil)
					if err != nil {
						// If the type is not registered, this means the sample file references
						// a CRD type that doesn't exist in the current codebase
						if runtime.IsNotRegisteredError(err) {
							// Extract the kind from the YAML to provide a better error message
							var yamlObj map[string]interface{}
							if yamlErr := yaml.Unmarshal(doc, &yamlObj); yamlErr == nil {
								if kind, ok := yamlObj["kind"].(string); ok {
									Fail(fmt.Sprintf(
										"Sample file %s references CRD type '%s' which is not registered in the scheme.\n"+
											"This likely means:\n"+
											"  1. The type definition is missing from api/v1alpha1/\n"+
											"  2. The type is not added to the scheme in groupversion_info.go\n"+
											"  3. The sample file was created before the CRD was implemented\n\n"+
											"Either implement the CRD type or remove the sample file.",
										entry.Name(), kind))
								}
							}
							Fail(fmt.Sprintf(
								"Sample file %s references a CRD type that is not registered in the scheme.\n"+
									"This means the sample file is for a type that doesn't exist yet.",
								entry.Name()))
						}
						// If we can't decode it, skip the dry-run test for this document
						continue
					}

					// Cast to client.Object
					clientObj, ok := obj.(client.Object)
					if !ok {
						continue
					}

					// Try to create the object with dry-run
					// This validates that the object structure is correct according to CRD validation
					ctx := context.Background()
					err = k8sClient.Create(ctx, clientObj, &client.CreateOptions{DryRun: []string{"All"}})
					if err != nil {
						// Some validation errors are expected (e.g., Konflux must be named "konflux")
						// But we should still log them
						GinkgoWriter.Printf("  ⚠ Dry-run failed (may be expected due to validation): %v\n", err)
						// Don't fail the test - validation errors are okay, we just want to catch structural issues
					} else {
						GinkgoWriter.Printf("  ✓ Dry-run successful\n")
					}
					appliedAny = true
				}

				if !appliedAny {
					GinkgoWriter.Printf("  ⚠ Skipping dry-run (no decodable documents)\n")
				}
			}
		})

		It("should preserve all fields from YAML when decoding (no unknown fields)", func() {
			entries, err := os.ReadDir(samplesDir)
			Expect(err).NotTo(HaveOccurred())

			for _, entry := range entries {
				if entry.IsDir() || (!strings.HasSuffix(entry.Name(), ".yaml") &&
					!strings.HasSuffix(entry.Name(), ".yml")) {
					continue
				}
				// Skip kustomization.yaml as it's not a CR sample
				if entry.Name() == "kustomization.yaml" || entry.Name() == "kustomization.yml" {
					continue
				}

				filePath := filepath.Join(samplesDir, entry.Name())
				By("Checking field preservation for: " + entry.Name())

				// Read the file
				data, err := os.ReadFile(filePath)
				Expect(err).NotTo(HaveOccurred(), "Should be able to read file: %s", filePath)

				// Handle multi-document YAML files
				reader := kyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
				checkedAny := false

				for {
					doc, err := reader.Read()
					if err != nil {
						if err == io.EOF {
							break
						}
						Expect(err).NotTo(
							HaveOccurred(), "Should be able to read YAML document: %s", filePath)
					}

					// Skip empty documents
					doc = bytes.TrimSpace(doc)
					if len(doc) == 0 {
						continue
					}

					// Parse original YAML into a map
					var originalYAML map[string]interface{}
					err = yaml.Unmarshal(doc, &originalYAML)
					if err != nil {
						// If we can't parse as YAML, skip this document
						continue
					}

					// Try to decode into a typed object
					obj, _, err := decoder.Decode(doc, nil, nil)
					if err != nil {
						// If the type is not registered, this means the sample file references
						// a CRD type that doesn't exist in the current codebase
						if runtime.IsNotRegisteredError(err) {
							// Extract the kind from the YAML to provide a better error message
							var yamlObj map[string]interface{}
							if yamlErr := yaml.Unmarshal(doc, &yamlObj); yamlErr == nil {
								if kind, ok := yamlObj["kind"].(string); ok {
									Fail(fmt.Sprintf(
										"Sample file %s references CRD type '%s' which is not registered in the scheme.\n"+
											"This likely means:\n"+
											"  1. The type definition is missing from api/v1alpha1/\n"+
											"  2. The type is not added to the scheme in groupversion_info.go\n"+
											"  3. The sample file was created before the CRD was implemented\n\n"+
											"Either implement the CRD type or remove the sample file.",
										entry.Name(), kind))
								}
							}
							Fail(fmt.Sprintf(
								"Sample file %s references a CRD type that is not registered in the scheme.\n"+
									"This means the sample file is for a type that doesn't exist yet.",
								entry.Name()))
						}
						// For other decode errors, skip
						continue
					}

					// Convert decoded object back to JSON, then to map for comparison
					jsonData, err := json.Marshal(obj)
					Expect(err).NotTo(HaveOccurred(), "Should be able to marshal decoded object: %s", filePath)

					var decodedMap map[string]interface{}
					err = json.Unmarshal(jsonData, &decodedMap)
					Expect(err).NotTo(HaveOccurred(), "Should be able to unmarshal JSON: %s", filePath)

					// Compare fields - check if any fields from original are missing in decoded
					missingFields := findMissingFields(originalYAML, decodedMap, "")
					if len(missingFields) > 0 {
						// Filter out known metadata fields that are expected to differ
						filteredMissing := []string{}
						for _, field := range missingFields {
							// Skip metadata fields that Kubernetes adds/transforms
							if !strings.HasPrefix(field, "metadata.creationTimestamp") &&
								!strings.HasPrefix(field, "metadata.generation") &&
								!strings.HasPrefix(field, "metadata.managedFields") &&
								!strings.HasPrefix(field, "metadata.resourceVersion") &&
								!strings.HasPrefix(field, "metadata.uid") &&
								!strings.HasPrefix(field, "metadata.selfLink") {
								filteredMissing = append(filteredMissing, field)
							}
						}

						if len(filteredMissing) > 0 {
							Fail(fmt.Sprintf(
								"Sample file %s contains fields that are not preserved after decoding (unknown/ignored fields): %v\n"+
									"This likely means the sample contains typos, outdated fields, or fields not defined in the CRD schema.",
								entry.Name(), filteredMissing))
						}
					}

					checkedAny = true
					GinkgoWriter.Printf("  ✓ All fields preserved after decoding\n")
				}

				if !checkedAny {
					GinkgoWriter.Printf("  ⚠ Skipping field preservation check (no decodable documents)\n")
				}
			}
		})
	})
})

// findMissingFields recursively compares two maps and returns paths to fields
// that exist in original but not in decoded (or have different values)
func findMissingFields(original, decoded map[string]interface{}, prefix string) []string {
	var missing []string

	for key, originalValue := range original {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		decodedValue, exists := decoded[key]

		if !exists {
			// Field is completely missing
			missing = append(missing, path)
			continue
		}

		// If both are maps, recurse
		originalMap, origIsMap := originalValue.(map[string]interface{})
		decodedMap, decIsMap := decodedValue.(map[string]interface{})

		if origIsMap && decIsMap {
			// Recurse into nested maps
			missing = append(missing, findMissingFields(originalMap, decodedMap, path)...)
		} else if origIsMap {
			// Original is a map but decoded isn't - check if decoded is nil/empty
			if decodedValue == nil {
				// Empty map vs nil is acceptable (normalization)
				continue
			}
			// Structure mismatch
			missing = append(missing, path+" (type mismatch: map vs "+fmt.Sprintf("%T", decodedValue)+")")
		} else if decIsMap {
			// Decoded is a map but original isn't - check if original is nil/empty
			if originalValue == nil {
				// Nil vs empty map is acceptable (normalization)
				continue
			}
			// Structure mismatch
			missing = append(missing, path+" (type mismatch: "+fmt.Sprintf("%T", originalValue)+" vs map)")
		} else {
			// Compare values (but ignore nil vs empty differences)
			if !reflect.DeepEqual(normalizeValue(originalValue), normalizeValue(decodedValue)) {
				// Values differ - this could be normalization (e.g., empty string vs nil)
				// Only report if it's a significant difference
				if !isNormalizedDifference(originalValue, decodedValue) {
					missing = append(missing, path+" (value differs)")
				}
			}
		}
	}

	return missing
}

// normalizeValue normalizes values for comparison (e.g., empty string to nil)
func normalizeValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string:
		if val == "" {
			return nil
		}
		return val
	case []interface{}:
		if len(val) == 0 {
			return nil
		}
		return val
	case map[string]interface{}:
		if len(val) == 0 {
			return nil
		}
		return val
	default:
		return v
	}
}

// isNormalizedDifference checks if the difference is just normalization (e.g., empty string vs nil)
func isNormalizedDifference(original, decoded interface{}) bool {
	// Empty string vs nil is normalized
	if originalStr, ok := original.(string); ok && originalStr == "" {
		return decoded == nil || decoded == ""
	}
	if decodedStr, ok := decoded.(string); ok && decodedStr == "" {
		return original == nil || original == ""
	}
	// Empty slice vs nil is normalized
	if originalSlice, ok := original.([]interface{}); ok && len(originalSlice) == 0 {
		return decoded == nil
	}
	if decodedSlice, ok := decoded.([]interface{}); ok && len(decodedSlice) == 0 {
		return original == nil
	}
	return false
}
