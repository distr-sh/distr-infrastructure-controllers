package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/distr-sh/distr-controller-contrib/internal/config"
	"github.com/distr-sh/distr-controller-contrib/internal/status"
	"github.com/distr-sh/distr-controller-contrib/internal/store"
	"github.com/distr-sh/distr-controller-contrib/internal/tofu"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestMain(m *testing.M) {
	if os.Getenv("FAKE_TOFU") != "" {
		os.Exit(fakeTofu(os.Args[1:]))
	}
	os.Exit(m.Run())
}

type fixture struct {
	reconciler *Reconciler
	tracker    *status.Tracker
	store      *store.Store
	logs       *observer.ObservedLogs
	cfg        *config.Config
	runner     *tofu.Runner
}

func newFixture(t *testing.T, cfg config.Config, fakeEnv ...string) *fixture {
	t.Helper()
	return newFixtureWithModule(t, cfg, nil, fakeEnv...)
}

func newFixtureWithModule(t *testing.T, cfg config.Config, module Module, fakeEnv ...string) *fixture {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	if cfg.InstanceName == "" {
		cfg.InstanceName = "acme-prod"
	}
	if cfg.ApplyMode == "" {
		cfg.ApplyMode = config.ApplyModeAuto
	}
	if cfg.AppVersion == "" {
		cfg.AppVersion = "1.0.0"
	}
	if cfg.WorkingDir == "" {
		cfg.WorkingDir = t.TempDir()
	}

	dataStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runner := &tofu.Runner{
		Binary:           os.Args[0],
		Dir:              t.TempDir(),
		Env:              append([]string{"FAKE_TOFU=1"}, fakeEnv...),
		Logger:           logger,
		ProgressInterval: time.Second,
	}
	tracker := &status.Tracker{}
	return &fixture{
		reconciler: New(&cfg, runner, module, dataStore, tracker, logger, runner.Env),
		tracker:    tracker,
		store:      dataStore,
		logs:       logs,
		cfg:        &cfg,
		runner:     runner,
	}
}

func (f *fixture) messages() []string {
	entries := f.logs.All()
	m := make([]string, 0, len(entries))
	for _, e := range entries {
		m = append(m, e.Message)
	}
	return m
}

func TestReconcileAppliesChanges(t *testing.T) {
	g := NewWithT(t)
	f := newFixture(t, config.Config{}, "FAKE_TOFU_PLAN=changes")

	run := f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultSucceeded))
	g.Expect(run.Changes).To(Equal(&status.Changes{Add: 1}))
	g.Expect(f.tracker.Ready()).To(BeTrue())
	g.Expect(f.store.StateExists()).To(BeTrue())
	g.Expect(f.messages()).To(ContainElements(
		"provisioning instance acme-prod for the first time",
		"plan: create random_pet.cluster",
		"apply complete: 1 added, 0 changed, 0 destroyed",
	))

	persisted, err := f.store.ReadPersisted()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(persisted.LastAppliedVersion).To(Equal("1.0.0"))
	g.Expect(persisted.LastRun.Result).To(Equal(status.ResultSucceeded))

	f.cfg.AppVersion = "1.1.0"
	f.reconciler.Reconcile(context.Background())
	g.Expect(f.messages()).To(ContainElement("upgrading the infrastructure from version 1.0.0 to 1.1.0"))

	f.reconciler.Reconcile(context.Background())
	g.Expect(f.logs.FilterMessageSnippet("drift detected").Len()).To(Equal(1))
}

func TestReconcileNoChanges(t *testing.T) {
	g := NewWithT(t)
	f := newFixture(t, config.Config{})

	run := f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultSucceeded))
	g.Expect(f.messages()).To(ContainElement("no changes, the infrastructure is up to date"))
}

func TestReconcilePlanModeOnlyShowsThePlan(t *testing.T) {
	g := NewWithT(t)
	f := newFixture(t, config.Config{ApplyMode: config.ApplyModePlan}, "FAKE_TOFU_PLAN=changes")

	run := f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultPendingChanges))
	g.Expect(f.tracker.Ready()).To(BeTrue())
	g.Expect(f.store.StateExists()).To(BeFalse())
	g.Expect(f.messages()).To(ContainElements(
		"  # random_pet.cluster will be created",
		"changes are pending, set APPLY_MODE=auto to apply them",
	))
}

func TestReconcileFailureMakesUnready(t *testing.T) {
	g := NewWithT(t)
	f := newFixture(t, config.Config{}, "FAKE_TOFU_PLAN=changes", "FAKE_TOFU_APPLY_FAIL=1")

	run := f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultFailed))
	g.Expect(run.Error).To(ContainSubstring("quota exceeded"))
	g.Expect(f.tracker.Ready()).To(BeFalse())
	g.Expect(f.logs.FilterLevelExact(zapcore.ErrorLevel).FilterMessageSnippet("creating failed").Len()).To(Equal(1))

	persisted, err := f.store.ReadPersisted()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(persisted.LastAppliedVersion).To(BeEmpty())
}

func TestReconcilePlanErrorIsReported(t *testing.T) {
	g := NewWithT(t)
	f := newFixture(t, config.Config{}, "FAKE_TOFU_PLAN=error")

	run := f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultFailed))
	g.Expect(run.Error).To(ContainSubstring("No valid credential sources found"))
}

