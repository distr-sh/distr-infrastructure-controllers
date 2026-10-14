package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/distr-sh/distr-infrastructure-controllers/internal/config"
	"github.com/distr-sh/distr-infrastructure-controllers/internal/status"
	"github.com/distr-sh/distr-infrastructure-controllers/internal/store"
	"github.com/distr-sh/distr-infrastructure-controllers/internal/tofu"
	"go.uber.org/zap"
)

type Reconciler struct {
	cfg     *config.Config
	tofu    *tofu.Runner
	store   *store.Store
	tracker *status.Tracker
	logger  *zap.Logger
	// moduleDigest identifies the OpenTofu configuration of the image, so that a plan with changes can tell a
	// new configuration apart from changes someone made to the infrastructure by hand.
	moduleDigest string
	// configHash identifies the input variables, so that a plan with changes can tell a changed
	// configuration apart from changes someone made to the infrastructure by hand.
	configHash string
	runs       int
}

func New(
	cfg *config.Config,
	runner *tofu.Runner,
	moduleDigest string,
	store *store.Store,
	tracker *status.Tracker,
	logger *zap.Logger,
	environ []string,
) *Reconciler {
	return &Reconciler{
		cfg:          cfg,
		tofu:         runner,
		moduleDigest: moduleDigest,
		store:        store,
		tracker:      tracker,
		logger:       logger,
		configHash:   hashVariables(environ),
	}
}

// Run reconciles until ctx is cancelled: right away, then every reconcile interval, or every retry interval
// after a failed run.
func (r *Reconciler) Run(ctx context.Context) error {
	for {
		run := r.Reconcile(ctx)
		if ctx.Err() != nil {
			return nil
		}
		wait := r.cfg.ReconcileInterval
		if run.Result == status.ResultFailed && r.cfg.RetryInterval < wait {
			wait = r.cfg.RetryInterval
		}
		r.logger.Info(fmt.Sprintf("next run in %s at %s", wait, time.Now().Add(wait).Format(time.TimeOnly)))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}
}

func (r *Reconciler) Reconcile(ctx context.Context) status.Run {
	r.runs++
	run := status.Run{
		Number:     r.runs,
		Operation:  status.OperationApply,
		AppVersion: r.cfg.AppVersion,
		StartedAt:  time.Now().UTC(),
	}
	if r.cfg.DestroyRequested() {
		run.Operation = status.OperationDestroy
	}
	r.tracker.Start(run)
	r.logger.Info(fmt.Sprintf("run #%d started", run.Number),
		zap.String("operation", string(run.Operation)),
		zap.String("mode", string(r.cfg.ApplyMode)),
		zap.String("version", r.cfg.AppVersion))

	persisted, err := r.store.ReadPersisted()
	if err != nil {
		r.logger.Warn("could not read the status of the previous runs, starting without it", zap.Error(err))
	}

	result, changes, err := r.reconcile(ctx, run.Operation, persisted)
	finishedAt := time.Now().UTC()
	run.FinishedAt = &finishedAt
	run.Result = result
	run.Changes = changes
	run.ModuleDigest = r.moduleDigest
	elapsed := finishedAt.Sub(run.StartedAt).Round(100 * time.Millisecond)
	if err != nil {
		run.Result = status.ResultFailed
		run.Error = err.Error()
		r.logger.Error(fmt.Sprintf("run #%d failed after %s", run.Number, elapsed), zap.Error(err))
	} else {
		r.logger.Info(fmt.Sprintf("run #%d finished in %s: %s", run.Number, elapsed, describe(run.Result)))
	}

	persisted.LastRun = &run
	switch run.Result {
	case status.ResultSucceeded:
		persisted.LastAppliedVersion = r.cfg.AppVersion
		persisted.LastAppliedModule = r.moduleDigest
		persisted.LastAppliedConfig = r.configHash
	case status.ResultDestroyed:
		persisted.LastAppliedVersion = ""
		persisted.LastAppliedModule = ""
		persisted.LastAppliedConfig = ""
	}
	if err := r.store.WritePersisted(persisted); err != nil {
		r.logger.Warn("could not save the status of this run", zap.Error(err))
	}

	r.tracker.Finish(run)
	return run
}

