package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// TestHelloWorld runs the real OpenTofu against examples/opentofu/hello-world, with encrypted state,
// through a first apply, a run without changes, an upgrade, a pending plan and a destroy.
func TestHelloWorld(t *testing.T) {
	binary, err := exec.LookPath("tofu")
	if err != nil {
		t.Skip("tofu is not installed, run the tests with mise")
	}
	if testing.Short() {
		t.Skip("skipping the OpenTofu integration test in short mode")
	}
	g := NewWithT(t)

	workingDir := copyExample(t, "hello-world")
	encryption, err := tofu.EncryptionConfig("a passphrase with ${braces} in it")
	g.Expect(err).NotTo(HaveOccurred())

	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)
	dataStore, err := store.Open(t.TempDir())
	g.Expect(err).NotTo(HaveOccurred())

	cfg := &config.Config{InstanceName: "it-hello", AppVersion: "1.0.0", ApplyMode: config.ApplyModeAuto}
	newReconciler := func() *Reconciler {
		runner := &tofu.Runner{
			Binary:         binary,
			Dir:            workingDir,
			PluginCacheDir: dataStore.PluginCacheDir(),
			Env: []string{
				"TF_IN_AUTOMATION=1",
				"TF_ENCRYPTION=" + encryption,
				"TF_VAR_instance_name=" + cfg.InstanceName,
				"TF_VAR_app_version=" + cfg.AppVersion,
				"TF_VAR_provisioning_duration=1s",
			},
			Logger:           logger,
			ProgressInterval: 30 * time.Second,
		}
		return New(cfg, runner, nil, dataStore, &status.Tracker{}, logger, runner.Env)
	}
	ctx := t.Context()

	run := newReconciler().Reconcile(ctx)
	g.Expect(run.Error).To(BeEmpty())
	g.Expect(run.Changes).To(Equal(&status.Changes{Add: 5}))
	g.Expect(logs.FilterMessageSnippet("output greeting = \"Hello from it-hello-").Len()).To(Equal(1))
	g.Expect(logs.FilterMessage("output database_password = (sensitive value)").Len()).To(Equal(1))

	state, err := os.ReadFile(dataStore.StatePath())
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(string(state)).To(ContainSubstring(`"encrypted_data"`))
	g.Expect(string(state)).NotTo(ContainSubstring("it-hello"))
	g.Expect(filepath.Glob(filepath.Join(dataStore.PluginCacheDir(), "registry.opentofu.org", "hashicorp", "*"))).
		To(HaveLen(2))

	run = newReconciler().Reconcile(ctx)
	g.Expect(run.Result).To(Equal(status.ResultSucceeded))
	g.Expect(run.Changes).To(BeNil())

	cfg.AppVersion = "1.1.0"
	cfg.ApplyMode = config.ApplyModePlan
	run = newReconciler().Reconcile(ctx)
	g.Expect(run.Result).To(Equal(status.ResultPendingChanges))
	g.Expect(logs.FilterMessage("upgrading the infrastructure from version 1.0.0 to 1.1.0").Len()).To(Equal(1))

	cfg.ApplyMode = config.ApplyModeAuto
	run = newReconciler().Reconcile(ctx)
	g.Expect(run.Result).To(Equal(status.ResultSucceeded))
	g.Expect(run.Changes.Add + run.Changes.Change).To(BeNumerically(">", 0))
	g.Expect(run.Changes.Remove).To(BeNumerically("<=", 1))

	cfg.DestroyConfirm = cfg.InstanceName
	run = newReconciler().Reconcile(ctx)
	g.Expect(run.Result).To(Equal(status.ResultDestroyed))
	g.Expect(run.Changes.Remove).To(Equal(5))

	run = newReconciler().Reconcile(context.WithoutCancel(ctx))
	g.Expect(run.Result).To(Equal(status.ResultDestroyed))
	g.Expect(run.Changes).To(BeNil())
}

// copyExample copies an example into a temporary directory, so that the test does not leave a .terraform
// directory in the repository.
func copyExample(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("..", "..", "examples", "opentofu", name)
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") && e.Name() != ".terraform.lock.hcl" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}
