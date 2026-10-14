package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/distr-sh/distr-infrastructure-controllers/internal/status"
)

const (
	stateFileName  = "terraform.tfstate"
	statusFileName = "status.json"
	plansDirName   = "plans"
	pluginsDirName = "plugins"
	moduleDirName  = "module"
)

// Store is the data directory: the OpenTofu state, the plan in progress, the status of the last run, the
// OpenTofu configuration and the provider plugin cache. It lives on a volume, so that the state and the
// providers survive a restart.
type Store struct {
	dir string
}

type Persisted struct {
	LastRun *status.Run `json:"lastRun,omitempty"`
	// LastAppliedVersion, LastAppliedModule and LastAppliedConfig describe the last successful apply. A plan
	// with changes for the same version, OpenTofu configuration and variables means that someone changed the
	// infrastructure by hand.
	LastAppliedVersion string `json:"lastAppliedVersion,omitempty"`
	LastAppliedModule  string `json:"lastAppliedModule,omitempty"`
	LastAppliedConfig  string `json:"lastAppliedConfig,omitempty"`
}

func Open(dir string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for _, d := range []string{abs, filepath.Join(abs, plansDirName), filepath.Join(abs, pluginsDirName)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("could not create data directory: %w", err)
		}
	}
	return &Store{dir: abs}, nil
}

func (s *Store) Dir() string {
	return s.dir
}

func (s *Store) StatePath() string {
	return filepath.Join(s.dir, stateFileName)
}

func (s *Store) PlanPath() string {
	return filepath.Join(s.dir, plansDirName, "current.tfplan")
}

// PluginCacheDir keeps the providers across restarts and module upgrades, so that OpenTofu downloads every
// provider version only once.
func (s *Store) PluginCacheDir() string {
	return filepath.Join(s.dir, pluginsDirName)
}

// ModuleDir receives a copy of the OpenTofu configuration of the image on every start, and OpenTofu runs in it.
func (s *Store) ModuleDir() string {
	return filepath.Join(s.dir, moduleDirName)
}

func (s *Store) StateExists() bool {
	_, err := os.Stat(s.StatePath())
	return err == nil
}

func (s *Store) ReadPersisted() (Persisted, error) {
	var p Persisted
	data, err := os.ReadFile(filepath.Join(s.dir, statusFileName))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	} else if err != nil {
		return p, err
	}
	err = json.Unmarshal(data, &p)
	return p, err
}

func (s *Store) WritePersisted(p Persisted) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.dir, statusFileName), data)
}

func (s *Store) RemovePlan() {
	_ = os.Remove(s.PlanPath())
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
