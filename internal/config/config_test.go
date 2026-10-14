package config

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"go.uber.org/zap/zapcore"
)

func lookup(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func TestFromEnvDefaults(t *testing.T) {
	g := NewWithT(t)
	cfg, err := FromEnv(lookup(map[string]string{"INSTANCE_NAME": "acme-prod"}))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cfg.InstanceName).To(Equal("acme-prod"))
	g.Expect(cfg.ApplyMode).To(Equal(ApplyModeAuto))
	g.Expect(cfg.ReconcileInterval).To(Equal(15 * time.Minute))
	g.Expect(cfg.RetryInterval).To(Equal(2 * time.Minute))
	g.Expect(cfg.ModuleDir).To(Equal("/module"))
	g.Expect(cfg.DataDir).To(Equal("/data"))
	g.Expect(cfg.LogLevel).To(Equal(zapcore.InfoLevel))
	g.Expect(cfg.DestroyRequested()).To(BeFalse())
	g.Expect(cfg.AppVersion).To(Equal("snapshot"))
}

func TestFromEnvVersionAndProviderMirror(t *testing.T) {
	g := NewWithT(t)
	env := map[string]string{
		"INSTANCE_NAME":   "acme-prod",
		"APP_VERSION":     "1.2.0",
		"MODULE_DIR":      "/opt/acme/module",
		"PROVIDER_MIRROR": "ghcr.io/acme/providers/",
	}
	cfg, err := FromEnv(lookup(env))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cfg.AppVersion).To(Equal("1.2.0"))
	g.Expect(cfg.ModuleDir).To(Equal("/opt/acme/module"))
	g.Expect(cfg.ProviderMirror).To(Equal("ghcr.io/acme/providers"))

	env["REGISTRY_USERNAME"] = "acme"
	_, err = FromEnv(lookup(env))
	g.Expect(err).To(MatchError(ContainSubstring("REGISTRY_USERNAME and REGISTRY_PASSWORD")))
}

func TestFromEnvDestroy(t *testing.T) {
	g := NewWithT(t)
	cfg, err := FromEnv(lookup(map[string]string{"INSTANCE_NAME": "acme-prod", "DESTROY_CONFIRM": "acme"}))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cfg.DestroyRequested()).To(BeTrue())
	g.Expect(cfg.DestroyConfirmed()).To(BeFalse())

	cfg.DestroyConfirm = "acme-prod"
	g.Expect(cfg.DestroyConfirmed()).To(BeTrue())
}

func TestFromEnvCollectsAllErrors(t *testing.T) {
	g := NewWithT(t)
	_, err := FromEnv(lookup(map[string]string{
		"INSTANCE_NAME":               "Acme_Prod",
		"APPLY_MODE":                  "yolo",
		"RECONCILE_INTERVAL":          "soon",
		"STATE_ENCRYPTION_PASSPHRASE": "short",
		"LOG_LEVEL":                   "loud",
	}))
	g.Expect(err).To(HaveOccurred())
	for _, key := range []string{
		"INSTANCE_NAME", "APPLY_MODE", "RECONCILE_INTERVAL", "STATE_ENCRYPTION_PASSPHRASE", "LOG_LEVEL",
	} {
		g.Expect(err.Error()).To(ContainSubstring(key))
	}
}

func TestFromEnvRequiresInstanceName(t *testing.T) {
	g := NewWithT(t)
	_, err := FromEnv(lookup(map[string]string{}))
	g.Expect(err).To(MatchError(ContainSubstring("INSTANCE_NAME is required")))
}
