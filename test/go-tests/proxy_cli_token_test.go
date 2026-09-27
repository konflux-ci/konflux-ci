package go_tests

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/konflux-ci/konflux-ci/operator/pkg/dex"
	. "github.com/onsi/gomega"
)

func TestJwtAudienceContains(t *testing.T) {
	token := func(t *testing.T, aud any) string {
		t.Helper()
		g := NewGomegaWithT(t)
		payload, err := json.Marshal(map[string]any{"aud": aud})
		g.Expect(err).NotTo(HaveOccurred())
		return "hdr." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
	}

	t.Run("string audience matches", func(t *testing.T) {
		g := NewGomegaWithT(t)
		ok, err := jwtAudienceContains(token(t, dex.CLIClientID), dex.CLIClientID)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(ok).To(BeTrue())
	})

	t.Run("list audience contains client", func(t *testing.T) {
		g := NewGomegaWithT(t)
		ok, err := jwtAudienceContains(token(t, []string{dex.CLIClientID, "other"}), dex.CLIClientID)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(ok).To(BeTrue())
	})

	t.Run("different audience does not match", func(t *testing.T) {
		g := NewGomegaWithT(t)
		ok, err := jwtAudienceContains(token(t, dex.OAuth2ProxyClientID), dex.CLIClientID)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(ok).To(BeFalse())
	})

	t.Run("invalid token returns error", func(t *testing.T) {
		g := NewGomegaWithT(t)
		_, err := jwtAudienceContains("not-a-jwt", dex.CLIClientID)
		g.Expect(err).To(HaveOccurred())
	})
}
