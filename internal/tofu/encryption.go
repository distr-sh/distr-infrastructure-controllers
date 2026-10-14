package tofu

import (
	"encoding/json"
	"strings"
)

// templateEscaper escapes the template sequences OpenTofu evaluates in a string of its JSON syntax.
var templateEscaper = strings.NewReplacer("${", "$${", "%{", "%%{")

// EncryptionConfig returns the value of TF_ENCRYPTION that encrypts state and plan files with a key derived
// from passphrase. It is JSON rather than HCL so that encoding/json does the quoting. The references are
// bare traversals, since OpenTofu accepts no template expression there.
func EncryptionConfig(passphrase string) (string, error) {
	method := map[string]any{"method": "method.aes_gcm.byoc", "enforced": true}
	cfg := map[string]any{
		"key_provider": map[string]any{
			"pbkdf2": map[string]any{"byoc": map[string]any{"passphrase": templateEscaper.Replace(passphrase)}},
		},
		"method": map[string]any{
			"aes_gcm": map[string]any{"byoc": map[string]any{"keys": "key_provider.pbkdf2.byoc"}},
		},
		"state": method,
		"plan":  method,
	}
	data, err := json.Marshal(cfg)
	return string(data), err
}
