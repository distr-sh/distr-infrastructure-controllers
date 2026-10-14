package tofu

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

const (
	exitCodeChanges = 2
	maxLineLength   = 4 * 1024 * 1024
	errorTailLines  = 20
	lockTimeout     = "5m"
)

type Runner struct {
	Binary string
	Dir    string
	// Env is appended to the environment of the controller.
	Env []string
	// PluginCacheDir is a provider plugin cache of the runner's own, which init prunes to the providers the
	// working directory uses.
	PluginCacheDir   string
	Logger           *zap.Logger
	ProgressInterval time.Duration
}

type PlanResult struct {
	HasChanges  bool
	Summary     ChangeSummary
	Changes     []ResourceChange
	Diagnostics []Diagnostic
}

type ApplyResult struct {
	Summary ChangeSummary
	Outputs map[string]Output
}

func (r *Runner) Version(ctx context.Context) (string, error) {
	var out strings.Builder
	if _, err := r.run(ctx, []string{"version", "-json"}, func(line string) { out.WriteString(line) }, nil); err != nil {
		return "", err
	}
	var v struct {
		Version string `json:"terraform_version"`
	}
	if err := json.Unmarshal([]byte(out.String()), &v); err != nil {
		return "", fmt.Errorf("could not parse tofu version: %w", err)
	}
	return v.Version, nil
}

// Init initializes the working directory with the local backend storing its state at statePath. The
// providers have to match the dependency lock file of the configuration.
func (r *Runner) Init(ctx context.Context, statePath string) error {
	tail := newTail(errorTailLines)
	exitCode, err := r.run(ctx,
		[]string{
			"init", "-input=false", "-no-color", "-reconfigure", "-lockfile=readonly",
			"-backend-config=path=" + statePath,
		},
		func(line string) { tail.add(line); r.Logger.Debug(line) },
		func(line string) { tail.add(line); r.Logger.Debug(line) },
	)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		for _, line := range tail.lines() {
			r.Logger.Error(line)
		}
		return fmt.Errorf("tofu init failed with exit code %d", exitCode)
	}
	if err := r.checkLocalBackend(statePath); err != nil {
		return err
	}
	if r.PluginCacheDir != "" {
		if err := r.prunePluginCache(); err != nil {
			r.Logger.Warn("could not prune the plugin cache", zap.Error(err))
		}
	}
	return nil
}

