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

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/jsonorder"
	"github.com/MaimoryLab/BootAgent/internal/provider"
	"github.com/MaimoryLab/BootAgent/internal/securefs"
	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

type Writer struct {
	Home string
	OS   string
	FS   securefs.Store
}

// kimiOwnedName is the provider entry and model-alias prefix BootAgent owns in
// Kimi Code's config. Everything else under providers/models is the user's.
const kimiOwnedName = "bootagent"

const (
	// dshOwnedRoute is the llm-pi-ai provider route BootAgent owns in DeepSeek
	// Harness's settings. Every other route under providers is the user's, whether
	// they declared it by hand or through dsh's Models page.
	dshOwnedRoute = "bootagent"
	// dshCredentialReference is the credential this route resolves its key
	// through. In BootAgent's own namespace rather than DEEPSEEK_API_KEY so
	// storing it cannot disturb the credential dsh's Models page manages for the
	// shipped DeepSeek route.
	dshCredentialReference = "BOOTAGENT_API_KEY"
	// dshOfficialRoute is the provider route dsh itself ships for DeepSeek's
	// first-party service. Not BootAgent's to declare or replace -- WriteDSHOfficial
	// only points the default selection at it and stores the key where it looks.
	dshOfficialRoute = "deepseek-official"
	// dshOfficialCredential is the credential the shipped route resolves its key
	// through. The same entry dsh's own Models page manages, which is the point:
	// an activation against DeepSeek's own service must land the key where that
	// route already reads it.
	dshOfficialCredential = "DEEPSEEK_API_KEY"
)

// dshOfficialReasoningEfforts is what dsh's llm-deepseek adapter accepts as a
// reasoningEffort, read off @deepseek-ai/dsh-llm-deepseek (serialize.d.ts
// declares 'off' | 'high' | 'max'; an absent effort resolves to the adapter
// default, high). Deliberately NOT the seven-level scale llm-pi-ai declares:
// the shipped official route dispatches through llm-deepseek, and a level like
// "medium" that pi-ai would accept fails there.
var dshOfficialReasoningEfforts = []string{"off", "high", "max"}

// ValidateDSHOfficialReasoningEffort rejects an effort the shipped DeepSeek
// route cannot dispatch. Validated at write time rather than left to dsh:
// dsh's settings schema accepts any string, and the failure would otherwise
// surface as an UNSUPPORTED_REASONING_EFFORT error on the user's first request
// -- far from the Profile edit that caused it.
func ValidateDSHOfficialReasoningEffort(effort string) error {
	for _, allowed := range dshOfficialReasoningEfforts {
		if effort == allowed {
			return nil
		}
	}
	return oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf(
		"DeepSeek reasoning effort must be one of %s, got %q",
		strings.Join(dshOfficialReasoningEfforts, ", "), effort))
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
		"model_provider = \"bootagent\"",
		"model = " + quoteTOML(model),
		"",
		"[model_providers.bootagent]",
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
	bootagent := jsonorder.NewObject()
	bootagent.Set("npm", "@ai-sdk/openai-compatible")
	bootagent.Set("name", providerName)
	bootagent.Set("options", options)
	bootagent.Set("models", models)
	providers.Set("bootagent", bootagent)
	document.Set("model", "bootagent/"+model)
	return w.writeJSON(ctx, path, document, true)
}

// zcodeOwnedName labels the provider entry BootAgent maintains. ZCode keys custom
// providers by a generated UUID rather than by a stable name, so the name is the
// only field that can identify an earlier BootAgent write.
const zcodeOwnedName = "BootAgent"

// WriteZCode adds or updates BootAgent's provider entry in ~/.zcode/v2/config.json.
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
	// BootAgent's to manage; every other key in this file belongs to ZCode and is
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
	return "bootagent-" + hex.EncodeToString(digest[:8])
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
// BootAgent does not touch `agents`, `channels` or `tools` here -- those are the
// parts a user configures through `openclaw onboard`, and rewriting them from a
// Provider record would overwrite decisions BootAgent has no input on.
//
// Credentials sit at the top of the provider entry in camelCase (baseUrl,
// apiKey), matching what OpenClaw reads. `api` names the wire protocol; the
// catalog maps this adapter to OpenAI Chat Completions, so it is fixed here
// rather than derived, and changing one without the other would write a config
// whose declared protocol does not match the endpoint BootAgent probed.
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
	providers.Set("bootagent", entry)
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
	defaultModel.Set("primary", "bootagent/"+model)
	return w.writeJSON(ctx, path, document, true)
}

