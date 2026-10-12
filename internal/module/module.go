// Package module downloads the OpenTofu configuration of an application from an OCI registry. The
// configuration is a module package as OpenTofu defines it for oci:// module sources: an image manifest with
// the artifact type application/vnd.opentofu.modulepkg and a single archive/zip layer.
package module

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	ArtifactType   = "application/vnd.opentofu.modulepkg"
	LayerMediaType = "archive/zip"

	maxManifestSize  = 4 << 20
	maxPackageSize   = 256 << 20
	maxExtractedSize = 1 << 30
)

// Source is a module source address of the form oci://REGISTRY/REPOSITORY[//SUBDIR][?tag=TAG|?digest=DIGEST],
// like OpenTofu accepts it in module blocks.
type Source struct {
	Repository string
	Tag        string
	Digest     string
	// Subdir is the directory of the root module within the package.
	Subdir string
}

func Parse(source string) (Source, error) {
	rest, ok := strings.CutPrefix(source, "oci://")
	if !ok {
		return Source{}, fmt.Errorf("%q must start with oci://", source)
	}
	address, rawQuery, _ := strings.Cut(rest, "?")
	repository, subdir, _ := strings.Cut(address, "//")
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return Source{}, fmt.Errorf("%q has an invalid query: %w", source, err)
	}
	for key := range query {
		if key != "tag" && key != "digest" {
			return Source{}, fmt.Errorf("%q has the unknown argument %q, only tag and digest are allowed", source, key)
		}
	}
	s := Source{Repository: repository, Tag: query.Get("tag"), Digest: query.Get("digest")}
	switch {
	case s.Tag != "" && s.Digest != "":
		return Source{}, fmt.Errorf("%q must not set both tag and digest", source)
	case s.Tag == "" && s.Digest == "":
		s.Tag = "latest"
	}
	if _, err := registry.ParseReference(s.reference()); err != nil {
		return Source{}, fmt.Errorf("%q is not a valid OCI reference: %w", source, err)
	}
	if subdir != "" {
		s.Subdir = path.Clean(subdir)
		if !filepath.IsLocal(s.Subdir) {
			return Source{}, fmt.Errorf("%q has a subdirectory outside of the package", source)
		}
	}
	return s, nil
}

func (s Source) Host() string {
	host, _, _ := strings.Cut(s.Repository, "/")
	return host
}

func (s Source) String() string {
	address := "oci://" + s.Repository
	if s.Subdir != "" {
		address += "//" + s.Subdir
	}
	if s.Digest != "" {
		return address + "?digest=" + s.Digest
	}
	return address + "?tag=" + s.Tag
}

func (s Source) reference() string {
	if s.Digest != "" {
		return s.Repository + "@" + s.Digest
	}
	return s.Repository + ":" + s.Tag
}

// Fetcher downloads the module package of a source. It authenticates with the credentials passed to it
// for the registry of the source, and otherwise with those of the Docker configuration file, like
// OpenTofu does.
type Fetcher struct {
	source Source
	repo   *remote.Repository
}

func NewFetcher(source Source, credential auth.Credential) (*Fetcher, error) {
	repo, err := remote.NewRepository(source.Repository)
	if err != nil {
		return nil, err
	}
	store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not read the Docker credentials: %w", err)
	}
	ambient := credentials.Credential(store)
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
		Credential: func(ctx context.Context, hostport string) (auth.Credential, error) {
			if credential != auth.EmptyCredential && hostport == source.Host() {
				return credential, nil
			}
			return ambient(ctx, hostport)
		},
	}
	return &Fetcher{source: source, repo: repo}, nil
}

func (f *Fetcher) String() string {
	return f.source.String()
}

func (f *Fetcher) Resolve(ctx context.Context) (string, error) {
	if f.source.Digest != "" {
		return f.source.Digest, nil
	}
	desc, err := f.repo.Resolve(ctx, f.source.Tag)
	if err != nil {
		return "", err
	}
	return desc.Digest.String(), nil
}