// checkLocalBackend fails unless init configured the local backend with statePath. Without a backend block
// OpenTofu ignores -backend-config and writes the state into the working directory, which does not survive
// a restart of the container.
func (r *Runner) checkLocalBackend(statePath string) error {
	dataDir := os.Getenv("TF_DATA_DIR")
	if dataDir == "" {
		dataDir = ".terraform"
	}
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(r.Dir, dataDir)
	}
	missing := fmt.Errorf(
		`the configuration in %s must declare an empty local backend (terraform { backend "local" {} }) `+
			"so that the state is stored in the data directory", r.Dir)

	data, err := os.ReadFile(filepath.Join(dataDir, "terraform.tfstate"))
	if errors.Is(err, os.ErrNotExist) {
		return missing
	} else if err != nil {
		return fmt.Errorf("could not read backend configuration: %w", err)
	}
	var meta struct {
		Backend *struct {
			Type   string `json:"type"`
			Config struct {
				Path string `json:"path"`
			} `json:"config"`
		} `json:"backend"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return fmt.Errorf("could not parse backend configuration: %w", err)
	}
	if meta.Backend == nil || meta.Backend.Type != "local" {
		return missing
	}
	if meta.Backend.Config.Path != statePath {
		return fmt.Errorf("the local backend stores the state at %q instead of %q, remove its path argument",
			meta.Backend.Config.Path, statePath)
	}
	return nil
}

func (r *Runner) Plan(ctx context.Context, planFile string, destroy bool) (*PlanResult, error) {
	args := []string{
		"plan", "-input=false", "-json", "-detailed-exitcode", "-lock-timeout=" + lockTimeout, "-out=" + planFile,
	}
	if destroy {
		args = append(args, "-destroy")
	}
	t := newTranslator(r.Logger, r.ProgressInterval)
	exitCode, err := r.run(ctx, args, t.Line, r.logStderr)
	if err != nil {
		return nil, err
	}
	result := &PlanResult{Changes: t.changes, Diagnostics: t.diagnostics}
	if t.summary != nil {
		result.Summary = *t.summary
	}
	switch exitCode {
	case 0:
		return result, nil
	case exitCodeChanges:
		result.HasChanges = true
		return result, nil
	default:
		return nil, failure("plan", exitCode, t.errors())
	}
}

func (r *Runner) Apply(ctx context.Context, planFile string) (*ApplyResult, error) {
	t := newTranslator(r.Logger, r.ProgressInterval)
	t.quietPlan = true
	exitCode, err := r.run(ctx,
		[]string{"apply", "-input=false", "-json", "-lock-timeout=" + lockTimeout, planFile},
		t.Line, r.logStderr,
	)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, failure("apply", exitCode, t.errors())
	}
	result := &ApplyResult{Outputs: t.outputs}
	if t.summary != nil {
		result.Summary = *t.summary
	}
	return result, nil
}

func (r *Runner) Show(ctx context.Context, planFile string) ([]string, error) {
	var lines []string
	exitCode, err := r.run(ctx, []string{"show", "-no-color", planFile},
		func(line string) { lines = append(lines, line) }, r.logStderr)
	if err != nil {
		return nil, err
	}
	if exitCode != 0 {
		return nil, fmt.Errorf("tofu show failed with exit code %d", exitCode)
	}
	return lines, nil
}

func (r *Runner) logStderr(line string) {
	if line != "" {
		r.Logger.Warn(line)
	}
}

func failure(command string, exitCode int, diagnostics []Diagnostic) error {
	if len(diagnostics) == 0 {
		return fmt.Errorf("tofu %s failed with exit code %d", command, exitCode)
	}
	errs := make([]error, len(diagnostics))
	for i, d := range diagnostics {
		errs[i] = errors.New(d.String())
	}
	return fmt.Errorf("tofu %s failed: %w", command, errors.Join(errs...))
}

// run starts tofu and feeds every line of its output to the callbacks. A cancelled context interrupts
// OpenTofu like Ctrl-C does: it finishes the operations in flight, writes the state and exits. run waits
// for that instead of killing the process, since a killed apply loses track of what it created. The
// container runtime ends the wait once the stop grace period is over.
func (r *Runner) run(ctx context.Context, args []string, stdout, stderr func(string)) (int, error) {
	cmd := exec.CommandContext(ctx, r.Binary, args...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), r.Env...)
	if r.PluginCacheDir != "" {
		cmd.Env = append(cmd.Env, "TF_PLUGIN_CACHE_DIR="+r.PluginCacheDir)
	}
	// A process group of its own keeps a Ctrl-C in a terminal from reaching OpenTofu next to the interrupt
	// sent here, since a second interrupt makes OpenTofu abort without saving the state.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		r.Logger.Warn("interrupting OpenTofu, waiting for the operations in progress to finish")
		return cmd.Process.Signal(os.Interrupt)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return 0, err
	}

	r.Logger.Debug("running tofu", zap.Strings("args", args))
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("could not start %s: %w", r.Binary, err)
	}

	var wg sync.WaitGroup
	var stdoutErr, stderrErr error
	wg.Go(func() { stdoutErr = scanLines(stdoutPipe, stdout) })
	wg.Go(func() { stderrErr = scanLines(stderrPipe, stderr) })
	wg.Wait()

	exitCode := 0
	err = cmd.Wait()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		if ctx.Err() != nil {
			return exitErr.ExitCode(), fmt.Errorf("tofu %s was interrupted: %w", args[0], context.Cause(ctx))
		}
		exitCode, err = exitErr.ExitCode(), nil
	}
	if err != nil {
		return 0, err
	}
	// The result of a command whose output was cut off is incomplete, even if it exited successfully.
	if err := errors.Join(stdoutErr, stderrErr); err != nil {
		return exitCode, fmt.Errorf("could not read the output of tofu %s: %w", args[0], err)
	}
	return exitCode, nil
}

func scanLines(r io.Reader, fn func(string)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxLineLength)
	for scanner.Scan() {
		if fn != nil {
			fn(scanner.Text())
		}
	}
	err := scanner.Err()
	if errors.Is(err, bufio.ErrTooLong) {
		err = fmt.Errorf("a line is longer than %d MiB", maxLineLength/1024/1024)
	}
	// Drain what is left after an overlong line so that tofu never blocks on a full pipe.
	_, _ = io.Copy(io.Discard, r)
	return err
}

// tail keeps the last lines of stdout and stderr, which run reads concurrently.
type tail struct {
	mu  sync.Mutex
	max int
	buf []string
}

func newTail(n int) *tail {
	return &tail{max: n}
}

func (t *tail) add(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, line)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
}

func (t *tail) lines() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.buf)
}
