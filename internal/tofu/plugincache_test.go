package tofu

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func TestPrunePluginCacheKeepsTheProvidersInUse(t *testing.T) {
	g := NewWithT(t)
	cache := t.TempDir()
	dir := t.TempDir()
	for _, version := range []string{"3.6.0", "3.7.2"} {
		g.Expect(os.MkdirAll(filepath.Join(cache, "registry.opentofu.org/hashicorp/random", version, "linux_amd64"),
			0o700)).To(Succeed())
	}
	link := filepath.Join(dir, ".terraform/providers/registry.opentofu.org/hashicorp/random/3.7.2/linux_amd64")
	g.Expect(os.MkdirAll(filepath.Dir(link), 0o700)).To(Succeed())
	g.Expect(os.Symlink(filepath.Join(cache, "registry.opentofu.org/hashicorp/random/3.7.2/linux_amd64"), link)).
		To(Succeed())

	r := &Runner{Dir: dir, PluginCacheDir: cache, Logger: zap.NewNop()}
	g.Expect(r.prunePluginCache()).To(Succeed())
	g.Expect(filepath.Join(cache, "registry.opentofu.org/hashicorp/random/3.7.2/linux_amd64")).To(BeADirectory())
	g.Expect(filepath.Join(cache, "registry.opentofu.org/hashicorp/random/3.6.0/linux_amd64")).
		NotTo(BeAnExistingFile())
}
