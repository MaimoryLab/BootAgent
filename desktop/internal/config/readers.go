package config

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/jsonorder"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// Detected is one Agent's configuration as found on disk.
//
// It deliberately carries no credential and no hint that one exists. Three of
// the five formats hold the key in plain text, so a field about it would reach
// the status payload -- and even a boolean would leak whether this machine has a
// key configured. The field set is asserted by a test for that reason.
type Detected struct {
	BaseURL string `json:"baseUrl"`
	Model   string `json:"model"`
	// ManagedByOneAgent means the file looks like one OneAgent wrote, which is
	// what tells "we configured this" from "someone else did".
	ManagedByOneAgent bool `json:"managedByOneAgent"`
	// Unreadable is a reason, never the file's contents. Null when the file was
	// read successfully, matching the JSON the frontend already consumes.
	Unreadable *string `json:"unreadable"`
}

// detected builds a successful reading.
func detected(baseURL, model string, managed bool) Detected {
	return Detected{BaseURL: baseURL, Model: model, ManagedByOneAgent: managed}
}

// unreadable builds a failed reading. The messages are user-visible Chinese and
// are reproduced verbatim from the Python core, because the frontend matches on
// nothing but shows them directly.
func unreadable(reason string) Detected {
	return Detected{Unreadable: &reason}
}

// ReadCodexConfig reads a Codex config.toml.
func ReadCodexConfig(text string) Detected {
	var parsed map[string]any
	if _, err := toml.Decode(text, &parsed); err != nil {
		return unreadable("TOML 无法解析：" + err.Error())
	}
	providers, _ := parsed["model_providers"].(map[string]any)

	// Follow the provider the file actually selects, which is not necessarily
	// ours: reading [model_providers.oneagent] unconditionally would report a
	// configuration the Agent is not using.
	baseURL := ""
	if selected, ok := parsed["model_provider"].(string); ok {
		if table, ok := providers[selected].(map[string]any); ok {
			if value, ok := table["base_url"].(string); ok {
				baseURL = value
			}
		}
	}
	model, _ := parsed["model"].(string)
	_, managed := providers["oneagent"]
	return detected(baseURL, model, managed)
}

// ReadClaudeConfig reads Claude Code's settings.json.
func ReadClaudeConfig(text string) Detected {
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return unreadable("JSON 无法解析：" + err.Error())
	}
	object, ok := parsed.(map[string]any)
	if !ok {
		return unreadable("配置的顶层不是对象")
	}
	env, _ := object["env"].(map[string]any)

	declared := map[string]string{}
	if agent, present := catalog.MustLoad().Agent("claude-code"); present {
		declared = agent.EnvVars
	}
	baseURLName := declared["base_url"]
	if baseURLName == "" {
		baseURLName = "ANTHROPIC_BASE_URL"
	}
	modelName := declared["model"]
	if modelName == "" {
		modelName = "ANTHROPIC_MODEL"
	}

	baseURL, _ := env[baseURLName].(string)
	model, _ := env[modelName].(string)

	// Managed means every variable the adapter writes is present. A file holding
	// only a base URL was configured by someone else, and claiming it as ours
	// would make Apply look safe when it would overwrite their work.
	managed := len(declared) > 0
	for _, name := range declared {
		value, ok := env[name].(string)
		if !ok || value == "" {
			managed = false
			break
		}
	}
	return detected(baseURL, model, managed)
}

// jsoncCommentInText finds a comment, which distinguishes valid JSONC from
// broken JSON.
var jsoncCommentInText = regexp.MustCompile(`(?:^|\s)(?://|/\*)`)

// ReadOpenAICompatibleConfig reads the OpenCode and Kilo format.
func ReadOpenAICompatibleConfig(text string) Detected {
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		// A .jsonc file with comments is valid to its Agent and invalid here.
		// Saying which it is gives the user something to act on.
		if jsoncCommentInText.MatchString(text) {
			return unreadable("包含 JSONC 注释，OneAgent 不解析")
		}
		return unreadable("JSON 无法解析：" + err.Error())
	}
	object, ok := parsed.(map[string]any)
	if !ok {
		return unreadable("配置的顶层不是对象")
	}
	providers, _ := object["provider"].(map[string]any)
	model, _ := object["model"].(string)

	// model is "<provider>/<model>"; the provider half names which table holds
	// the endpoint, so it selects rather than decorates.
	selected, bareModel, hasSlash := strings.Cut(model, "/")
	baseURL := ""
	if selected != "" {
		if table, ok := providers[selected].(map[string]any); ok {
			if options, ok := table["options"].(map[string]any); ok {
				if value, ok := options["baseURL"].(string); ok {
					baseURL = value
				}
			}
		}
	}
	reportedModel := bareModel
	if !hasSlash || bareModel == "" {
		reportedModel = model
	}
	_, managed := providers["oneagent"]
	return detected(baseURL, reportedModel, managed)
}

