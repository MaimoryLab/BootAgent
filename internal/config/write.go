package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
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
	"gopkg.in/yaml.v3"
)

type Writer struct {
	Home string
	OS   string
	FS   securefs.Store
}

// kimiOwnedName is the provider entry and model-alias prefix OneAgent owns in
// Kimi Code's config. Everything else under providers/models is the user's.
const kimiOwnedName = "oneagent"

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

// zcodeOwnedName labels the provider entry OneAgent maintains. ZCode keys custom
// providers by a generated UUID rather than by a stable name, so the name is the
// only field that can identify an earlier OneAgent write.
const zcodeOwnedName = "OneAgent"

// WriteZCode adds or updates OneAgent's provider entry in ~/.zcode/v2/config.json.
//
// It writes the provider only, and deliberately does not select a model. ZCode
// keeps no top-level "model" key and where it records the active model is neither
// documented nor anywhere it could be found on disk. Guessing at that would risk
// corrupting state the app owns, so the user picks the model once inside ZCode.
// This mirrors WriteWorkBuddy, which likewise registers a model service and
// leaves selection to the app.
//
// The shape below was read off ZCode 3.1.3. Nothing about it is documented -- the
// published docs describe GUI configuration and name no file path -- so it is an
// observation of one version, and 3.1.1 to 3.1.3 already moved: that release
// declared "$schema": "https://opencode.ai/config.json" and carried enabled /
// systemDisabledReason on builtin entries, all of which 3.1.3 dropped. Re-check
// against a current build before assuming any of it still holds.
//
// Existing entries are matched by name, not by key. Keying by a fresh UUID on
// every write would append a duplicate provider each time the user reconfigured.
//
// The entry is written as kind "openai". ZCode also accepts kind "anthropic" --
// its own builtin entries use it, pointed at https://api.z.ai/api/anthropic --
// but every Provider in providers.lock.json serves the OpenAI protocol while only
// some serve Anthropic, and the registry pins ZCode to openai for that reason.
// Supporting the other kind means giving the Definition a real protocol choice,
// not adding a branch here that nothing can reach.
func (w Writer) WriteZCode(ctx context.Context, path, providerName, baseURL, apiKey, model string) error {
	document, err := loadJSON(path)
	if err != nil {
		return err
	}
	// No "$schema" is written. ZCode 3.1.1 declared OpenCode's schema URL and
	// 3.1.3 dropped the key entirely, so writing it back would reintroduce a field
	// the current version chose to remove. Only the one provider entry below is
	// OneAgent's to manage; every other key in this file belongs to ZCode and is
	// left exactly as found.
	providers, err := document.Child("provider")
	if err != nil {
		return configError("Existing provider configuration must contain an object: %s", path)
	}

	options := jsonorder.NewObject()
	// ZCode stores a base URL, not a request path: the entries it writes itself
	// read like "https://api.z.ai/api/anthropic", with no /v1/messages suffix.
	options.Set("baseURL", provider.OpenAIBaseURL(baseURL))
	options.Set("apiKey", apiKey)
	// Without this ZCode treats the endpoint as needing no credential and sends
	// the request unauthenticated, which the Provider rejects as a missing key.
	options.Set("apiKeyRequired", true)

	entry := jsonorder.NewObject()
	entry.Set("name", zcodeOwnedName+" - "+providerName)
	entry.Set("kind", "openai")
	entry.Set("options", options)
	entry.Set("source", "custom")
	// The model is registered so it appears in ZCode's picker; which one is
	// active stays ZCode's decision.
	if model != "" {
		models := jsonorder.NewObject()
		models.Set(model, jsonorder.NewObject())
		entry.Set("models", models)
	}

	providers.Set(zcodeProviderKey(providers, entry.GetString("name")), entry)
	return w.writeJSON(ctx, path, document, true)
}

// zcodeNewKey derives the provider key from the name instead of generating a
// random UUID. ZCode's own custom entries use UUIDs, but nothing reads them as
// one: the key is an opaque identifier. A derived key makes a write idempotent
// even if the file is replaced between runs, and it stays readable in a config a
// user may open.
func zcodeNewKey(name string) string {
	digest := sha256.Sum256([]byte(name))
	return "oneagent-" + hex.EncodeToString(digest[:8])
}