// WriteDSH points DeepSeek Harness at the Provider through a hand-declared
// pi-ai route in $DSH_HOME/settings.yaml, with the credential in
// $DSH_HOME/.credentials.yaml. For DeepSeek's own service the caller uses
// WriteDSHOfficial instead; deciding which is which takes the Provider catalog,
// so that choice stays with the caller.
//
// Those two files rather than $DSH_HOME/.env, which dsh also reads: .env is the
// *lowest* of four credential layers, and the managed .credentials.yaml document
// outranks it. dsh's own Models page writes that document, so a key BootAgent
// left in .env is shadowed the moment the user stores one there -- silently, and
// permanently. The endpoint loses the same way: llm-deepseek reads
// $DEEPSEEK_BASE_URL only when no `llm-deepseek:` settings section overrides it,
// and that section is what the same page writes.
//
// Not an override of the shipped deepseek-official route, for the same reason:
// that route is branded DeepSeek and advertises DeepSeek's model ids, so
// repointing it at an arbitrary gateway would misrepresent both the provider and
// the models. A separate route carries the Provider's own name instead. dsh
// mounts llm-pi-ai dormant precisely so a settings section can register routes
// into it, and picks them up without a restart.
//
// The models list is seeded with the resolved model as the route's only entry: a
// hand-declared route has no installed catalog to inherit, so an absent list
// would advertise nothing and leave the picker empty. The user can add more from
// dsh's Models page, which writes this same section.
//
// The agent-default-model selection is repointed at the route too, because
// registering one only makes it available -- dsh resolves every new Agent from
// that selection, and the user can still change it from the same page.
//
// The endpoint goes in with OpenAIBaseURL's /v1 rather than bare, because the
// adapter appends only the operation path to whatever it is given.
func (w Writer) WriteDSH(ctx context.Context, path, providerName, baseURL, apiKey, model string) error {
	// The credential lands first: a route pointing at a provider dsh cannot
	// authenticate is worse than an unreferenced key.
	if err := w.writeDSHCredential(ctx, filepath.Join(filepath.Dir(path), ".credentials.yaml"), dshCredentialReference, apiKey); err != nil {
		return err
	}
	root, err := yamlDocument(path, "DeepSeek Harness settings")
	if err != nil {
		return err
	}
	adapter, err := yamlMapping(root.Content[0], "llm-pi-ai")
	if err != nil {
		return configError("Existing llm-pi-ai settings must contain an object: %s", path)
	}
	providers, err := yamlMapping(adapter, "providers")
	if err != nil {
		return configError("Existing llm-pi-ai providers must contain an object: %s", path)
	}
	route := &yaml.Node{Kind: yaml.MappingNode}
	for _, item := range []struct{ key, value string }{
		{"displayName", providerName},
		{"apiKeyEnv", dshCredentialReference},
		{"api", "openai-completions"},
		{"baseURL", provider.OpenAIBaseURL(baseURL)},
	} {
		yamlSet(route, item.key, item.value)
	}
	entry := &yaml.Node{Kind: yaml.MappingNode}
	yamlSet(entry, "id", model)
	route.Content = append(route.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "models"},
		&yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{entry}},
	)
	// Replaced wholesale rather than merged field by field: a stale key from an
	// earlier activation surviving inside our own route would outlive the write
	// that was supposed to redefine it. Every other route stays untouched.
	yamlReplace(providers, dshOwnedRoute, route)

	// Registering the route only makes it *available*. dsh creates every Agent
	// against the agent-default-model selection, so leaving that pointed at the
	// shipped deepseek-official route would mean activation changed nothing the
	// user can observe -- the same "configured but not in effect" failure as
	// writing the endpoint into a layer something else overrides.
	//
	// Replaced wholesale rather than merged, and reasoningEffort deliberately
	// dropped: dsh treats a saved selection as complete, and an effort left over
	// from a DeepSeek model is not one the seeded model declares.
	selection := &yaml.Node{Kind: yaml.MappingNode}
	yamlSet(selection, "provider", dshOwnedRoute)
	yamlSet(selection, "model", model)
	yamlReplace(root.Content[0], "agent-default-model", selection)

	data, err := yaml.Marshal(root)
	if err != nil {
		return configError("Cannot encode YAML configuration %s: %v", path, err)
	}
	return w.write(ctx, path, data, false)
}

