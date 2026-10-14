package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/distr-sh/distr-infrastructure-controllers/internal/buildconfig"
	"go.uber.org/zap/zapcore"
)

type ApplyMode string

const (
	ApplyModeAuto ApplyMode = "auto"
	ApplyModePlan ApplyMode = "plan"
)

type LogFormat string

const (
	LogFormatConsole LogFormat = "console"
	LogFormatJSON    LogFormat = "json"
)

// minPassphraseLength is the minimum OpenTofu accepts for the pbkdf2 key provider.
const minPassphraseLength = 16

var instanceNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{1,38}[a-z0-9])$`)

type Config struct {
	// InstanceName identifies the infrastructure of one end customer. It is passed to OpenTofu as
	// var.instance_name and has to be repeated in DestroyConfirm to destroy the infrastructure.
	InstanceName string
	AppVersion   string
	// ModuleDir holds the OpenTofu configuration, which the image of the infrastructure application copies there.
	// The controller copies it into DataDir before it runs OpenTofu.
	ModuleDir string
	// ProviderMirror is an OCI repository prefix to install the providers of registry.opentofu.org from,
	// such as registry.example.com/acme/opentofu-providers.
	ProviderMirror            string
	RegistryUsername          string
	RegistryPassword          string
	DataDir                   string
	TofuBinary                string
	ApplyMode                 ApplyMode
	ReconcileInterval         time.Duration
	RetryInterval             time.Duration
	ProgressInterval          time.Duration
	DestroyConfirm            string
	StateEncryptionPassphrase string
	HealthAddr                string
	LogLevel                  zapcore.Level
	LogFormat                 LogFormat
}

func (c *Config) DestroyRequested() bool {
	return c.DestroyConfirm != ""
}

func (c *Config) DestroyConfirmed() bool {
	return c.DestroyConfirm == c.InstanceName
}

func FromEnv(lookup func(string) (string, bool)) (*Config, error) {
	p := parser{lookup: lookup}
	cfg := Config{
		InstanceName:              p.required("INSTANCE_NAME"),
		AppVersion:                p.string("APP_VERSION", ""),
		ModuleDir:                 p.string("MODULE_DIR", "/module"),
		ProviderMirror:            strings.TrimSuffix(p.string("PROVIDER_MIRROR", ""), "/"),
		RegistryUsername:          p.string("REGISTRY_USERNAME", ""),
		RegistryPassword:          p.string("REGISTRY_PASSWORD", ""),
		DataDir:                   p.string("DATA_DIR", "/data"),
		TofuBinary:                p.string("TOFU_BINARY", "tofu"),
		ApplyMode:                 ApplyMode(p.string("APPLY_MODE", string(ApplyModeAuto))),
		ReconcileInterval:         p.duration("RECONCILE_INTERVAL", 15*time.Minute),
		RetryInterval:             p.duration("RETRY_INTERVAL", 2*time.Minute),
		ProgressInterval:          p.duration("PROGRESS_LOG_INTERVAL", 30*time.Second),
		DestroyConfirm:            p.string("DESTROY_CONFIRM", ""),
		StateEncryptionPassphrase: p.string("STATE_ENCRYPTION_PASSPHRASE", ""),
		HealthAddr:                p.string("HEALTH_ADDR", ":8686"),
		LogLevel:                  p.level("LOG_LEVEL", zapcore.InfoLevel),
		LogFormat:                 LogFormat(p.string("LOG_FORMAT", string(LogFormatConsole))),
	}

	if cfg.InstanceName != "" && !instanceNamePattern.MatchString(cfg.InstanceName) {
		p.fail("INSTANCE_NAME must be 3 to 40 lowercase letters, digits and hyphens, " +
			"starting and ending with a letter or digit")
	}
	if cfg.ApplyMode != ApplyModeAuto && cfg.ApplyMode != ApplyModePlan {
		p.fail(fmt.Sprintf("APPLY_MODE must be %q or %q", ApplyModeAuto, ApplyModePlan))
	}
	if cfg.LogFormat != LogFormatConsole && cfg.LogFormat != LogFormatJSON {
		p.fail(fmt.Sprintf("LOG_FORMAT must be %q or %q", LogFormatConsole, LogFormatJSON))
	}
	if cfg.StateEncryptionPassphrase != "" && len(cfg.StateEncryptionPassphrase) < minPassphraseLength {
		p.fail(fmt.Sprintf("STATE_ENCRYPTION_PASSPHRASE must be at least %d characters long", minPassphraseLength))
	}
	if (cfg.RegistryUsername == "") != (cfg.RegistryPassword == "") {
		p.fail("REGISTRY_USERNAME and REGISTRY_PASSWORD must be set together")
	}
	if cfg.AppVersion == "" {
		cfg.AppVersion = buildconfig.Version()
	}

	if err := errors.Join(p.errs...); err != nil {
		return nil, err
	}
	return &cfg, nil
}

type parser struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (p *parser) fail(msg string) {
	p.errs = append(p.errs, errors.New(msg))
}

func (p *parser) string(key, fallback string) string {
	if v, ok := p.lookup(key); ok && v != "" {
		return v
	}
	return fallback
}

func (p *parser) required(key string) string {
	v := p.string(key, "")
	if v == "" {
		p.fail(key + " is required")
	}
	return v
}

func (p *parser) duration(key string, fallback time.Duration) time.Duration {
	v := p.string(key, "")
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		p.fail(fmt.Sprintf("%s must be a positive duration such as 15m, got %q", key, v))
		return fallback
	}
	return d
}

func (p *parser) level(key string, fallback zapcore.Level) zapcore.Level {
	v := p.string(key, "")
	if v == "" {
		return fallback
	}
	l, err := zapcore.ParseLevel(v)
	if err != nil {
		p.fail(fmt.Sprintf("%s must be one of debug, info, warn or error, got %q", key, v))
		return fallback
	}
	return l
}