// Fetch downloads the package with the manifest digest and extracts it into dir, which must not exist yet.
// It returns the directory of the root module.
func (f *Fetcher) Fetch(ctx context.Context, digest, dir string) (string, error) {
	desc, rc, err := f.repo.FetchReference(ctx, digest)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	if desc.MediaType != ocispec.MediaTypeImageManifest {
		return "", fmt.Errorf("the manifest has the media type %q instead of %q", desc.MediaType,
			ocispec.MediaTypeImageManifest)
	}
	if desc.Size > maxManifestSize {
		return "", fmt.Errorf("the manifest is larger than %d bytes", maxManifestSize)
	}
	data, err := content.ReadAll(rc, desc)
	if err != nil {
		return "", err
	}
	layer, err := packageLayer(data)
	if err != nil {
		return "", err
	}
	if layer.Size > maxPackageSize {
		return "", fmt.Errorf("the package is larger than %d bytes", maxPackageSize)
	}
	pkg, err := content.FetchAll(ctx, f.repo.Blobs(), layer)
	if err != nil {
		return "", err
	}
	if err := extract(pkg, dir); err != nil {
		return "", fmt.Errorf("could not extract the package: %w", err)
	}
	rootDir := f.RootDir(dir)
	if info, err := os.Stat(rootDir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("the package has no directory %q", f.source.Subdir)
	}
	return rootDir, nil
}

// RootDir returns the directory of the root module in a package extracted into dir.
func (f *Fetcher) RootDir(dir string) string {
	return filepath.Join(dir, filepath.FromSlash(f.source.Subdir))
}

func packageLayer(data []byte) (ocispec.Descriptor, error) {
	var manifest ocispec.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("could not parse the manifest: %w", err)
	}
	// Before OCI 1.1 artifacts carried their type as the media type of the config.
	if manifest.ArtifactType != ArtifactType && manifest.Config.MediaType != ArtifactType {
		return ocispec.Descriptor{}, fmt.Errorf("the artifact is not an OpenTofu module package of type %q, "+
			"push it with oras push --artifact-type=%s REFERENCE PACKAGE.zip:%s", ArtifactType, ArtifactType,
			LayerMediaType)
	}
	var layers []ocispec.Descriptor
	for _, l := range manifest.Layers {
		if l.MediaType == LayerMediaType {
			layers = append(layers, l)
		}
	}
	if len(layers) != 1 {
		return ocispec.Descriptor{}, fmt.Errorf("the package has %d layers of type %q instead of exactly one",
			len(layers), LayerMediaType)
	}
	return layers[0], nil
}

// extract unpacks the zip archive into dir. os.Root rejects entries that would end up outside of dir.
func extract(data []byte, dir string) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	budget := int64(maxExtractedSize)
	for _, file := range archive.File {
		name := path.Clean(strings.TrimSuffix(file.Name, "/"))
		if !filepath.IsLocal(name) {
			return fmt.Errorf("%q is outside of the package", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := root.MkdirAll(name, 0o700); err != nil {
				return err
			}
			continue
		}
		if !file.Mode().IsRegular() {
			return fmt.Errorf("%q is not a regular file", file.Name)
		}
		if parent := path.Dir(name); parent != "." {
			if err := root.MkdirAll(parent, 0o700); err != nil {
				return err
			}
		}
		written, err := extractFile(root, name, file, budget)
		if err != nil {
			return err
		}
		budget -= written
	}
	return nil
}

func extractFile(root *os.Root, name string, file *zip.File, budget int64) (int64, error) {
	in, err := file.Open()
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	written, err := io.CopyN(out, in, budget+1)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = out.Close()
		return written, err
	}
	if written > budget {
		_ = out.Close()
		return written, fmt.Errorf("the package extracts to more than %d bytes", maxExtractedSize)
	}
	return written, out.Close()
}
