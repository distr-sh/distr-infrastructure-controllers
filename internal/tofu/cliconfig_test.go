package tofu

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestCLIConfigIsEmptyWithoutMirrorAndCredentials(t *testing.T) {
	NewWithT(t).Expect(CLIConfig("", []string{"ghcr.io"}, "", "")).To(BeEmpty())
}

func TestCLIConfig(t *testing.T) {
	g := NewWithT(t)
	config := CLIConfig("registry.example.com/acme/providers", []string{
		"registry.example.com", "ghcr.io",
		"registry.example.com",
	}, "acme", `pa"ss${word}%{x}`)
	g.Expect(config).To(Equal(`provider_installation {
  oci_mirror {
    repository_template = "registry.example.com/acme/providers/${namespace}/${type}"
    include             = ["registry.opentofu.org/*/*"]
  }
  direct {
    exclude = ["registry.opentofu.org/*/*"]
  }
}

oci_credentials "ghcr.io" {
  username = "acme"
  password = "pa\"ss$${word}%%{x}"
}

oci_credentials "registry.example.com" {
  username = "acme"
  password = "pa\"ss$${word}%%{x}"
}
`))
}
