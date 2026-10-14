// Package module installs the OpenTofu configuration of an infrastructure application into the data directory.
// The image of the application carries the configuration, built FROM the image of the OpenTofu controller.
// In the data directory it sits next to the state and the provider plugin cache, and OpenTofu can write its
// working files next to it.
package module

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const lockFileName = ".terraform.lock.hcl"

// Install copies the configuration in src into dst, replacing what dst held before, and returns the digest of
// the configuration. It leaves out .terraform directories, the working files of a local "tofu init".
func Install(src, dst string) (string, error) {
	if info, err := os.Stat(src); err != nil {
		return "", err
	} else if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", src)
	}
	if _, err := os.Stat(filepath.Join(src, lockFileName)); err != nil {
		return "", fmt.Errorf("%s holds no OpenTofu configuration with a %s, which pins the providers", src,
			lockFileName)
	}

	staging := dst + ".new"
	if err := os.RemoveAll(staging); err != nil {
		return "", err
	}
	digest, err := copyDir(src, staging)
	if err != nil {
		_ = os.RemoveAll(staging)
		return "", err
	}

	previous := dst + ".old"
	if err := os.RemoveAll(previous); err != nil {
		return "", err
	}
	if err := os.Rename(dst, previous); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(staging, dst); err != nil {
		return "", err
	}
	return digest, os.RemoveAll(previous)
}

// copyDir copies the directories and regular files of src into dst, which must not exist yet, and hashes their
// paths and contents. WalkDir visits them in lexical order, so equal configurations yield equal digests.
func copyDir(src, dst string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir() && d.Name() == ".terraform":
			return filepath.SkipDir
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case d.Type().IsRegular():
			_, _ = fmt.Fprintf(hash, "%s\x00", filepath.ToSlash(rel))
			if err := copyFile(path, target, hash); err != nil {
				return err
			}
			_, _ = hash.Write([]byte{0})
			return nil
		default:
			return fmt.Errorf("%s is neither a directory nor a regular file", path)
		}
	})
	if err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func copyFile(src, dst string, hash io.Writer) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(io.MultiWriter(out, hash), in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
