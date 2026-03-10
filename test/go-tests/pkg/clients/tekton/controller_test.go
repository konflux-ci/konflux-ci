package tekton

import (
	"fmt"
	"testing"

	"github.com/h2non/gock"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/utils/tekton"
	"github.com/stretchr/testify/assert"
)

func TestCosignResultShouldPresence(t *testing.T) {
	assert.False(t, tekton.CosignResult{}.IsPresent())

	assert.False(t, tekton.CosignResult{
		SignatureImageRef: "something",
	}.IsPresent())

	assert.False(t, tekton.CosignResult{
		AttestationImageRef: "something",
	}.IsPresent())

	assert.True(t, tekton.CosignResult{
		SignatureImageRef:   "something",
		AttestationImageRef: "something",
	}.IsPresent())
}

func TestCosignResultMissingFormat(t *testing.T) {
	assert.Equal(t, "prefix.sig and prefix.att", tekton.CosignResult{}.Missing("prefix"))

	assert.Equal(t, "prefix.att", tekton.CosignResult{
		SignatureImageRef: "something",
	}.Missing("prefix"))

	assert.Equal(t, "prefix.sig", tekton.CosignResult{
		AttestationImageRef: "something",
	}.Missing("prefix"))

	assert.Empty(t, tekton.CosignResult{
		SignatureImageRef:   "something",
		AttestationImageRef: "something",
	}.Missing("prefix"))
}

func createHttpMock(urlPath, tag string, response any) {
	s := gock.New("https://quay.io/api/v1")
	if len(tag) > 0 {
		s.MatchParam("specificTag", tag)
	}
	s.Get(urlPath).
		Reply(200).
		JSON(response)
}

func TestFindingCosignResults(t *testing.T) {
	const imageRegistryName = "quay.io"
	const imageRepo = "test/repo"
	const imageTag = "123"
	const imageDigest = "sha256:abc"
	const cosignImageTag = "sha256-abc"
	const imageRef = imageRegistryName + "/" + imageRepo + ":" + imageTag + "@" + imageDigest
	const signatureImageDigest = "sha256:signature"
	const attestationImageDigest = "sha256:attestation"
	const SignatureImageRef = imageRegistryName + "/" + imageRepo + "@" + signatureImageDigest
	const AttestationImageRef = imageRegistryName + "/" + imageRepo + "@" + attestationImageDigest

	cases := []struct {
		Name                    string
		SignatureImagePresent   bool
		AttestationImagePresent bool
		AttestationImageLayers  []any
		ExpectedErrors          []string
		Result                  *tekton.CosignResult
	}{
		{"happy day", true, true, []any{""}, []string{}, &tekton.CosignResult{
			SignatureImageRef:   SignatureImageRef,
			AttestationImageRef: AttestationImageRef,
		}},
		{"happy day multiple attestations", true, true, []any{"", ""}, []string{}, &tekton.CosignResult{
			SignatureImageRef:   SignatureImageRef,
			AttestationImageRef: AttestationImageRef,
		}},
		{"missing signature", false, true, []any{""}, []string{"error when getting signature"}, &tekton.CosignResult{
			SignatureImageRef:   "",
			AttestationImageRef: AttestationImageRef,
		}},
		{"missing attestation", true, false, []any{""}, []string{"error when getting attestation"}, &tekton.CosignResult{
			SignatureImageRef:   SignatureImageRef,
			AttestationImageRef: "",
		}},
		{"missing signature and attestation", false, false, []any{""}, []string{"error when getting attestation", "error when getting signature"}, &tekton.CosignResult{
			SignatureImageRef:   "",
			AttestationImageRef: "",
		}},
		{"missing layers in attestation", true, true, []any{}, []string{"cannot get layers from"}, &tekton.CosignResult{
			SignatureImageRef:   SignatureImageRef,
			AttestationImageRef: "",
		}},
	}

	for _, cse := range cases {
		t.Run(cse.Name, func(t *testing.T) {
			defer gock.Off()

			if cse.SignatureImagePresent {
				createHttpMock(fmt.Sprintf("/repository/%s/tag", imageRepo), cosignImageTag+".sig", &tekton.TagResponse{Tags: []tekton.Tag{{Digest: signatureImageDigest}}})
			}
			if cse.AttestationImagePresent {
				createHttpMock(fmt.Sprintf("/repository/%s/tag", imageRepo), cosignImageTag+".att", &tekton.TagResponse{Tags: []tekton.Tag{{Digest: attestationImageDigest}}})
			}
			createHttpMock(fmt.Sprintf("/repository/%s/manifest/%s", imageRepo, attestationImageDigest), "", &tekton.ManifestResponse{Layers: cse.AttestationImageLayers})

			result, err := tekton.FindCosignResultsForImage(imageRef)

			if err != nil {
				assert.NotEmpty(t, cse.ExpectedErrors)
				for _, errSubstring := range cse.ExpectedErrors {
					assert.Contains(t, err.Error(), errSubstring)
				}
			} else {
				assert.Empty(t, cse.ExpectedErrors)
			}
			assert.Equal(t, cse.Result, result)
		})
	}

}
