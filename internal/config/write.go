package config

import (
	"context"
	"fmt"
	"os"
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

func (w Writer) WriteCodex(ctx context.Context, path, providerName, baseURL, model string) error {
	managed := strings.Join([]string{
		"model_provider = \"oneagent\"",
		"model = " + quoteTOML(model),
		"",
		"[model_providers.oneagent]",
		"name = " + quoteTOML(providerName),
		"base_url = " + quoteTOML(baseURL),
		"env_key = " + quoteTOML(agentEnvVar("codex")),
		"wire_api = \"responses\"",
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
	return w.write(ctx, path, []byte(content), false)
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

func (w Writer) WriteOpenAICompatible(ctx context.Context, path, schemaURL, providerName, baseURL, model, agentID string) error {
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
	options.Set("apiKey", "{env:"+agentEnvVar(agentID)+"}")
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
	return w.writeJSON(ctx, path, document, false)
}

func (w Writer) WriteAider(ctx context.Context, path, baseURL, apiKey string) error {
	base := provider.OpenAIBaseURL(baseURL)
	var content string
	if w.OS == "windows" {
		content = "$env:OPENAI_API_BASE = '" + powershellQuote(base) + "'\n" +
			"$env:OPENAI_API_KEY = '" + powershellQuote(apiKey) + "'\n"
	} else {
		content = "export OPENAI_API_BASE=" + shellQuote(base) + "\n" +
			"export OPENAI_API_KEY=" + shellQuote(apiKey) + "\n"
	}
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
		if strings.HasSuffix(path, ".jsonc") && jsoncCommentPattern.MatchString(text) {
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
	for _, line := range strings.Split(existing, "\n") {
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

func shellQuote(value string) string {
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
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func powershellQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func configError(format string, values ...any) error {
	return oneerrors.New(oneerrors.ConfigWriteFailed, fmt.Sprintf(format, values...))
}

var (
	topLevelKeyPattern    = regexp.MustCompile(`^\s*(model_provider|model)\s*=`)
	managedSectionPattern = regexp.MustCompile(`^\[model_providers\.oneagent(?:\..+)?\]$`)
)
