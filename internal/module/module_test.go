package module

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	godigest "github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/registry/remote/auth"
)

func TestParse(t *testing.T) {
	g := NewWithT(t)

	s, err := Parse("oci://ghcr.io/acme/modules/app?tag=1.2.0")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(s).To(Equal(Source{Repository: "ghcr.io/acme/modules/app", Tag: "1.2.0"}))
	g.Expect(s.Host()).To(Equal("ghcr.io"))
	g.Expect(s.String()).To(Equal("oci://ghcr.io/acme/modules/app?tag=1.2.0"))

	digest := "sha256:" + strings.Repeat("a", 64)
	s, err = Parse("oci://localhost:5000/app//envs/prod?digest=" + digest)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(s).To(Equal(Source{Repository: "localhost:5000/app", Digest: digest, Subdir: "envs/prod"}))
	g.Expect(s.Host()).To(Equal("localhost:5000"))
	g.Expect(s.String()).To(Equal("oci://localhost:5000/app//envs/prod?digest=" + digest))

	s, err = Parse("oci://ghcr.io/acme/app")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(s.Tag).To(Equal("latest"))

	for _, invalid := range []string{
		"ghcr.io/acme/app?tag=1.0.0",
		"git::https://github.com/acme/app",
		"oci://ghcr.io/acme/app?tag=1.0.0&digest=" + digest,
		"oci://ghcr.io/acme/app?version=1.0.0",
		"oci://ghcr.io/acme/app?digest=sha256:short",
		"oci://ghcr.io/acme/app//../outside?tag=1.0.0",
	} {
		_, err := Parse(invalid)
		g.Expect(err).To(HaveOccurred(), invalid)
	}
}

func TestFetch(t *testing.T) {
	g := NewWithT(t)
	reg := newRegistry(t)
	digest := reg.push(t, "modules/app", "1.0.0", ArtifactType, zipOf(t, map[string]string{
		"main.tf":             `terraform { backend "local" {} }`,
		".terraform.lock.hcl": "# lock",
		"envs/prod/main.tf":   "# prod",
	}))

	f := reg.fetcher(t, "modules/app?tag=1.0.0")
	resolved, err := f.Resolve(context.Background())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(resolved).To(Equal(digest))

	dir := filepath.Join(t.TempDir(), "module")
	rootDir, err := f.Fetch(context.Background(), resolved, dir)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rootDir).To(Equal(dir))
	g.Expect(os.ReadFile(filepath.Join(dir, ".terraform.lock.hcl"))).To(Equal([]byte("# lock")))
	g.Expect(os.ReadFile(filepath.Join(dir, "envs", "prod", "main.tf"))).To(Equal([]byte("# prod")))

	f = reg.fetcher(t, "modules/app//envs/prod?tag=1.0.0")
	rootDir, err = f.Fetch(context.Background(), digest, filepath.Join(t.TempDir(), "module"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(filepath.Join(rootDir, "main.tf")).To(BeAnExistingFile())

	f = reg.fetcher(t, "modules/app//envs/missing?tag=1.0.0")
	_, err = f.Fetch(context.Background(), digest, filepath.Join(t.TempDir(), "module"))
	g.Expect(err).To(MatchError(ContainSubstring(`no directory "envs/missing"`)))
}

func TestFetchRejectsOtherArtifacts(t *testing.T) {
	g := NewWithT(t)
	reg := newRegistry(t)
	pkg := zipOf(t, map[string]string{"main.tf": ""})
	digest := reg.push(t, "images/app", "1.0.0", "application/vnd.example.other", pkg)

	_, err := reg.fetcher(t, "images/app?tag=1.0.0").Fetch(context.Background(), digest, filepath.Join(t.TempDir(), "m"))
	g.Expect(err).To(MatchError(ContainSubstring("is not an OpenTofu module package")))
}

func TestExtractRejectsEntriesOutsideThePackage(t *testing.T) {
	g := NewWithT(t)
	for _, name := range []string{"../outside.tf", "/etc/outside.tf"} {
		err := extract(zipOf(t, map[string]string{name: ""}), filepath.Join(t.TempDir(), "m"))
		g.Expect(err).To(MatchError(ContainSubstring("outside of the package")), name)
	}
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type testRegistry struct {
	server    *httptest.Server
	manifests map[string][]byte
	blobs     map[string][]byte
}

func newRegistry(t *testing.T) *testRegistry {
	r := &testRegistry{manifests: map[string][]byte{}, blobs: map[string][]byte{}}
	r.server = httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.server.Close)
	return r
}

func (r *testRegistry) push(t *testing.T, repo, tag, artifactType string, pkg []byte) string {
	t.Helper()
	empty := []byte("{}")
	r.blobs[godigest.FromBytes(empty).String()] = empty
	r.blobs[godigest.FromBytes(pkg).String()] = pkg
	manifest, err := json.Marshal(ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       ocispec.DescriptorEmptyJSON,
		Layers: []ocispec.Descriptor{
			{MediaType: LayerMediaType, Digest: godigest.FromBytes(pkg), Size: int64(len(pkg))},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := godigest.FromBytes(manifest).String()
	r.manifests[repo+":"+tag] = manifest
	r.manifests[repo+"@"+digest] = manifest
	return digest
}

func (r *testRegistry) serve(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/v2/")
	if repo, ref, ok := strings.Cut(path, "/manifests/"); ok {
		sep := ":"
		if strings.HasPrefix(ref, "sha256:") {
			sep = "@"
		}
		manifest, found := r.manifests[repo+sep+ref]
		if !found {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", ocispec.MediaTypeImageManifest)
		w.Header().Set("Docker-Content-Digest", godigest.FromBytes(manifest).String())
		w.Header().Set("Content-Length", strconv.Itoa(len(manifest)))
		if req.Method != http.MethodHead {
			_, _ = w.Write(manifest)
		}
		return
	}
	if _, digest, ok := strings.Cut(path, "/blobs/"); ok {
		blob, found := r.blobs[digest]
		if !found {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(blob)
		return
	}
	http.NotFound(w, req)
}

func (r *testRegistry) fetcher(t *testing.T, repoAndQuery string) *Fetcher {
	t.Helper()
	source, err := Parse("oci://" + strings.TrimPrefix(r.server.URL, "http://") + "/" + repoAndQuery)
	if err != nil {
		t.Fatal(err)
	}
	f, err := NewFetcher(source, auth.EmptyCredential)
	if err != nil {
		t.Fatal(err)
	}
	f.repo.PlainHTTP = true
	return f
}
