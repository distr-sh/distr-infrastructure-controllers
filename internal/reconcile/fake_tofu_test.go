package reconcile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The test binary doubles as a fake tofu: TestMain hands over to fakeTofu when FAKE_TOFU is set, which the
// tests pass to the runner. The FAKE_TOFU_* variables choose how each command behaves.
func fakeTofu(args []string) int {
	if len(args) == 0 {
		return 1
	}
	switch args[0] {
	case "version":
		fmt.Println(`{"terraform_version":"1.13.1"}`)
		return 0
	case "init":
		return fakeInit(args[1:])
	case "plan":
		return fakePlan(args[1:])
	case "apply":
		return fakeApply()
	case "show":
		fmt.Println("  # random_pet.cluster will be created")
		fmt.Println(`  + resource "random_pet" "cluster" {}`)
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unexpected command", args[0])
		return 1
	}
}

func fakeInit(args []string) int {
	if os.Getenv("FAKE_TOFU_INIT_FAIL") != "" {
		fmt.Fprintln(os.Stderr, "Error: Failed to query available provider packages")
		return 1
	}
	var statePath string
	for _, arg := range args {
		if path, ok := strings.CutPrefix(arg, "-backend-config=path="); ok {
			statePath = path
		}
	}
	if os.Getenv("FAKE_TOFU_NO_BACKEND") != "" {
		return 0
	}
	meta := map[string]any{"backend": map[string]any{"type": "local", "config": map[string]any{"path": statePath}}}
	data, err := json.Marshal(meta)
	if err != nil {
		return 1
	}
	_ = os.MkdirAll(".terraform", 0o700)
	_ = os.WriteFile(filepath.Join(".terraform", "terraform.tfstate"), data, 0o600)
	_ = os.WriteFile("state-path", []byte(statePath), 0o600)
	return 0
}

func fakePlan(args []string) int {
	for _, arg := range args {
		if path, ok := strings.CutPrefix(arg, "-out="); ok {
			_ = os.WriteFile(path, []byte("plan"), 0o600)
		}
	}
	switch os.Getenv("FAKE_TOFU_PLAN") {
	case "changes":
		fmt.Println(`{"type":"planned_change","change":{"resource":{"addr":"random_pet.cluster"},"action":"create"}}`)
		fmt.Println(`{"type":"change_summary","changes":{"add":1,"change":0,"remove":0,"operation":"plan"}}`)
		return 2
	case "error":
		fmt.Println(`{"type":"diagnostic","diagnostic":{"severity":"error","summary":"No valid credential sources found"}}`)
		return 1
	default:
		fmt.Println(`{"type":"change_summary","changes":{"add":0,"change":0,"remove":0,"operation":"plan"}}`)
		return 0
	}
}

func fakeApply() int {
	if os.Getenv("FAKE_TOFU_APPLY_FAIL") != "" {
		fmt.Println(`{"type":"apply_errored","hook":{"resource":{"addr":"random_pet.cluster"},"action":"create"}}`)
		fmt.Println(`{"type":"diagnostic","diagnostic":{"severity":"error","summary":"quota exceeded"}}`)
		return 1
	}
	statePath, _ := os.ReadFile("state-path")
	_ = os.WriteFile(string(statePath), []byte(`{"version":4}`), 0o600)
	fmt.Println(`{"type":"apply_complete","hook":{"resource":{"addr":"random_pet.cluster"},"action":"create"}}`)
	fmt.Println(`{"type":"change_summary","changes":{"add":1,"change":0,"remove":0,"operation":"apply"}}`)
	return 0
}
