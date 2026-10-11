package tofu

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// prunePluginCache removes the provider versions from the plugin cache that the working directory does not
// use, after an init linked the providers it uses into its .terraform/providers. The cache keeps one
// directory per hostname/namespace/type/version/os_arch.
func (r *Runner) prunePluginCache() error {
	used := map[string]bool{}
	providersDir := filepath.Join(r.Dir, ".terraform", "providers")
	err := walkPlatforms(providersDir, func(path string) error {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		used[target] = true
		return nil
	})
	if err != nil {
		return err
	}
	cacheDir, err := filepath.EvalSymlinks(r.PluginCacheDir)
	if err != nil {
		return err
	}
	return walkPlatforms(cacheDir, func(path string) error {
		if used[path] {
			return nil
		}
		rel, _ := filepath.Rel(cacheDir, path)
		r.Logger.Info("removing the provider " + filepath.ToSlash(rel) + " from the plugin cache, " +
			"the configuration no longer uses it")
		return os.RemoveAll(path)
	})
}

func walkPlatforms(dir string, fn func(path string) error) error {
	matches, err := filepath.Glob(filepath.Join(dir, "*", "*", "*", "*", "*"))
	if err != nil {
		return err
	}
	for _, path := range matches {
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if !info.IsDir() {
			continue
		}
		if err := fn(path); err != nil {
			return err
		}
	}
	return nil
}