// zcodeProviderKey reuses the key of a provider already carrying this name, so a
// second write updates that entry instead of adding another. A builtin:* entry is
// never reused: those belong to ZCode and are keyed by a name it controls.
func zcodeProviderKey(providers *jsonorder.Object, name string) string {
	for _, key := range providers.Keys() {
		if strings.HasPrefix(key, "builtin:") {
			continue
		}
		existing, ok := providers.GetObject(key)
		if !ok {
			continue
		}
		if existing.GetString("name") == name {
			return key
		}
	}
	return zcodeNewKey(name)
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
			maps.Copy(existing, entry)
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

// WriteOpenClaw points OpenClaw's model provider at the Provider, writing one
// named entry under models.providers in ~/.openclaw/openclaw.json.
//
// Scope is deliberately narrower than the rest of the file: this writes the
// provider and the default model, and nothing else. OpenClaw is a gateway whose
// own commands own the daemon, the channel pairing and the Control UI, so
// OneAgent does not touch `agents`, `channels` or `tools` here -- those are the
// parts a user configures through `openclaw onboard`, and rewriting them from a
// Provider record would overwrite decisions OneAgent has no input on.
//
// Credentials sit at the top of the provider entry in camelCase (baseUrl,
// apiKey), matching what OpenClaw reads. `api` names the wire protocol; the
// catalog maps this adapter to OpenAI Chat Completions, so it is fixed here
// rather than derived, and changing one without the other would write a config
// whose declared protocol does not match the endpoint OneAgent probed.
func (w Writer) WriteOpenClaw(ctx context.Context, path, providerName, baseURL, apiKey, model string) error {
	document, err := loadJSON(path)
	if err != nil {
		return err
	}
	models, err := document.Child("models")
	if err != nil {
		return configError("Existing OpenClaw models configuration must contain an object: %s", path)
	}
	providers, err := models.Child("providers")
	if err != nil {
		return configError("Existing OpenClaw provider configuration must contain an object: %s", path)
	}
	entry := jsonorder.NewObject()
	entry.Set("name", providerName)
	entry.Set("baseUrl", provider.OpenAIBaseURL(baseURL))
	entry.Set("apiKey", apiKey)
	entry.Set("api", "openai-completions")
	modelEntry := jsonorder.NewObject()
	modelEntry.Set("id", model)
	entry.Set("models", []any{modelEntry})
	providers.Set("oneagent", entry)
	// OpenClaw addresses a model as "<provider-key>/<model-id>", so the default
	// has to name the entry written above, not the bare model id.
	agents, err := document.Child("agents")
	if err != nil {
		return configError("Existing OpenClaw agents configuration must contain an object: %s", path)
	}
	defaults, err := agents.Child("defaults")
	if err != nil {
		return configError("Existing OpenClaw agent defaults must contain an object: %s", path)
	}
	defaultModel, err := defaults.Child("model")
	if err != nil {
		return configError("Existing OpenClaw default model must contain an object: %s", path)
	}
	defaultModel.Set("primary", "oneagent/"+model)
	return w.writeJSON(ctx, path, document, true)
}

func (w Writer) WriteAider(ctx context.Context, path, baseURL, apiKey string) error {
	base := provider.OpenAIBaseURL(baseURL)
	content := "OPENAI_API_BASE=" + dotenvQuote(base) + "\n" +
		"OPENAI_API_KEY=" + dotenvQuote(apiKey) + "\n"
	return w.write(ctx, path, []byte(content), true)
}

// WriteKimiCode points Kimi Code at the Provider through one owned provider
// entry plus one model alias in ~/.kimi-code/config.toml.
//
// The credential goes in the same file as the endpoint, unlike Codex: Kimi Code
// documents that it reads credentials only from its config and does not fall
// back to shell environment variables, so a config without the key would leave
// the CLI failing at startup with nothing to fall back to. The whole file is
// written as a secret for that reason.
//
// type is "openai", not Kimi Code's own "kimi": that one means Moonshot's
// first-party endpoint, while a OneAgent Provider is an arbitrary
// OpenAI-compatible base URL.
func (w Writer) WriteKimiCode(ctx context.Context, path, baseURL, apiKey, model string) error {
	// The alias is namespaced under the owned provider name so it cannot collide
	// with a model the user declared. Both table headers are quoted: the alias
	// contains a slash, and a model ID may contain a dot, either of which TOML
	// would otherwise read as a nested table separator.
	alias := kimiOwnedName + "/" + model
	managed := strings.Join([]string{
		"default_model = " + quoteTOML(alias),
		"",
		"[providers." + quoteTOML(kimiOwnedName) + "]",
		"type = \"openai\"",
		"base_url = " + quoteTOML(provider.OpenAIBaseURL(baseURL)),
		"api_key = " + quoteTOML(apiKey),
		"",
		"[models." + quoteTOML(alias) + "]",
		"provider = " + quoteTOML(kimiOwnedName),
		"model = " + quoteTOML(model),
		"",
	}, "\n")
	existing, err := readText(path)
	if err != nil {
		return configError("Cannot read existing TOML configuration %s: %v", path, err)
	}
	content, err := mergeManagedTOML(existing, managed, path, managedTOMLShape{
		topLevelKeys:   kimiTopLevelKeyPattern,
		managedSection: kimiManagedSectionPattern,
		ownedKeys:      []string{"default_model"},
		ownedTables:    [][2]string{{"providers", kimiOwnedName}},
	})
	if err != nil {
		return err
	}
	return w.write(ctx, path, []byte(content), true)
}

func (w Writer) WriteHermes(ctx context.Context, path, baseURL, apiKey, model string) error {
	text, err := readText(path)
	if err != nil {
		return configError("Cannot read existing YAML configuration %s: %v", path, err)
	}
	root := &yaml.Node{}
	if strings.TrimSpace(text) == "" {
		root = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	} else if err := yaml.Unmarshal([]byte(text), root); err != nil {
		return configError("Existing YAML configuration is invalid: %s: %v", path, err)
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return configError("Existing YAML configuration must contain an object: %s", path)
	}
	modelConfig, err := yamlMapping(root.Content[0], "model")
	if err != nil {
		return configError("Existing Hermes model configuration must contain an object: %s", path)
	}
	for _, item := range []struct{ key, value string }{
		{"default", model},
		{"provider", "custom"},
		{"api_key", apiKey},
		{"base_url", provider.OpenAIBaseURL(baseURL)},
	} {
		yamlSet(modelConfig, item.key, item.value)
	}
	data, err := yaml.Marshal(root)
	if err != nil {
		return configError("Cannot encode YAML configuration %s: %v", path, err)
	}
	return w.write(ctx, path, data, true)
}

func yamlMapping(parent *yaml.Node, key string) (*yaml.Node, error) {
	for index := 0; index < len(parent.Content); index += 2 {
		if parent.Content[index].Value != key {
			continue
		}
		value := parent.Content[index+1]
		if value.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s is not a mapping", key)
		}
		return value, nil
	}
	value := &yaml.Node{Kind: yaml.MappingNode}
	parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
	return value, nil
}

