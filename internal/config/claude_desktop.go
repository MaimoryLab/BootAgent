package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	claudeDesktopProfileID   = "2c9e0d0e-cf67-4a75-9b67-1298f3a45e75"
	claudeDesktopProfileName = "BootAgent"
)

type claudeDesktopPaths struct{ normal, threep, profile, meta string }
type claudeDesktopSnapshot struct {
	path           string
	data           []byte
	exists, secret bool
}

// WriteClaudeDesktop activates one BootAgent-owned, transactional 3P profile.
func (w Writer) WriteClaudeDesktop(ctx context.Context, baseURL, apiKey, model string, context1M bool) (string, error) {
	paths, err := claudeDesktopConfigPaths(w.Home, w.OS)
	if err != nil {
		return "", err
	}
	baseURL, apiKey, model = strings.TrimSpace(baseURL), strings.TrimSpace(apiKey), strings.TrimSpace(model)
	if baseURL == "" || apiKey == "" {
		return "", configError("Claude Desktop requires an Anthropic base URL and API key")
	}
	if !claudeDesktopModelOK(model) {
		return "", configError("Claude Desktop model must use a claude-sonnet-*, claude-opus-*, claude-haiku-*, or claude-fable-* ID")
	}

	profile := map[string]any{
		"coworkEgressAllowedHosts":     []string{"*"},
		"disableDeploymentModeChooser": true,
		"inferenceGatewayApiKey":       apiKey,
		"inferenceGatewayAuthScheme":   "bearer",
		"inferenceGatewayBaseUrl":      baseURL,
		"inferenceProvider":            "gateway",
		"inferenceModels":              []any{model},
	}
	if context1M {
		profile["inferenceModels"] = []any{map[string]any{"name": model, "supports1m": true}}
	}
	profileData, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return "", configError("Cannot encode Claude Desktop profile: %v", err)
	}
	profileData = append(profileData, '\n')

	normal, err := claudeDesktopDeployment(paths.normal)
	if err != nil {
		return "", err
	}
	threep, err := claudeDesktopDeployment(paths.threep)
	if err != nil {
		return "", err
	}
	meta, err := claudeDesktopMeta(paths.meta)
	if err != nil {
		return "", err
	}
	writes := []claudeDesktopSnapshot{
		{path: paths.profile, data: profileData, secret: true},
		{path: paths.meta, data: meta},
		{path: paths.threep, data: threep},
		{path: paths.normal, data: normal},
	}
	for i := range writes {
		data, readErr := os.ReadFile(writes[i].path)
		if readErr == nil {
			writes[i].data, writes[i].exists = append([]byte(nil), data...), true
		} else if !os.IsNotExist(readErr) {
			return "", configError("Cannot snapshot Claude Desktop configuration %s: %v", writes[i].path, readErr)
		}
	}
	payloads := [][]byte{profileData, meta, threep, normal}
	for i, payload := range payloads {
		if err := w.write(ctx, writes[i].path, payload, writes[i].secret); err != nil {
			if rollbackErr := w.rollbackClaudeDesktop(context.WithoutCancel(ctx), writes[:i+1]); rollbackErr != nil {
				return "", fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
			}
			return "", err
		}
	}
	return paths.profile, nil
}

func (w Writer) rollbackClaudeDesktop(ctx context.Context, snapshots []claudeDesktopSnapshot) error {
	var result error
	for _, snapshot := range slices.Backward(snapshots) {
		if snapshot.exists {
			_, err := w.FS.AtomicWrite(ctx, snapshot.path, snapshot.data, snapshot.secret)
			result = errors.Join(result, err)
		} else if err := os.Remove(snapshot.path); err != nil && !os.IsNotExist(err) {
			result = errors.Join(result, err)
		}
	}
	return result
}

func claudeDesktopConfigPaths(home, osID string) (claudeDesktopPaths, error) {
	var root string
	switch osID {
	case "macos":
		root = filepath.Join(home, "Library", "Application Support")
	case "windows":
		root = filepath.Join(home, "AppData", "Local")
	default:
		return claudeDesktopPaths{}, configError("Claude Desktop configuration is not supported on %s", osID)
	}
	normal, threep := filepath.Join(root, "Claude"), filepath.Join(root, "Claude-3p")
	library := filepath.Join(threep, "configLibrary")
	return claudeDesktopPaths{
		normal:  filepath.Join(normal, "claude_desktop_config.json"),
		threep:  filepath.Join(threep, "claude_desktop_config.json"),
		profile: filepath.Join(library, claudeDesktopProfileID+".json"),
		meta:    filepath.Join(library, "_meta.json"),
	}, nil
}

func claudeDesktopDeployment(path string) ([]byte, error) {
	object, err := readClaudeDesktopObject(path)
	if err != nil {
		return nil, err
	}
	object["deploymentMode"] = "3p"
	return marshalClaudeDesktopObject(path, object)
}

func claudeDesktopMeta(path string) ([]byte, error) {
	object, err := readClaudeDesktopObject(path)
	if err != nil {
		return nil, err
	}
	entries := []any{}
	if value, ok := object["entries"]; ok {
		var valid bool
		entries, valid = value.([]any)
		if !valid {
			return nil, configError("Existing Claude Desktop entries must be an array: %s", path)
		}
	}
	kept := entries[:0]
	for _, entry := range entries {
		if object, ok := entry.(map[string]any); ok && object["id"] == claudeDesktopProfileID {
			continue
		}
		kept = append(kept, entry)
	}
	object["entries"] = append(kept, map[string]any{"id": claudeDesktopProfileID, "name": claudeDesktopProfileName})
	object["appliedId"] = claudeDesktopProfileID
	return marshalClaudeDesktopObject(path, object)
}

func readClaudeDesktopObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, configError("Cannot read Claude Desktop configuration %s: %v", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, nil
	}
	object := map[string]any{}
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, configError("Existing Claude Desktop configuration must contain a JSON object: %s", path)
	}
	return object, nil
}

func marshalClaudeDesktopObject(path string, object any) ([]byte, error) {
	data, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return nil, configError("Cannot encode Claude Desktop configuration %s: %v", path, err)
	}
	return append(data, '\n'), nil
}

func claudeDesktopModelOK(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	tail, ok := strings.CutPrefix(model, "anthropic/claude-")
	if !ok {
		tail, ok = strings.CutPrefix(model, "claude-")
	}
	if !ok || strings.Contains(tail, "[1m]") {
		return false
	}
	for _, role := range []string{"sonnet-", "opus-", "haiku-", "fable-"} {
		if value, found := strings.CutPrefix(tail, role); found && value != "" {
			return true
		}
	}
	return false
}
