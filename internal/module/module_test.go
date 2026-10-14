package module

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestInstallCopiesTheConfiguration(t *testing.T) {
	g := NewWithT(t)
	src := t.TempDir()
	writeFiles(t, src, map[string]string{
		"main.tf":                      "# main",
		".terraform.lock.hcl":          "# lock",
		"templates/index.html.tftpl":   "<html>",
		".terraform/terraform.tfstate": "{}",
	})
	dst := filepath.Join(t.TempDir(), "module")
	writeFiles(t, dst, map[string]string{"removed.tf": "# gone"})

	digest, err := Install(src, dst)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(digest).To(HavePrefix("sha256:"))
	g.Expect(os.ReadFile(filepath.Join(dst, "main.tf"))).To(Equal([]byte("# main")))
	g.Expect(filepath.Join(dst, ".terraform.lock.hcl")).To(BeAnExistingFile())
	g.Expect(filepath.Join(dst, "templates", "index.html.tftpl")).To(BeAnExistingFile())
	g.Expect(filepath.Join(dst, ".terraform")).NotTo(BeAnExistingFile())
	g.Expect(filepath.Join(dst, "removed.tf")).NotTo(BeAnExistingFile())
	g.Expect(dst + ".new").NotTo(BeAnExistingFile())
	g.Expect(dst + ".old").NotTo(BeAnExistingFile())

	again, err := Install(src, dst)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(again).To(Equal(digest))

	writeFiles(t, src, map[string]string{"main.tf": "# changed"})
	changed, err := Install(src, dst)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(changed).NotTo(Equal(digest))
}

func TestInstallRequiresTheLockFile(t *testing.T) {
	g := NewWithT(t)
	src := t.TempDir()
	writeFiles(t, src, map[string]string{"main.tf": ""})
	dst := filepath.Join(t.TempDir(), "module")
	writeFiles(t, dst, map[string]string{"main.tf": "# previous"})

	_, err := Install(src, dst)
	g.Expect(err).To(MatchError(ContainSubstring(".terraform.lock.hcl")))
	g.Expect(os.ReadFile(filepath.Join(dst, "main.tf"))).To(Equal([]byte("# previous")))

	_, err = Install(filepath.Join(src, "missing"), dst)
	g.Expect(err).To(HaveOccurred())
}