// aiderAssignment matches the endpoint line in either shell's syntax.
var aiderAssignment = regexp.MustCompile(`^(?:export\s+OPENAI_API_BASE=|\$env:OPENAI_API_BASE\s*=\s*)(.+)$`)

// ReadAiderConfig parses the shell script the Aider adapter writes.
//
// Parsed line by line and never executed: the file is written with the key in
// it, so running it would both leak the credential into the environment and hand
// arbitrary shell from a user-editable file to an interpreter.
//
// ManagedByOneAgent is always false, deliberately. The other four Agents keep
// their config where they look for it, so a OneAgent-shaped marker inside
// distinguishes our write from someone else's. Aider's config is a script that
// only exists because we created it, and its whole content is the two exports --
// there is no marker a hand-written equivalent would lack. Claiming to tell them
// apart would be a guess.
func ReadAiderConfig(text string) Detected {
	baseURL := ""
	for _, line := range splitLines(text) {
		match := aiderAssignment.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		raw := strings.TrimSpace(match[1])
		if len(raw) >= 2 && raw[0] == raw[len(raw)-1] && (raw[0] == '\'' || raw[0] == '"') {
			raw = strings.ReplaceAll(raw[1:len(raw)-1], "''", "'")
		}
		// The last assignment wins, matching a shell sourcing the file.
		baseURL = raw
	}
	return detected(baseURL, "", false)
}

// configReaders maps an adapter to its reader. Keyed on the adapter, like the
// writers, so two Agents sharing a format share the reader.
var configReaders = map[string]func(string) Detected{
	AdapterCodex:      ReadCodexConfig,
	AdapterClaudeCode: ReadClaudeConfig,
	AdapterOpenCode:   ReadOpenAICompatibleConfig,
	AdapterKiloCLI:    ReadOpenAICompatibleConfig,
	AdapterAider:      ReadAiderConfig,
}

// DetectAgentConfig reports what an Agent is configured to use, read from its own
// config file.
//
// Separate from the per-Agent binding OneAgent keeps: that binding is our own
// bookkeeping and exists only for configurations we wrote. A user who configured
// Codex by hand had status report configured with provider, model and baseUrl all
// null -- the file was seen but never read.
//
// Returns nil when there is nothing to report, and never panics: a config edited
// into invalid TOML used to fail the whole status request, which blanked the
// entire UI over one broken file.
func DetectAgentConfig(rt *runtime.Runtime, agent catalog.Agent) (result *Detected) {
	if agent.GuideOnly() {
		return nil
	}
	reader, known := configReaders[agent.ConfigAdapter]
	if !known {
		// A lock entry may name an adapter before a reader exists. The honest
		// answer is that we cannot read it, not a guessed endpoint.
		value := unreadable("没有可用的配置解析器")
		return &value
	}
	path := ConfigPath(rt, agent)
	if path == "" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		// The reason, not the contents.
		value := unreadable("无法读取：" + readErrorReason(err))
		return &value
	}
	text := string(raw)
	if strings.TrimSpace(text) == "" {
		value := unreadable("配置文件为空")
		return &value
	}

	// A reader must not be able to break the status request. Python has a bare
	// except for this; recover is the equivalent, and the guarantee matters more
	// than any single reader being correct.
	defer func() {
		if recovered := recover(); recovered != nil {
			value := unreadable("配置解析失败")
			result = &value
		}
	}()
	value := reader(text)
	return &value
}

// readErrorReason extracts the part of an OS error worth showing. Python reports
// strerror, which omits the path -- and the path is already known to the caller,
// so repeating it only makes the message longer.
func readErrorReason(err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
}

// DetectedFieldNames lists the keys Detected serialises to. Used by a test to
// prove nothing about the credential was added.
func DetectedFieldNames() []string {
	raw, err := json.Marshal(Detected{})
	if err != nil {
		return nil
	}
	object, err := jsonorder.Parse(raw)
	if err != nil {
		return nil
	}
	return object.Keys()
}
