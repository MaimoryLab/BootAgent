package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/jsonorder"
	"github.com/MaimoryLab/OneAgent/internal/provider"
	"github.com/MaimoryLab/OneAgent/internal/securefs"
	"github.com/pelletier/go-toml/v2"
)

type Writer struct {
	Home string
	OS   string
	FS   securefs.Store
}

func NewWriter(home, osID string, filesystem securefs.Store) Writer {
	return Writer{Home: home, OS: osID, FS: filesystem}
}

// WriteCodex points Codex at the Provider. The credential goes into
// ~/.codex/auth.json next to config.toml, and requires_openai_auth tells Codex
// to authenticate the managed provider with it instead of an environment
// variable.
func (w Writer) WriteCodex(ctx context.Context, path, providerName, baseURL, apiKey, model string) error {
	managed := strings.Join([]string{
		"model_provider = \"oneagent\"",
		"model = " + quoteTOML(model),
		"",
		"[model_providers.oneagent]",
		"name = " + quoteTOML(providerName),
		"base_url = " + quoteTOML(baseURL),
		"wire_api = \"responses\"",
		"requires_openai_auth = true",
		"",
	}, "\n")
	existing, err := readText(path)
	if err != nil {
		return configError("Cannot read existing TOML configuration %s: %v", path, err)
	}
	content, err := mergeCodexTOML(existing, managed, path)
	if err != nil {
		return err
	}
	// The credential lands first: a config.toml pointing at a provider Codex
	// cannot authenticate is worse than an unreferenced key.
	if err := w.writeCodexAuth(ctx, filepath.Join(filepath.Dir(path), "auth.json"), apiKey); err != nil {
		return err
	}
	return w.write(ctx, path, []byte(content), false)
}

// writeCodexAuth merges the key into auth.json, keeping any other login
// material Codex stored there.
func (w Writer) writeCodexAuth(ctx context.Context, path, apiKey string) error {
	document, err := loadJSON(path)
	if err != nil {
		return err
	}
	document.Set("OPENAI_API_KEY", apiKey)
	// A leftover "chatgpt" mode next to a fresh key makes Codex prefer its
	// cached OAuth tokens and ignore the key entirely.
	document.Set("auth_mode", "apikey")
	return w.writeJSON(ctx, path, document, true)
}

func (w Writer) WriteClaude(ctx context.Context, path, baseURL, apiKey, model, smallFastModel string) error {
	document, err := loadJSON(path)
	if err != nil {
		return err
	}
	env, err := document.Child("env")
	if err != nil {
		return configError("Existing Claude Code env configuration must contain an object: %s", path)
	}
	env.Set("ANTHROPIC_BASE_URL", baseURL)
	env.Set("ANTHROPIC_AUTH_TOKEN", apiKey)
	env.Set("ANTHROPIC_MODEL", model)
	if smallFastModel == "" {
		smallFastModel = model
	}
	env.Set("ANTHROPIC_SMALL_FAST_MODEL", smallFastModel)
	return w.writeJSON(ctx, path, document, true)
}

// WriteOpenAICompatible writes the OpenCode/Kilo provider block, including the
// literal key under options.apiKey so the Agent needs no environment variable.
func (w Writer) WriteOpenAICompatible(ctx context.Context, path, schemaURL, providerName, baseURL, apiKey, model string) error {
	document, err := loadJSON(path)
	if err != nil {
		return err
	}
	document.Set("$schema", schemaURL)
	providers, err := document.Child("provider")
	if err != nil {
		return configError("Existing provider configuration must contain an object: %s", path)
	}
	options := jsonorder.NewObject()
	options.Set("baseURL", provider.OpenAIBaseURL(baseURL))
	options.Set("apiKey", apiKey)
	models := jsonorder.NewObject()
	modelEntry := jsonorder.NewObject()
	modelEntry.Set("name", model)
	models.Set(model, modelEntry)
	oneagent := jsonorder.NewObject()
	oneagent.Set("npm", "@ai-sdk/openai-compatible")
	oneagent.Set("name", providerName)
	oneagent.Set("options", options)
	oneagent.Set("models", models)
	providers.Set("oneagent", oneagent)
	document.Set("model", "oneagent/"+model)
	return w.writeJSON(ctx, path, document, true)
}

// WriteWorkBuddy adds or updates one custom OpenAI-compatible model while
// preserving every other model and field in ~/.workbuddy/models.json.
func (w Writer) WriteWorkBuddy(ctx context.Context, path, baseURL, apiKey, model string) error {
	text, err := readText(path)
	if err != nil {
		return configError("Cannot read existing WorkBuddy configuration %s: %v", path, err)
	}
	models := []map[string]any{}
	if strings.TrimSpace(text) != "" {
		if err := json.Unmarshal([]byte(text), &models); err != nil {
			return configError("Existing WorkBuddy configuration is invalid: %s: %v", path, err)
		}
		if models == nil {
			return configError("Existing WorkBuddy configuration is invalid: %s must contain a JSON array", path)
		}
	}
	entry := map[string]any{
		"id":                model,
		"name":              model,
		"vendor":            "Custom",
		"url":               baseURL,
		"apiKey":            apiKey,
		"supportsToolCall":  true,
		"supportsImages":    false,
		"supportsReasoning": false,
		"useCustomProtocol": false,
	}
	found := false
	for _, existing := range models {
		if id, _ := existing["id"].(string); id == model {
			for key, value := range entry {
				existing[key] = value
			}
			found = true
			break
		}
	}
	if !found {
		models = append(models, entry)
	}
	data, err := json.MarshalIndent(models, "", "  ")
	if err != nil {
		return configError("Cannot encode WorkBuddy configuration %s: %v", path, err)
	}
	return w.write(ctx, path, append(data, '\n'), true)
}

