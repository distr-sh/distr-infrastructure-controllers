package tofu

import (
	"fmt"
	"slices"
	"strings"
)

// CLIConfig renders an OpenTofu CLI configuration file. With a providerMirror, OpenTofu installs the
// providers of registry.opentofu.org from the OCI repositories <providerMirror>/<namespace>/<type> and all
// others from their origin registry. With a username, OpenTofu authenticates at the OCI registries hosts.
func CLIConfig(providerMirror string, hosts []string, username, password string) string {
	var b strings.Builder
	if providerMirror != "" {
		fmt.Fprintf(&b, `provider_installation {
  oci_mirror {
    repository_template = "%s/${namespace}/${type}"
    include             = ["registry.opentofu.org/*/*"]
  }
  direct {
    exclude = ["registry.opentofu.org/*/*"]
  }
}
`, hclEscape(providerMirror))
	}
	if username != "" {
		slices.Sort(hosts)
		for _, host := range slices.Compact(hosts) {
			fmt.Fprintf(&b, "\noci_credentials \"%s\" {\n  username = \"%s\"\n  password = \"%s\"\n}\n",
				hclEscape(host), hclEscape(username), hclEscape(password))
		}
	}
	return b.String()
}

// hclEscaper escapes a quoted HCL string, in which ${ and %{ would start a template sequence.
var hclEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`,
	"${", "$${", "%{", "%%{")

func hclEscape(s string) string {
	return hclEscaper.Replace(s)
}