func yamlSet(parent *yaml.Node, key, value string) {
	for index := 0; index < len(parent.Content); index += 2 {
		if parent.Content[index].Value == key {
			parent.Content[index+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
			return
		}
	}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
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
	return mergeManagedTOML(existing, managed, path, managedTOMLShape{
		topLevelKeys:   topLevelKeyPattern,
		managedSection: managedSectionPattern,
		ownedKeys:      []string{"model_provider", "model"},
		ownedTables:    [][2]string{{"model_providers", "oneagent"}},
	})
}

// MergeCodexMCPTOML preserves user-owned Codex TOML while replacing one
// complete MCP server section supplied by the caller.
func MergeCodexMCPTOML(existing, managed, path, serverID string) (string, error) {
	section := regexp.QuoteMeta(serverID)
	return mergeManagedTOML(existing, managed, path, managedTOMLShape{
		topLevelKeys:   regexp.MustCompile(`^$`),
		managedSection: regexp.MustCompile(`^\[mcp_servers\.` + section + `(?:\..+)?\]$`),
	})
}

// managedTOMLShape names the parts of a TOML config OneAgent owns, so the merge
// below can serve Agents whose file layout differs. Anything it does not name is
// carried through untouched.
type managedTOMLShape struct {
	// topLevelKeys matches the bare keys OneAgent rewrites, so a stale value
	// above the first table header is dropped rather than duplicated.
	topLevelKeys *regexp.Regexp
	// managedSection matches the table headers OneAgent owns outright; their
	// bodies are discarded and replaced.
	managedSection *regexp.Regexp
	// ownedKeys and ownedTables are the same ground truth expressed against the
	// parsed document, which is how an inline-table or dotted-key spelling of a
	// managed value gets rejected instead of silently duplicated. A table is
	// {parent, child}.
	ownedKeys   []string
	ownedTables [][2]string
}

func mergeManagedTOML(existing, managed, path string, shape managedTOMLShape) (string, error) {
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
			skipManaged = shape.managedSection.MatchString(header)
			if skipManaged {
				managedFound = true
				continue
			}
		}
		if skipManaged {
			continue
		}
		if !inTable {
			if match := shape.topLevelKeys.FindStringSubmatch(line); len(match) > 1 {
				removed[match[1]] = true
				continue
			}
			topLevel = append(topLevel, line)
		} else {
			tables = append(tables, line)
		}
	}
	for _, owned := range shape.ownedTables {
		parent, ok := parsed[owned[0]].(map[string]any)
		if !ok {
			continue
		}
		if _, exists := parent[owned[1]]; exists && !managedFound {
			return "", configError("Unsupported OneAgent TOML table syntax in %s", path)
		}
	}
	for _, key := range shape.ownedKeys {
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

	kimiTopLevelKeyPattern = regexp.MustCompile(`^\s*(default_model)\s*=`)
	// Both tables OneAgent owns, quoted or bare: providers.oneagent, and every
	// models."oneagent/…" alias it has written. Matching every alias rather than
	// just the current one is what stops a model switch from leaving the previous
	// alias behind, pointing at a provider entry that no longer describes it.
	//
	// The quotes are matched independently on each side because RE2 has no
	// backreferences. A mismatched pair is not valid TOML, so it cannot reach
	// here from a file that parsed.
	kimiManagedSectionPattern = regexp.MustCompile(`^\[(?:providers\."?oneagent"?|models\."?oneagent/.+)\]$`)
)