func (w Writer) WriteAider(ctx context.Context, path, baseURL, apiKey string) error {
	base := provider.OpenAIBaseURL(baseURL)
	content := "OPENAI_API_BASE=" + dotenvQuote(base) + "\n" +
		"OPENAI_API_KEY=" + dotenvQuote(apiKey) + "\n"
	return w.write(ctx, path, []byte(content), true)
}

func (w Writer) write(ctx context.Context, path string, data []byte, secret bool) error {
	if _, err := w.FS.AtomicWrite(ctx, path, data, secret); err != nil {
		return err
	}
	return nil
}

func (w Writer) writeJSON(ctx context.Context, path string, value *jsonorder.Object, secret bool) error {
	data, err := jsonorder.Marshal(value)
	if err != nil {
		return configError("Cannot encode JSON configuration %s: %v", path, err)
	}
	return w.write(ctx, path, data, secret)
}

func readText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func loadJSON(path string) (*jsonorder.Object, error) {
	text, err := readText(path)
	if err != nil {
		return nil, configError("Cannot read existing JSON configuration %s: %v", path, err)
	}
	if strings.TrimSpace(text) == "" {
		return jsonorder.NewObject(), nil
	}
	value, err := jsonorder.Parse([]byte(text))
	if err != nil {
		// OpenCode and Kilo parse their JSON with JSON5, so comments can appear in
		// a plain .json file too; the extension does not decide this.
		if jsoncCommentPattern.MatchString(text) {
			return nil, configError("%s contains JSONC comments, which OneAgent cannot preserve when it rewrites the file", path)
		}
		return nil, configError("Existing JSON configuration is invalid: %s: %v", path, err)
	}
	return value, nil
}

func mergeCodexTOML(existing, managed, path string) (string, error) {
	if strings.TrimSpace(existing) == "" {
		return managed, nil
	}
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(existing), &parsed); err != nil {
		return "", configError("Existing TOML configuration is invalid: %s: %v", path, err)
	}
	var topLevel, tables []string
	removed := map[string]bool{}
	managedFound := false
	inTable := false
	skipManaged := false
	for line := range strings.SplitSeq(existing, "\n") {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "[") {
			header := strings.ReplaceAll(strings.TrimSpace(strings.SplitN(stripped, "#", 2)[0]), " ", "")
			inTable = true
			skipManaged = managedSectionPattern.MatchString(header)
			if skipManaged {
				managedFound = true
				continue
			}
		}
		if skipManaged {
			continue
		}
		if !inTable {
			if match := topLevelKeyPattern.FindStringSubmatch(line); match != nil {
				removed[match[1]] = true
				continue
			}
			topLevel = append(topLevel, line)
		} else {
			tables = append(tables, line)
		}
	}
	if providers, ok := parsed["model_providers"].(map[string]any); ok {
		if _, exists := providers["oneagent"]; exists && !managedFound {
			return "", configError("Unsupported OneAgent TOML table syntax in %s", path)
		}
	}
	for _, key := range []string{"model_provider", "model"} {
		if _, exists := parsed[key]; exists && !removed[key] {
			return "", configError("Unsupported TOML key syntax for %s in %s", key, path)
		}
	}
	sections := make([]string, 0, 3)
	for _, section := range []string{strings.TrimSpace(strings.Join(topLevel, "\n")), strings.TrimSpace(managed), strings.TrimSpace(strings.Join(tables, "\n"))} {
		if section != "" {
			sections = append(sections, section)
		}
	}
	merged := strings.Join(sections, "\n\n") + "\n"
	var validation map[string]any
	if err := toml.Unmarshal([]byte(merged), &validation); err != nil {
		return "", configError("Cannot merge TOML configuration %s: %v", path, err)
	}
	return merged, nil
}

func quoteTOML(value string) string {
	return strconv.Quote(value)
}

func dotenvQuote(value string) string {
	if value != "" {
		safe := true
		for _, character := range value {
			if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-", character) {
				safe = false
				break
			}
		}
		if safe {
			return value
		}
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	return "'" + strings.ReplaceAll(escaped, "'", `\'`) + "'"
}

func configError(format string, values ...any) error {
	return oneerrors.New(oneerrors.ConfigWriteFailed, fmt.Sprintf(format, values...))
}

var (
	topLevelKeyPattern    = regexp.MustCompile(`^\s*(model_provider|model)\s*=`)
	managedSectionPattern = regexp.MustCompile(`^\[model_providers\.oneagent(?:\..+)?\]$`)
)
