package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/jsonorder"
	"github.com/MaimoryLab/BootAgent/internal/securefs"
	"github.com/pelletier/go-toml/v2"
	"github.com/tailscale/hujson"
)

// MigrateLegacyAgentConfigs renames only provider identifiers previously owned
// by OneAgent. User-defined providers and unrelated configuration stay intact.
func MigrateLegacyAgentConfigs(ctx context.Context, home string, filesystem securefs.Store) error {
	migrations := []struct {
		path    string
		migrate func([]byte) ([]byte, bool, error)
	}{
		{filepath.Join(home, ".codex", "config.toml"), migrateLegacyCodex},
		{filepath.Join(home, ".config", "opencode", "opencode.json"), migrateLegacyOpenAICompatible},
		{filepath.Join(home, ".config", "kilo", "kilo.jsonc"), migrateLegacyOpenAICompatible},
		{filepath.Join(home, ".openclaw", "openclaw.json"), migrateLegacyOpenClaw},
		{filepath.Join(home, ".kimi-code", "config.toml"), migrateLegacyKimi},
		{filepath.Join(home, ".zcode", "v2", "config.json"), migrateLegacyZCode},
	}
	for _, migration := range migrations {
		data, err := os.ReadFile(migration.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", migration.path, err)
		}
		updated, changed, err := migration.migrate(data)
		if err != nil {
			return fmt.Errorf("migrate %s: %w", migration.path, err)
		}
		if changed {
			if _, err := filesystem.AtomicWrite(ctx, migration.path, updated, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func migrateLegacyCodex(data []byte) ([]byte, bool, error) {
	var parsed map[string]any
	if err := toml.Unmarshal(data, &parsed); err != nil {
		return nil, false, err
	}
	providers, _ := parsed["model_providers"].(map[string]any)
	if _, old := providers["oneagent"]; !old {
		return data, false, nil
	}
	if _, conflict := providers["bootagent"]; conflict {
		return nil, false, fmt.Errorf("both oneagent and bootagent providers exist")
	}
	text := strings.ReplaceAll(string(data), "model_providers.oneagent", "model_providers.bootagent")
	text = strings.ReplaceAll(text, "model_provider = \"oneagent\"", "model_provider = \"bootagent\"")
	text = strings.ReplaceAll(text, "model = \"oneagent/", "model = \"bootagent/")
	return []byte(text), true, nil
}

func migrateLegacyOpenAICompatible(data []byte) ([]byte, bool, error) {
	return migrateLegacyJSON(data, func(root map[string]any) ([]map[string]any, error) {
		providers, _ := root["provider"].(map[string]any)
		if _, old := providers["oneagent"]; !old {
			return nil, nil
		}
		if _, conflict := providers["bootagent"]; conflict {
			return nil, fmt.Errorf("both oneagent and bootagent providers exist")
		}
		patches := []map[string]any{{"op": "move", "from": "/provider/oneagent", "path": "/provider/bootagent"}}
		if model, _ := root["model"].(string); strings.HasPrefix(model, "oneagent/") {
			patches = append(patches, map[string]any{"op": "replace", "path": "/model", "value": "bootagent/" + strings.TrimPrefix(model, "oneagent/")})
		}
		return patches, nil
	})
}

func migrateLegacyOpenClaw(data []byte) ([]byte, bool, error) {
	return migrateLegacyJSON(data, func(root map[string]any) ([]map[string]any, error) {
		models, _ := root["models"].(map[string]any)
		providers, _ := models["providers"].(map[string]any)
		if _, old := providers["oneagent"]; !old {
			return nil, nil
		}
		if _, conflict := providers["bootagent"]; conflict {
			return nil, fmt.Errorf("both oneagent and bootagent providers exist")
		}
		patches := []map[string]any{{"op": "move", "from": "/models/providers/oneagent", "path": "/models/providers/bootagent"}}
		agents, _ := root["agents"].(map[string]any)
		defaults, _ := agents["defaults"].(map[string]any)
		model, _ := defaults["model"].(map[string]any)
		if primary, _ := model["primary"].(string); strings.HasPrefix(primary, "oneagent/") {
			patches = append(patches, map[string]any{"op": "replace", "path": "/agents/defaults/model/primary", "value": "bootagent/" + strings.TrimPrefix(primary, "oneagent/")})
		}
		return patches, nil
	})
}

func migrateLegacyJSON(data []byte, changes func(map[string]any) ([]map[string]any, error)) ([]byte, bool, error) {
	value, err := hujson.Parse(data)
	if err != nil {
		return nil, false, err
	}
	standard := value.Clone()
	standard.Standardize()
	var root map[string]any
	if err := json.Unmarshal(standard.Pack(), &root); err != nil {
		return nil, false, err
	}
	patches, err := changes(root)
	if err != nil || len(patches) == 0 {
		return data, false, err
	}
	patch, err := json.Marshal(patches)
	if err != nil {
		return nil, false, err
	}
	if err := value.Patch(patch); err != nil {
		return nil, false, err
	}
	value.Format()
	return value.Pack(), true, nil
}

func migrateLegacyKimi(data []byte) ([]byte, bool, error) {
	var parsed map[string]any
	if err := toml.Unmarshal(data, &parsed); err != nil {
		return nil, false, err
	}
	providers, _ := parsed["providers"].(map[string]any)
	if _, old := providers["oneagent"]; !old {
		return data, false, nil
	}
	if _, conflict := providers["bootagent"]; conflict {
		return nil, false, fmt.Errorf("both oneagent and bootagent providers exist")
	}
	text := strings.ReplaceAll(string(data), `[providers."oneagent"]`, `[providers."bootagent"]`)
	text = strings.ReplaceAll(text, `[providers.oneagent]`, `[providers.bootagent]`)
	text = strings.ReplaceAll(text, `[models."oneagent/`, `[models."bootagent/`)
	text = strings.ReplaceAll(text, `[models.oneagent/`, `[models.bootagent/`)
	text = strings.ReplaceAll(text, `provider = "oneagent"`, `provider = "bootagent"`)
	text = strings.ReplaceAll(text, `default_model = "oneagent/`, `default_model = "bootagent/`)
	return []byte(text), true, nil
}

func migrateLegacyZCode(data []byte) ([]byte, bool, error) {
	document, err := jsonorder.Parse(data)
	if err != nil {
		return nil, false, err
	}
	providers, ok := document.GetObject("provider")
	if !ok {
		return data, false, nil
	}
	changed := false
	for _, key := range providers.Keys() {
		entry, ok := providers.GetObject(key)
		if !ok || !strings.HasPrefix(entry.GetString("name"), "OneAgent - ") {
			continue
		}
		name := "BootAgent - " + strings.TrimPrefix(entry.GetString("name"), "OneAgent - ")
		newKey := legacyZCodeKey(name)
		if _, conflict := providers.Get(newKey); conflict && newKey != key {
			return nil, false, fmt.Errorf("BootAgent provider %q already exists", newKey)
		}
		entry.Set("name", name)
		providers.Set(newKey, entry)
		if newKey != key {
			providers.Delete(key)
		}
		changed = true
	}
	if !changed {
		return data, false, nil
	}
	updated, err := jsonorder.Marshal(document)
	return updated, err == nil, err
}

func legacyZCodeKey(name string) string {
	digest := sha256.Sum256([]byte(name))
	return "bootagent-" + hex.EncodeToString(digest[:8])
}
