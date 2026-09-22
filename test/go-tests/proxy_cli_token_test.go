package go_tests

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

func TestJwtAudienceContains(t *testing.T) {
	g := NewGomegaWithT(t)

	token := func(aud any) string {
		t.Helper()
		payload, err := json.Marshal(map[string]any{"aud": aud})
		g.Expect(err).NotTo(HaveOccurred())
		return "hdr." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
	}

	ok, err := jwtAudienceContains(token("cli"), "cli")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ok).To(BeTrue())

	ok, err = jwtAudienceContains(token([]string{"cli", "other"}), "cli")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ok).To(BeTrue())

	ok, err = jwtAudienceContains(token("oauth2-proxy"), "cli")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ok).To(BeFalse())

	_, err = jwtAudienceContains("not-a-jwt", "cli")
	g.Expect(err).To(HaveOccurred())
}
