// Package config contains read-only projections of Agent configuration files.
// Readers intentionally extract only endpoint/model metadata; they never
// return credential values or execute user-controlled scripts.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type Detected struct {
	BaseURL           string  `json:"baseUrl"`
	Model             string  `json:"model"`
	ManagedByOneAgent bool    `json:"managedByOneAgent"`
	Unreadable        *string `json:"unreadable"`
}

func ReadCodexConfig(text string) Detected {
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(text), &parsed); err != nil {
		return unreadable(fmt.Sprintf("TOML 无法解析：%v", err))
	}
	providers, _ := parsed["model_providers"].(map[string]any)
	selected, _ := parsed["model_provider"].(string)
	baseURL := ""
	if table, ok := providers[selected].(map[string]any); ok {
		if value, ok := table["base_url"].(string); ok {
			baseURL = value
		}
	}
	model, _ := parsed["model"].(string)
	_, managed := providers["oneagent"]
	return Detected{BaseURL: baseURL, Model: model, ManagedByOneAgent: managed}
}

func ReadClaudeConfig(text string, envVars map[string]string) Detected {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return unreadable(fmt.Sprintf("JSON 无法解析：%v", err))
	}
	if parsed == nil {
		return unreadable("配置的顶层不是对象")
	}
	env, _ := parsed["env"].(map[string]any)
	baseKey := envVars["base_url"]
	if baseKey == "" {
		baseKey = "ANTHROPIC_BASE_URL"
	}
	modelKey := envVars["model"]
	if modelKey == "" {
		modelKey = "ANTHROPIC_MODEL"
	}
	baseURL, _ := env[baseKey].(string)
	model, _ := env[modelKey].(string)
	managed := len(envVars) > 0
	for _, key := range envVars {
		value, ok := env[key].(string)
		if !ok || value == "" {
			managed = false
			break
		}
	}
	return Detected{BaseURL: baseURL, Model: model, ManagedByOneAgent: managed}
}

func ReadOpenAICompatibleConfig(text string) Detected {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		if jsoncCommentPattern.MatchString(text) {
			return unreadable("包含 JSONC 注释，OneAgent 不解析")
		}
		return unreadable(fmt.Sprintf("JSON 无法解析：%v", err))
	}
	if parsed == nil {
		return unreadable("配置的顶层不是对象")
	}
	providers, _ := parsed["provider"].(map[string]any)
	modelValue, _ := parsed["model"].(string)
	selected, bareModel := splitModel(modelValue)
	baseURL := ""
	if table, ok := providers[selected].(map[string]any); ok {
		if options, ok := table["options"].(map[string]any); ok {
			if value, ok := options["baseURL"].(string); ok {
				baseURL = value
			}
		}
	}
	_, managed := providers["oneagent"]
	return Detected{BaseURL: baseURL, Model: bareModel, ManagedByOneAgent: managed}
}

func ReadAiderConfig(text string) Detected {
	baseURL := ""
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range []string{"OPENAI_API_BASE=", "export OPENAI_API_BASE=", "$env:OPENAI_API_BASE ="} {
			if !strings.HasPrefix(trimmed, prefix) {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
			baseURL = unquoteShellValue(value)
		}
	}
	return Detected{BaseURL: baseURL}
}

// DetectFile returns nil only when the file is absent. Any present but empty,
// unreadable, or unknown-format file gets a local diagnostic so one bad Agent
// cannot fail the entire status request.
func DetectFile(path, adapter string, envVars map[string]string) *Detected {
	reader := readerFor(adapter, envVars)
	if reader == nil {
		result := unreadable("没有可用的配置解析器")
		return &result
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) || err == nil && !info.Mode().IsRegular() {
		if os.IsNotExist(err) {
			return nil
		}
		result := unreadable("配置文件不是普通文件")
		return &result
	}
	if err != nil {
		result := unreadable(fmt.Sprintf("无法读取：%v", err))
		return &result
	}
	data, err := os.ReadFile(path)
	if err != nil {
		result := unreadable(fmt.Sprintf("无法读取：%v", err))
		return &result
	}
	if strings.TrimSpace(string(data)) == "" {
		result := unreadable("配置文件为空")
		return &result
	}
	result := safeRead(reader, string(data))
	return &result
}

type reader func(string) Detected

func readerFor(adapter string, envVars map[string]string) reader {
	switch adapter {
	case "codex":
		return ReadCodexConfig
	case "claude-code":
		return func(text string) Detected { return ReadClaudeConfig(text, envVars) }
	case "opencode", "kilo-cli":
		return ReadOpenAICompatibleConfig
	case "aider":
		return ReadAiderConfig
	default:
		return nil
	}
}

func safeRead(read reader, text string) (result Detected) {
	defer func() {
		if recover() != nil {
			result = unreadable("配置解析失败")
		}
	}()
	return read(text)
}

func unreadable(message string) Detected {
	return Detected{Unreadable: new(message)}
}

func splitModel(value string) (string, string) {
	selected, bare, found := strings.Cut(value, "/")
	if !found {
		return "", value
	}
	return selected, bare
}

func unquoteShellValue(value string) string {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		return value[1 : len(value)-1]
	}
	return value
}

var jsoncCommentPattern = regexp.MustCompile(`(?m)(^|\s)(//|/\*)`)
