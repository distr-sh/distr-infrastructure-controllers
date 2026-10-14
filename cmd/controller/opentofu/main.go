package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/distr-sh/distr-infrastructure-controllers/internal/buildconfig"
	"github.com/distr-sh/distr-infrastructure-controllers/internal/config"
	"github.com/distr-sh/distr-infrastructure-controllers/internal/health"
	"github.com/distr-sh/distr-infrastructure-controllers/internal/logging"
	"github.com/distr-sh/distr-infrastructure-controllers/internal/module"
	"github.com/distr-sh/distr-infrastructure-controllers/internal/reconcile"
	"github.com/distr-sh/distr-infrastructure-controllers/internal/status"
	"github.com/distr-sh/distr-infrastructure-controllers/internal/store"
	"github.com/distr-sh/distr-infrastructure-controllers/internal/tofu"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

func main() {
	root := &cobra.Command{
		Use:           "opentofu-controller",
		Short:         "Provisions and reconciles the infrastructure of a Distr BYOC application with OpenTofu",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(runCommand(), healthcheckCommand(), versionCommand())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Reconcile the infrastructure until the process is stopped",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.FromEnv(os.LookupEnv)
			if err != nil {
				return fmt.Errorf("invalid configuration:\n%w", err)
			}
			logger := logging.New(cfg.LogLevel, cfg.LogFormat)
			defer func() { _ = logger.Sync() }()

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return run(ctx, cfg, logger)
		},
	}
}

func run(ctx context.Context, cfg *config.Config, logger *zap.Logger) error {
	dataStore, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}

	env := []string{
		"TF_IN_AUTOMATION=1",
		"TF_INPUT=0",
		"CHECKPOINT_DISABLE=1",
		"TF_VAR_instance_name=" + cfg.InstanceName,
		"TF_VAR_app_version=" + cfg.AppVersion,
	}
	if v, ok := os.LookupEnv("TF_VAR_app_version"); ok && v != cfg.AppVersion {
		logger.Warn(fmt.Sprintf("TF_VAR_app_version is ignored, the infrastructure gets version %s. "+
			"Set APP_VERSION to change the version", cfg.AppVersion), zap.String("ignored", v))
	}
	if cfg.StateEncryptionPassphrase != "" {
		encryption, err := tofu.EncryptionConfig(cfg.StateEncryptionPassphrase)
		if err != nil {
			return err
		}
		env = append(env, "TF_ENCRYPTION="+encryption)
	} else {
		logger.Warn("STATE_ENCRYPTION_PASSPHRASE is not set, the state is stored unencrypted. " +
			"It contains every secret OpenTofu creates, such as database passwords")
	}
	var pluginCacheDir string
	if _, ok := os.LookupEnv("TF_PLUGIN_CACHE_DIR"); !ok {
		pluginCacheDir = dataStore.PluginCacheDir()
	}
	cliConfigEnv, err := cliConfig(cfg, logger)
	if err != nil {
		return err
	}
	env = append(env, cliConfigEnv...)

	moduleDigest, err := module.Install(cfg.ModuleDir, dataStore.ModuleDir())
	if err != nil {
		return fmt.Errorf("could not copy the OpenTofu configuration from MODULE_DIR into the data directory, the "+
			"image has to be built FROM the OpenTofu controller image and copy the configuration to %s: %w",
			cfg.ModuleDir, err)
	}

	runner := &tofu.Runner{
		Binary:           cfg.TofuBinary,
		Dir:              dataStore.ModuleDir(),
		Env:              env,
		PluginCacheDir:   pluginCacheDir,
		Logger:           logger,
		ProgressInterval: cfg.ProgressInterval,
	}
	tofuVersion, err := runner.Version(ctx)
	if err != nil {
		return fmt.Errorf("could not run %s: %w", cfg.TofuBinary, err)
	}

	fields := []zap.Field{
		zap.String("version", buildconfig.Version()),
		zap.String("commit", buildconfig.Commit()),
		zap.String("appVersion", cfg.AppVersion),
		zap.String("tofuVersion", tofuVersion),
		zap.String("instance", cfg.InstanceName),
		zap.String("mode", string(cfg.ApplyMode)),
		zap.Duration("interval", cfg.ReconcileInterval),
		zap.String("moduleDir", cfg.ModuleDir),
		zap.String("module", moduleDigest),
		zap.String("dataDir", dataStore.Dir()),
	}
	if cfg.ProviderMirror != "" {
		fields = append(fields, zap.String("providerMirror", cfg.ProviderMirror))
	}
	logger.Info("starting the OpenTofu controller", fields...)

	listener, err := net.Listen("tcp", cfg.HealthAddr)
	if err != nil {
		return fmt.Errorf("could not listen on HEALTH_ADDR %s for the health checks: %w", cfg.HealthAddr, err)
	}
	tracker := &status.Tracker{}
	server := &http.Server{
		Handler:           health.Handler(tracker),
		ReadHeaderTimeout: 5 * time.Second,
	}
	// Without the health checks the runtime cannot tell that the controller works, so the controller stops
	// with them and lets the runtime restart it.
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("the health checks stopped answering, stopping the controller", zap.Error(err))
			cancel(fmt.Errorf("the health checks stopped answering: %w", err))
		}
	}()
	defer func() {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelShutdown()
		_ = server.Shutdown(shutdownCtx)
	}()

	reconciler := reconcile.New(cfg, runner, moduleDigest, dataStore, tracker, logger, append(os.Environ(), env...))
	err = reconciler.Run(ctx)
	logger.Info("the OpenTofu controller stopped")
	if cause := context.Cause(ctx); err == nil && cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}
	return err
}

// cliConfig writes the OpenTofu CLI configuration for the provider mirror and the registry credentials, and
// returns the environment that points OpenTofu at it. A CLI configuration of the user's own takes precedence.
func cliConfig(cfg *config.Config, logger *zap.Logger) ([]string, error) {
	var hosts []string
	if cfg.ProviderMirror != "" {
		host, _, _ := strings.Cut(cfg.ProviderMirror, "/")
		hosts = append(hosts, host)
	}
	content := tofu.CLIConfig(cfg.ProviderMirror, hosts, cfg.RegistryUsername, cfg.RegistryPassword)
	if content == "" {
		return nil, nil
	}
	if path, ok := os.LookupEnv("TF_CLI_CONFIG_FILE"); ok {
		logger.Warn("TF_CLI_CONFIG_FILE is set, PROVIDER_MIRROR and the registry credentials have to be configured in "+
			"that file", zap.String("path", path))
		return nil, nil
	}
	// The credentials stay out of the data directory, which outlives the container.
	dir, err := os.MkdirTemp("", "opentofu-controller-")
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "tofurc")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("could not write the OpenTofu CLI configuration: %w", err)
	}
	return []string{"TF_CLI_CONFIG_FILE=" + path}, nil
}

func healthcheckCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "healthcheck",
		Short: "Exit with an error unless the controller in this container is ready",
		RunE: func(cmd *cobra.Command, args []string) error {
			addr := os.Getenv("HEALTH_ADDR")
			if addr == "" {
				addr = ":8686"
			}
			return health.Check(cmd.Context(), addr)
		},
	}
}

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of the controller",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(buildconfig.Version(), buildconfig.Commit())
		},
	}
}