func (r *Reconciler) reconcile(
	ctx context.Context,
	operation status.Operation,
	persisted store.Persisted,
) (status.Result, *status.Changes, error) {
	destroy := operation == status.OperationDestroy
	if destroy && !r.cfg.DestroyConfirmed() {
		return status.ResultFailed, nil, fmt.Errorf(
			"DESTROY_CONFIRM is %q but has to be the instance name %q to destroy the infrastructure, "+
				"unset it to keep reconciling", r.cfg.DestroyConfirm, r.cfg.InstanceName)
	}

	r.logger.Info("initializing the working directory")
	if err := r.tofu.Init(ctx, r.store.StatePath()); err != nil {
		return status.ResultFailed, nil, err
	}

	planFile := r.store.PlanPath()
	defer r.store.RemovePlan()

	if destroy {
		r.logger.Warn(fmt.Sprintf("planning to destroy all infrastructure of instance %s", r.cfg.InstanceName))
	} else {
		r.logger.Info("planning")
	}
	plan, err := r.tofu.Plan(ctx, planFile, destroy)
	if err != nil {
		return status.ResultFailed, nil, err
	}

	if !plan.HasChanges {
		if destroy {
			r.logger.Info("nothing left to destroy, the infrastructure application can be undeployed in Distr")
			return status.ResultDestroyed, nil, nil
		}
		r.logger.Info("no changes, the infrastructure is up to date")
		return status.ResultSucceeded, nil, nil
	}

	changes := &status.Changes{Add: plan.Summary.Add, Change: plan.Summary.Change, Remove: plan.Summary.Remove}
	if !destroy {
		r.explainChanges(persisted)
	}

	if r.cfg.ApplyMode == config.ApplyModePlan {
		r.showPlan(ctx, planFile)
		if !destroy {
			r.logger.Warn("changes are pending, set APPLY_MODE=auto to apply them")
			return status.ResultPendingChanges, changes, nil
		}
	}

	if destroy {
		r.logger.Warn(fmt.Sprintf("destroying all infrastructure of instance %s as DESTROY_CONFIRM requests",
			r.cfg.InstanceName))
	} else {
		r.logger.Info("applying")
	}
	if _, err := r.tofu.Apply(ctx, planFile); err != nil {
		return status.ResultFailed, changes, err
	}

	if destroy {
		r.logger.Warn("all infrastructure is destroyed, the infrastructure application can be undeployed in Distr")
		return status.ResultDestroyed, changes, nil
	}
	return status.ResultSucceeded, changes, nil
}

// showPlan logs the plan for a review. With APPLY_MODE=plan a destroy is still applied, since DESTROY_CONFIRM
// is its confirmation, but the logs show what it removes.
func (r *Reconciler) showPlan(ctx context.Context, planFile string) {
	lines, err := r.tofu.Show(ctx, planFile)
	if err != nil {
		r.logger.Warn("could not render the plan", zap.Error(err))
		return
	}
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			r.logger.Info(line)
		}
	}
}

func (r *Reconciler) explainChanges(persisted store.Persisted) {
	switch {
	case persisted.LastAppliedVersion == "" && !r.store.StateExists():
		r.logger.Info(fmt.Sprintf("provisioning instance %s for the first time", r.cfg.InstanceName))
	case persisted.LastAppliedVersion == "":
		return
	case persisted.LastAppliedVersion != r.cfg.AppVersion:
		r.logger.Info(fmt.Sprintf("upgrading the infrastructure from version %s to %s",
			persisted.LastAppliedVersion, r.cfg.AppVersion))
	case persisted.LastAppliedModule != r.moduleDigest:
		r.logger.Info("the OpenTofu configuration in the image changed since the last apply",
			zap.String("from", persisted.LastAppliedModule), zap.String("to", r.moduleDigest))
	case persisted.LastAppliedConfig != r.configHash:
		r.logger.Info("the configuration changed since the last apply")
	default:
		r.logger.Warn(fmt.Sprintf("drift detected: the infrastructure no longer matches version %s, "+
			"it was changed outside of the OpenTofu controller", r.cfg.AppVersion))
	}
}

func describe(result status.Result) string {
	switch result {
	case status.ResultSucceeded:
		return "the infrastructure is up to date"
	case status.ResultPendingChanges:
		return "changes are pending"
	case status.ResultDestroyed:
		return "the infrastructure is destroyed"
	default:
		return string(result)
	}
}

// hashVariables hashes the TF_VAR_ entries of environ. Like for exec.Cmd.Env, a later entry of the same
// key wins.
func hashVariables(environ []string) string {
	byKey := map[string]string{}
	for _, kv := range environ {
		if key, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(key, "TF_VAR_") {
			byKey[key] = kv
		}
	}
	vars := slices.Sorted(maps.Values(byKey))
	sum := sha256.Sum256([]byte(strings.Join(vars, "\x00")))
	return hex.EncodeToString(sum[:])
}