// WriteDSHOfficial points DeepSeek Harness at DeepSeek's own service through
// the deepseek-official route dsh ships, instead of declaring a bootagent route
// the way WriteDSH does.
//
// The shipped route already knows the endpoint and carries DeepSeek's installed
// model catalog, so a hand-declared duplicate would add nothing and misfile the
// credential: that route resolves its key through DEEPSEEK_API_KEY, the same
// entry dsh's Models page manages, so that is where the key must land for the
// route to authenticate. This is also why no endpoint is written -- the route's
// endpoint is dsh's own fact, and the caller only chooses this path when the
// Provider's endpoint is exactly the one the route already uses.
//
// A bootagent route left behind by an earlier activation against a different
// Provider is removed: BootAgent owns that route, and after this write it would
// point at an endpoint and credential this activation just replaced --
// ReadDSHConfig would then report that stale route as the current binding. The
// BOOTAGENT_API_KEY credential itself stays: once the route is gone it is
// unreferenced and harmless, and .credentials.yaml is shared with dsh's Models
// page, so this write touches only the entry it has to.
//
// The agent-default-model selection is replaced wholesale for the same reason
// as in WriteDSH. reasoningEffort is written when given and the model is a
// DeepSeek model that supports it; a stale effort from a previous model is
// dropped when reasoningEffort is empty, because dsh treats a saved selection
// as complete and would otherwise keep sending an effort this model never
// declared.
func (w Writer) WriteDSHOfficial(ctx context.Context, path, apiKey, model, reasoningEffort string) error {
	// The credential lands first: a selection pointing at a route dsh cannot
	// authenticate is worse than an unreferenced key.
	if err := w.writeDSHCredential(ctx, filepath.Join(filepath.Dir(path), ".credentials.yaml"), dshOfficialCredential, apiKey); err != nil {
		return err
	}
	root, err := yamlDocument(path, "DeepSeek Harness settings")
	if err != nil {
		return err
	}
	// Lookup without creating: when no bootagent route exists there is nothing
	// to clean up, and checking must not leave an empty llm-pi-ai section behind
	// as a side effect.
	if adapter := yamlChild(root.Content[0], "llm-pi-ai"); adapter != nil {
		if providers := yamlChild(adapter, "providers"); providers != nil {
			yamlDelete(providers, dshOwnedRoute)
		}
	}
	selection := &yaml.Node{Kind: yaml.MappingNode}
	yamlSet(selection, "provider", dshOfficialRoute)
	yamlSet(selection, "model", model)
	if reasoningEffort != "" {
		if err := ValidateDSHOfficialReasoningEffort(reasoningEffort); err != nil {
			return err
		}
		yamlSet(selection, "reasoningEffort", reasoningEffort)
	}
	yamlReplace(root.Content[0], "agent-default-model", selection)

	data, err := yaml.Marshal(root)
	if err != nil {
		return configError("Cannot encode YAML configuration %s: %v", path, err)
	}
	return w.write(ctx, path, data, false)
}

