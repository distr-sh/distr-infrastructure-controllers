package status

import (
	"sync"
	"time"
)

type Result string

const (
	ResultSucceeded      Result = "succeeded"
	ResultPendingChanges Result = "pending_changes"
	ResultFailed         Result = "failed"
	ResultDestroyed      Result = "destroyed"
)

type Operation string

const (
	OperationApply   Operation = "apply"
	OperationDestroy Operation = "destroy"
)

type Changes struct {
	Add    int `json:"add"`
	Change int `json:"change"`
	Remove int `json:"remove"`
}

type Run struct {
	Number       int        `json:"number"`
	Operation    Operation  `json:"operation"`
	AppVersion   string     `json:"appVersion"`
	ModuleDigest string     `json:"moduleDigest,omitempty"`
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	Result       Result     `json:"result,omitempty"`
	Changes      *Changes   `json:"changes,omitempty"`
	Error        string     `json:"error,omitempty"`
}

type Snapshot struct {
	Ready   bool `json:"ready"`
	Current *Run `json:"current,omitempty"`
	Last    *Run `json:"last,omitempty"`
}

type Tracker struct {
	mu      sync.RWMutex
	current *Run
	last    *Run
}

func (t *Tracker) Start(run Run) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.current = &run
}

func (t *Tracker) Finish(run Run) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.current = nil
	t.last = &run
}

// Ready is false only once a run has finished with an error, and stays false until a run succeeds. A
// process that has not finished a run yet is ready: the Distr Kubernetes controller waits for readiness
// after every upgrade, and creating a cluster takes longer than it waits.
func (t *Tracker) Ready() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.ready()
}

func (t *Tracker) ready() bool {
	return t.last == nil || t.last.Result != ResultFailed
}

func (t *Tracker) Snapshot() Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s := Snapshot{Ready: t.ready()}
	if t.current != nil {
		s.Current = new(*t.current)
	}
	if t.last != nil {
		s.Last = new(*t.last)
	}
	return s
}
