package tofu

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Event is one line of the machine-readable UI that "tofu plan -json" and "tofu apply -json" print.
type Event struct {
	Level      string            `json:"@level"`
	Message    string            `json:"@message"`
	Type       string            `json:"type"`
	Change     *ResourceChange   `json:"change,omitempty"`
	Hook       *Hook             `json:"hook,omitempty"`
	Changes    *ChangeSummary    `json:"changes,omitempty"`
	Diagnostic *Diagnostic       `json:"diagnostic,omitempty"`
	Outputs    map[string]Output `json:"outputs,omitempty"`
}

type Resource struct {
	Addr string `json:"addr"`
}

type ResourceChange struct {
	Resource Resource `json:"resource"`
	Action   string   `json:"action"`
	Reason   string   `json:"reason,omitempty"`
}

type Hook struct {
	Resource       Resource `json:"resource"`
	Action         string   `json:"action"`
	IDKey          string   `json:"id_key,omitempty"`
	IDValue        string   `json:"id_value,omitempty"`
	ElapsedSeconds float64  `json:"elapsed_seconds,omitempty"`
}

func (h *Hook) Elapsed() time.Duration {
	return time.Duration(h.ElapsedSeconds * float64(time.Second))
}

type ChangeSummary struct {
	Add       int    `json:"add"`
	Change    int    `json:"change"`
	Import    int    `json:"import"`
	Remove    int    `json:"remove"`
	Forget    int    `json:"forget"`
	Operation string `json:"operation"`
}

func (s ChangeSummary) HasChanges() bool {
	return s.Add+s.Change+s.Import+s.Remove+s.Forget > 0
}

func (s ChangeSummary) String() string {
	var parts []string
	switch s.Operation {
	case "apply", "destroy":
		parts = []string{
			fmt.Sprintf("%d added", s.Add),
			fmt.Sprintf("%d changed", s.Change),
			fmt.Sprintf("%d destroyed", s.Remove),
		}
		if s.Import > 0 {
			parts = append(parts, fmt.Sprintf("%d imported", s.Import))
		}
		if s.Forget > 0 {
			parts = append(parts, fmt.Sprintf("%d forgotten", s.Forget))
		}
	default:
		parts = []string{
			fmt.Sprintf("%d to add", s.Add),
			fmt.Sprintf("%d to change", s.Change),
			fmt.Sprintf("%d to destroy", s.Remove),
		}
		if s.Import > 0 {
			parts = append(parts, fmt.Sprintf("%d to import", s.Import))
		}
		if s.Forget > 0 {
			parts = append(parts, fmt.Sprintf("%d to forget", s.Forget))
		}
	}
	return strings.Join(parts, ", ")
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	Detail   string `json:"detail,omitempty"`
	Address  string `json:"address,omitempty"`
	Range    *struct {
		Filename string `json:"filename"`
		Start    struct {
			Line int `json:"line"`
		} `json:"start"`
	} `json:"range,omitempty"`
}

func (d Diagnostic) IsError() bool {
	return d.Severity == "error"
}

func (d Diagnostic) Location() string {
	if d.Range == nil || d.Range.Filename == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", d.Range.Filename, d.Range.Start.Line)
}

func (d Diagnostic) String() string {
	if d.Detail == "" {
		return d.Summary
	}
	return d.Summary + ": " + oneLine(d.Detail)
}

type Output struct {
	Sensitive bool            `json:"sensitive"`
	Value     json.RawMessage `json:"value,omitempty"`
	Action    string          `json:"action,omitempty"`
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func progressive(action string) string {
	switch action {
	case "create":
		return "creating"
	case "update":
		return "updating"
	case "delete":
		return "destroying"
	case "read":
		return "reading"
	case "replace":
		return "replacing"
	default:
		return action
	}
}

func completed(action string) string {
	switch action {
	case "create":
		return "creation"
	case "update":
		return "modification"
	case "delete":
		return "destruction"
	case "read":
		return "read"
	default:
		return action
	}
}