// writeDSHCredential merges the key into the managed credential document under
// the given reference, keeping every other credential the user stored from
// dsh's Models page.
//
// The document is a strict credential-to-value mapping: dsh rejects a non-string
// value, an empty string, or a key that is not a POSIX identifier, and fails loud
// rather than skipping the entry. So this writes one identifier and nothing else
// -- no wrapper level, no version field.
func (w Writer) writeDSHCredential(ctx context.Context, path, reference, apiKey string) error {
	root, err := yamlDocument(path, "DeepSeek Harness credentials")
	if err != nil {
		return err
	}
	yamlSet(root.Content[0], reference, apiKey)
	data, err := yaml.Marshal(root)
	if err != nil {
		return configError("Cannot encode YAML credentials %s: %v", path, err)
	}
	return w.write(ctx, path, data, true)
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
// first-party endpoint, while a BootAgent Provider is an arbitrary
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
	root, err := yamlDocument(path, "YAML configuration")
	if err != nil {
		return err
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

// yamlReplace overwrites one key's whole value node, appending it when absent.
// Distinct from yamlSet, which carries a scalar: this one hands over a subtree
// BootAgent owns outright, so nothing of the previous value is kept.
func yamlReplace(parent *yaml.Node, key string, value *yaml.Node) {
	for index := 0; index < len(parent.Content); index += 2 {
		if parent.Content[index].Value == key {
			parent.Content[index+1] = value
			return
		}
	}
	parent.Content = append(parent.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, value)
}

// yamlChild returns the mapping stored under key, or nil when the key is absent
// or holds something else. Unlike yamlMapping it never creates the entry, so a
// lookup made only to delete from it cannot leave an empty section behind.
func yamlChild(parent *yaml.Node, key string) *yaml.Node {
	for index := 0; index < len(parent.Content); index += 2 {
		if parent.Content[index].Value == key && parent.Content[index+1].Kind == yaml.MappingNode {
			return parent.Content[index+1]
		}
	}
	return nil
}

// yamlDelete removes one key and its value from a mapping; an absent key is a
// no-op.
func yamlDelete(parent *yaml.Node, key string) {
	for index := 0; index < len(parent.Content); index += 2 {
		if parent.Content[index].Value == key {
			parent.Content = append(parent.Content[:index], parent.Content[index+2:]...)
			return
		}
	}
}

// yamlDocument parses a YAML file into a node tree, so comments, key order, and
// the formatting of everything BootAgent does not touch survive the rewrite. An
// absent or blank file starts an empty mapping rather than failing: the caller is
// about to write the first entry either way. The label names the document in the
// error a user would see.
func yamlDocument(path, label string) (*yaml.Node, error) {
	text, err := readText(path)
	if err != nil {
		return nil, configError("Cannot read existing %s %s: %v", label, path, err)
	}
	if strings.TrimSpace(text) == "" {
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}, nil
	}
	root := &yaml.Node{}
	if err := yaml.Unmarshal([]byte(text), root); err != nil {
		return nil, configError("Existing %s is invalid: %s: %v", label, path, err)
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return nil, configError("Existing %s must contain an object: %s", label, path)
	}
	return root, nil
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
			return nil, configError("%s contains JSONC comments, which BootAgent cannot preserve when it rewrites the file", path)
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
		ownedTables:    [][2]string{{"model_providers", "bootagent"}},
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

// managedTOMLShape names the parts of a TOML config BootAgent owns, so the merge
// below can serve Agents whose file layout differs. Anything it does not name is
// carried through untouched.
type managedTOMLShape struct {
	// topLevelKeys matches the bare keys BootAgent rewrites, so a stale value
	// above the first table header is dropped rather than duplicated.
	topLevelKeys *regexp.Regexp
	// managedSection matches the table headers BootAgent owns outright; their
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
			return "", configError("Unsupported BootAgent TOML table syntax in %s", path)
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
	managedSectionPattern = regexp.MustCompile(`^\[model_providers\.bootagent(?:\..+)?\]$`)

	kimiTopLevelKeyPattern = regexp.MustCompile(`^\s*(default_model)\s*=`)
	// Both tables BootAgent owns, quoted or bare: providers.bootagent, and every
	// models."bootagent/…" alias it has written. Matching every alias rather than
	// just the current one is what stops a model switch from leaving the previous
	// alias behind, pointing at a provider entry that no longer describes it.
	//
	// The quotes are matched independently on each side because RE2 has no
	// backreferences. A mismatched pair is not valid TOML, so it cannot reach
	// here from a file that parsed.
	kimiManagedSectionPattern = regexp.MustCompile(`^\[(?:providers\."?bootagent"?|models\."?bootagent/.+)\]$`)
)
