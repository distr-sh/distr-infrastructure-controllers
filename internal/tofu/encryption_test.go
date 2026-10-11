package tofu

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
)

func TestEncryptionConfigEscapesTemplates(t *testing.T) {
	g := NewWithT(t)
	cfg, err := EncryptionConfig(`a "quoted" ${passphrase} with %{ directives } and \ backslash`)
	g.Expect(err).NotTo(HaveOccurred())

	var parsed struct {
		KeyProvider struct {
			PBKDF2 map[string]struct {
				Passphrase string `json:"passphrase"`
			} `json:"pbkdf2"`
		} `json:"key_provider"`
	}
	g.Expect(json.Unmarshal([]byte(cfg), &parsed)).To(Succeed())
	g.Expect(parsed.KeyProvider.PBKDF2["byoc"].Passphrase).
		To(Equal(`a "quoted" $${passphrase} with %%{ directives } and \ backslash`))
}