func TestReconcileInitError(t *testing.T) {
	g := NewWithT(t)
	f := newFixture(t, config.Config{}, "FAKE_TOFU_INIT_FAIL=1")

	run := f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultFailed))
	g.Expect(f.messages()).To(ContainElement("Error: Failed to query available provider packages"))
}

func TestReconcileRequiresLocalBackend(t *testing.T) {
	g := NewWithT(t)
	f := newFixture(t, config.Config{}, "FAKE_TOFU_NO_BACKEND=1")

	run := f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultFailed))
	g.Expect(run.Error).To(ContainSubstring(`backend "local" {}`))
}

func TestReconcileDestroyNeedsTheInstanceName(t *testing.T) {
	g := NewWithT(t)
	f := newFixture(t, config.Config{DestroyConfirm: "yes"}, "FAKE_TOFU_PLAN=changes")

	run := f.reconciler.Reconcile(context.Background())
	g.Expect(run.Operation).To(Equal(status.OperationDestroy))
	g.Expect(run.Result).To(Equal(status.ResultFailed))
	g.Expect(run.Error).To(ContainSubstring(`has to be the instance name "acme-prod"`))
	g.Expect(f.logs.FilterMessageSnippet("planning").Len()).To(BeZero())
}

func TestReconcileDestroy(t *testing.T) {
	g := NewWithT(t)
	f := newFixture(t, config.Config{DestroyConfirm: "acme-prod"}, "FAKE_TOFU_PLAN=changes")

	run := f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultDestroyed))
	g.Expect(f.messages()).To(ContainElement(
		"all infrastructure is destroyed, the infrastructure application can be undeployed in Distr"))
}

type fakeModule struct {
	digest  string
	err     error
	fetches []string
}

func (m *fakeModule) Resolve(context.Context) (string, error) {
	return m.digest, m.err
}

func (m *fakeModule) Fetch(_ context.Context, digest, dir string) (string, error) {
	m.fetches = append(m.fetches, digest)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	return dir, os.WriteFile(filepath.Join(dir, "main.tf"), []byte(digest), 0o600)
}

func (m *fakeModule) String() string {
	return "oci://registry.example.com/modules/app?tag=1.0.0"
}

func TestReconcileDownloadsTheModuleOnlyWhenItChanges(t *testing.T) {
	g := NewWithT(t)
	mod := &fakeModule{digest: "sha256:aaa"}
	f := newFixtureWithModule(t, config.Config{}, mod, "FAKE_TOFU_PLAN=changes")

	run := f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultSucceeded))
	g.Expect(run.ModuleDigest).To(Equal("sha256:aaa"))
	g.Expect(f.runner.Dir).To(Equal(filepath.Join(f.cfg.WorkingDir, "module")))
	g.Expect(filepath.Join(f.runner.Dir, ".terraform", "terraform.tfstate")).To(BeAnExistingFile())

	f.reconciler.Reconcile(context.Background())
	g.Expect(mod.fetches).To(Equal([]string{"sha256:aaa"}))

	mod.digest = "sha256:bbb"
	run = f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultSucceeded))
	g.Expect(mod.fetches).To(Equal([]string{"sha256:aaa", "sha256:bbb"}))
	g.Expect(os.ReadFile(filepath.Join(f.runner.Dir, "main.tf"))).To(Equal([]byte("sha256:bbb")))
	g.Expect(f.messages()).To(ContainElement("the module changed since the last apply, from sha256:aaa to sha256:bbb"))

	persisted, err := f.store.ReadPersisted()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(persisted.LastAppliedModule).To(Equal("sha256:bbb"))
}

func TestReconcileKeepsTheModuleWhenTheRegistryIsUnavailable(t *testing.T) {
	g := NewWithT(t)
	mod := &fakeModule{err: errors.New("connection refused")}
	f := newFixtureWithModule(t, config.Config{}, mod)

	run := f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultFailed))
	g.Expect(run.Error).To(ContainSubstring("could not resolve the module"))

	mod.digest, mod.err = "sha256:aaa", nil
	run = f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultSucceeded))

	mod.err = errors.New("connection refused")
	run = f.reconciler.Reconcile(context.Background())
	g.Expect(run.Result).To(Equal(status.ResultSucceeded))
	g.Expect(f.logs.FilterMessageSnippet("could not check the module").Len()).To(Equal(1))
	g.Expect(mod.fetches).To(HaveLen(1))
}

func TestHashVariablesIgnoresOtherVariablesAndOverriddenValues(t *testing.T) {
	g := NewWithT(t)
	g.Expect(hashVariables([]string{"TF_VAR_a=1", "HOME=/root"})).
		To(Equal(hashVariables([]string{"TF_VAR_a=1"})))
	g.Expect(hashVariables([]string{"TF_VAR_a=0", "TF_VAR_a=1"})).
		To(Equal(hashVariables([]string{"TF_VAR_a=1"})))
	g.Expect(hashVariables([]string{"TF_VAR_a=1"})).
		NotTo(Equal(hashVariables([]string{"TF_VAR_a=2"})))
}
