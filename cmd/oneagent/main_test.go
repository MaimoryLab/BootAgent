package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlatInstallCLIEmitsStructuredGuideResult(t *testing.T) {
	home := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"--agent", "openclaw", "--check-agent-only", "--json", "--home", home}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true {
		t.Fatalf("payload=%v", payload)
	}
	results, ok := payload["results"].([]any)
	if !ok || len(results) != 1 || results[0].(map[string]any)["status"] != "guide-only" {
		t.Fatalf("results=%v", payload["results"])
	}
}

func TestAgentSetAndListCLIUseGoBindingsWithoutLeakingKey(t *testing.T) {
	home := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run([]string{"agent", "set", "codex", "--provider", "ppio", "--model", "model-a", "--api-key", "cli-secret", "--json", "--home", home}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("set exit=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "cli-secret") {
		t.Fatal("API key appeared in agent set output")
	}
	var setPayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &setPayload); err != nil || setPayload["provider"] != "ppio" {
		t.Fatalf("set payload=%s err=%v", stdout.String(), err)
	}
	if _, err := os.Stat(filepath.Join(home, ".oneagent", "agents", "codex.json")); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	code = run([]string{"agent", "list", "--json", "--home", home}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("list exit=%d stderr=%s", code, stderr.String())
	}
	var listPayload struct {
		OK     bool                      `json:"ok"`
		Agents map[string]map[string]any `json:"agents"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &listPayload); err != nil || !listPayload.OK {
		t.Fatalf("list payload=%s err=%v", stdout.String(), err)
	}
	if listPayload.Agents["codex"]["model"] != "model-a" {
		t.Fatalf("agents=%v", listPayload.Agents)
	}
}

func TestFlatCLIRejectsEmptyAgentListAndConflictingVersionModes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--agent", ",", "--check-agent-only"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "At least one Agent") {
		t.Fatalf("empty agents exit=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--check-agent-only", "--latest", "--locked-version", "--json"}, &stdout, &stderr); code != 2 {
		t.Fatalf("conflicting modes exit=%d output=%q", code, stdout.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil || payload["error_code"] != "INVALID_REQUEST" {
		t.Fatalf("error payload=%q err=%v", stdout.String(), err)
	}
}
