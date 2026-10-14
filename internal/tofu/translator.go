package tofu

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"
)

// translator turns the machine-readable UI into log lines a person can follow in the Distr deployment
// logs, and collects what the controller needs to decide on the next step.
type translator struct {
	logger           *zap.Logger
	progressInterval time.Duration
	lastProgress     map[string]time.Duration
	// quietPlan skips the planned changes, which applying a saved plan repeats after the plan logged them.
	quietPlan bool

	summary     *ChangeSummary
	changes     []ResourceChange
	drift       []ResourceChange
	diagnostics []Diagnostic
	outputs     map[string]Output
}

func newTranslator(logger *zap.Logger, progressInterval time.Duration) *translator {
	return &translator{
		logger:           logger,
		progressInterval: progressInterval,
		lastProgress:     map[string]time.Duration{},
	}
}

func (t *translator) Line(line string) {
	var event Event
	if err := json.Unmarshal([]byte(line), &event); err != nil || event.Type == "" {
		t.logger.Warn(line)
		return
	}
	t.Event(event)
}

//nolint:gocyclo
func (t *translator) Event(e Event) {
	switch e.Type {
	case "planned_change":
		if e.Change == nil {
			return
		}
		t.changes = append(t.changes, *e.Change)
		if t.quietPlan {
			return
		}
		msg := fmt.Sprintf("plan: %s %s", e.Change.Action, e.Change.Resource.Addr)
		if e.Change.Reason != "" {
			msg += fmt.Sprintf(" (%s)", e.Change.Reason)
		}
		t.logger.Info(msg)
	case "resource_drift":
		if e.Change != nil {
			t.drift = append(t.drift, *e.Change)
		}
	case "change_summary":
		if e.Changes == nil {
			return
		}
		t.summary = e.Changes
		switch e.Changes.Operation {
		case "apply":
			t.logger.Info("apply complete: " + e.Changes.String())
		case "destroy":
			t.logger.Info("destroy complete: " + e.Changes.String())
		default:
			t.logDrift(e.Changes.HasChanges())
			if e.Changes.HasChanges() {
				t.logger.Info("plan: " + e.Changes.String())
			}
		}
	case "apply_start":
		if e.Hook == nil {
			return
		}
		t.logger.Info(fmt.Sprintf("%s: %s", e.Hook.Resource.Addr, progressive(e.Hook.Action)))
	case "apply_progress":
		if e.Hook == nil {
			return
		}
		addr, elapsed := e.Hook.Resource.Addr, e.Hook.Elapsed()
		if elapsed-t.lastProgress[addr] < t.progressInterval {
			return
		}
		t.lastProgress[addr] = elapsed
		t.logger.Info(fmt.Sprintf("%s: still %s (%s elapsed)", addr, progressive(e.Hook.Action), elapsed))
	case "apply_complete":
		if e.Hook == nil {
			return
		}
		delete(t.lastProgress, e.Hook.Resource.Addr)
		msg := fmt.Sprintf("%s: %s complete after %s", e.Hook.Resource.Addr, completed(e.Hook.Action), e.Hook.Elapsed())
		if e.Hook.IDValue != "" {
			msg += fmt.Sprintf(" [%s=%s]", e.Hook.IDKey, e.Hook.IDValue)
		}
		t.logger.Info(msg)
	case "apply_errored":
		if e.Hook == nil {
			return
		}
		delete(t.lastProgress, e.Hook.Resource.Addr)
		t.logger.Error(fmt.Sprintf("%s: %s failed after %s",
			e.Hook.Resource.Addr, progressive(e.Hook.Action), e.Hook.Elapsed()))
	case "diagnostic":
		if e.Diagnostic == nil {
			return
		}
		t.diagnostics = append(t.diagnostics, *e.Diagnostic)
		fields := []zap.Field{}
		if e.Diagnostic.Detail != "" {
			fields = append(fields, zap.String("detail", oneLine(e.Diagnostic.Detail)))
		}
		if e.Diagnostic.Address != "" {
			fields = append(fields, zap.String("address", e.Diagnostic.Address))
		}
		if loc := e.Diagnostic.Location(); loc != "" {
			fields = append(fields, zap.String("location", loc))
		}
		if e.Diagnostic.IsError() {
			t.logger.Error("error: "+e.Diagnostic.Summary, fields...)
		} else {
			t.logger.Warn("warning: "+e.Diagnostic.Summary, fields...)
		}
	case "outputs":
		t.outputs = e.Outputs
		t.logOutputs(e.Outputs)
	default:
		t.logger.Debug(e.Message, zap.String("type", e.Type))
	}
}

// logDrift reports the resources the refresh found changed. Providers often normalize attributes on a
// refresh, which OpenTofu reports as drift too, so only a plan with changes makes them warnings.
func (t *translator) logDrift(planHasChanges bool) {
	for _, change := range t.drift {
		msg := fmt.Sprintf("drift: %s was changed outside of OpenTofu (%s)", change.Resource.Addr, change.Action)
		if planHasChanges {
			t.logger.Warn(msg)
		} else {
			t.logger.Debug(msg)
		}
	}
	t.drift = nil
}

func (t *translator) logOutputs(outputs map[string]Output) {
	names := make([]string, 0, len(outputs))
	for name := range outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		output := outputs[name]
		switch {
		case output.Action != "":
			// A plan announces the outputs it will change, without values.
			continue
		case output.Sensitive:
			t.logger.Info(fmt.Sprintf("output %s = (sensitive value)", name))
		case output.Value == nil:
			continue
		default:
			t.logger.Info(fmt.Sprintf("output %s = %s", name, output.Value))
		}
	}
}

func (t *translator) errors() []Diagnostic {
	var errs []Diagnostic
	for _, d := range t.diagnostics {
		if d.IsError() {
			errs = append(errs, d)
		}
	}
	return errs
}
