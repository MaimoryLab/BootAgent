package config

import (
	"os"
	"regexp"
	"strings"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/jsonorder"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/provider"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
	"github.com/MaimoryLab/OneAgent/desktop/internal/securefs"
	"github.com/MaimoryLab/OneAgent/desktop/internal/shellquote"
)

// Writer carries what every adapter needs.
type Writer struct {
	Runtime *runtime.Runtime
	FS      *securefs.FS
}

// NewWriter builds a Writer over a runtime.
func NewWriter(rt *runtime.Runtime) *Writer {
	return &Writer{Runtime: rt, FS: securefs.New(rt)}
}

// Settings is one Agent's configuration request. Named fields rather than a map
// so a caller cannot pass a key no adapter reads.
type Settings struct {
	AgentID      string
	ProviderName string
	BaseURL      string
	APIKey       string
	Model        string
	// SmallFastModel is Claude Code's cheaper background model. Empty follows
	// Model. Only the claude-code adapter reads it.
	SmallFastModel string
}

// jsoncComment finds a comment at the start of a value, which is what tells a
// JSONC file apart from broken JSON.
var jsoncComment = regexp.MustCompile(`(?:^|\s)(?://|/\*)`)

// readExisting reads a config that is about to be merged into.
//
// A variable rather than a direct os.ReadFile call so a test can widen the window
// between this read and the write that follows it. Every adapter here is
// read-merge-write, and that gap is what the app layer's write lock closes: two
// operations interleaving in it means the second reads before the first writes, and
// the first result is discarded. The gap is small on a real machine, which is
// exactly why a test needs to be able to make it large -- otherwise the lock's
// test passes whether or not the lock is there.
var readExisting = os.ReadFile

// loadJSON reads an existing config, preserving key order.
//
// A .jsonc file with comments is refused by name rather than reported as invalid
// JSON: OneAgent rewrites these files and cannot preserve comments, so refusing
// is right -- but telling the user their valid JSONC is invalid JSON leaves them
// with no idea what to change.
func loadJSON(path string) (*jsonorder.Object, error) {
	raw, err := readExisting(path)
	if err != nil {
		if os.IsNotExist(err) {
			return jsonorder.NewObject(), nil
		}
		return nil, oerr.Newf("CONFIG_WRITE_FAILED", "Cannot read existing JSON configuration %s: %v", path, err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return jsonorder.NewObject(), nil
	}
	object, parseErr := jsonorder.Parse(raw)
	if parseErr == nil {
		return object, nil
	}
	if strings.HasSuffix(path, ".jsonc") && jsoncComment.Match(raw) {
		return nil, oerr.Newf(
			"CONFIG_WRITE_FAILED",
			"%s contains JSONC comments, which OneAgent cannot preserve when it rewrites the file. "+
				"Remove the comments, or configure this Agent manually and leave the file untouched.",
			path,
		)
	}
	return nil, oerr.Newf("CONFIG_WRITE_FAILED", "Existing JSON configuration is invalid: %s: %v", path, parseErr)
}

// WriteCodex writes the Codex TOML, preserving everything the user had.
func (w *Writer) WriteCodex(agent catalog.Agent, settings Settings) (string, error) {
	path := ConfigPath(w.Runtime, agent)
	if path == "" {
		return "", oerr.New("CONFIG_WRITE_FAILED", "The manifest declares no config path for this Agent")
	}
	managed := CodexManagedBlock(
		settings.ProviderName,
		settings.BaseURL,
		settings.Model,
		AgentEnvVar(settings.AgentID, "API_KEY"),
	)

	existing := ""
	if raw, err := readExisting(path); err == nil {
		existing = string(raw)
	} else if !os.IsNotExist(err) {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Cannot read existing TOML configuration %s: %v", path, err)
	}

	content, err := MergeCodexTOML(existing, managed, path)
	if err != nil {
		return "", err
	}
	// Not secret: the credential is referenced by variable name, not written.
	if _, err := w.FS.Write(path, content, false); err != nil {
		return "", err
	}
	return path, nil
}

// WriteClaude writes Claude Code's settings.json.
//
// The credential goes into this file, which is why it is written on the secret
// path: the backup gets hardened too, and is deleted rather than left readable
// if that fails.
func (w *Writer) WriteClaude(agent catalog.Agent, settings Settings) (string, error) {
	path := ConfigPath(w.Runtime, agent)
	if path == "" {
		return "", oerr.New("CONFIG_WRITE_FAILED", "The manifest declares no config path for this Agent")
	}
	document, err := loadJSON(path)
	if err != nil {
		return "", err
	}
	env, err := document.Child("env")
	if err != nil {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Existing Claude Code env configuration must contain an object: %s", path)
	}

	env.Set("ANTHROPIC_BASE_URL", settings.BaseURL)
	env.Set("ANTHROPIC_AUTH_TOKEN", settings.APIKey)
	env.Set("ANTHROPIC_MODEL", settings.Model)
	// Claude Code drives its fast background work from a second model. A user may
	// point it at something cheaper; left empty it follows the main model.
	smallFast := settings.SmallFastModel
	if smallFast == "" {
		smallFast = settings.Model
	}
	env.Set("ANTHROPIC_SMALL_FAST_MODEL", smallFast)

	content, err := jsonorder.Marshal(document)
	if err != nil {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Cannot encode %s: %v", path, err)
	}
	if _, err := w.FS.Write(path, string(content), true); err != nil {
		return "", err
	}
	return path, nil
}

// WriteOpenAICompatible writes the OpenCode and Kilo format.
//
// The credential is referenced as {env:VAR} rather than embedded, so this file
// carries no secret -- which is why it is not written on the secret path.
func (w *Writer) WriteOpenAICompatible(agent catalog.Agent, settings Settings, schema string) (string, error) {
	path := ConfigPath(w.Runtime, agent)
	if path == "" {
		return "", oerr.New("CONFIG_WRITE_FAILED", "The manifest declares no config path for this Agent")
	}
	document, err := loadJSON(path)
	if err != nil {
		return "", err
	}

	document.Set("$schema", schema)
	providers, err := document.Child("provider")
	if err != nil {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Existing provider configuration must contain an object: %s", path)
	}

	options := jsonorder.NewObject()
	options.Set("baseURL", provider.OpenAIBaseURL(settings.BaseURL))
	options.Set("apiKey", "{env:"+AgentEnvVar(settings.AgentID, "API_KEY")+"}")

	models := jsonorder.NewObject()
	entry := jsonorder.NewObject()
	entry.Set("name", settings.Model)
	models.Set(settings.Model, entry)

	ours := jsonorder.NewObject()
	ours.Set("npm", "@ai-sdk/openai-compatible")
	ours.Set("name", settings.ProviderName)
	ours.Set("options", options)
	ours.Set("models", models)

	providers.Set("oneagent", ours)
	document.Set("model", "oneagent/"+settings.Model)

	content, err := jsonorder.Marshal(document)
	if err != nil {
		return "", oerr.Newf("CONFIG_WRITE_FAILED", "Cannot encode %s: %v", path, err)
	}
	if _, err := w.FS.Write(path, string(content), false); err != nil {
		return "", err
	}
	return path, nil
}

// WriteAider writes Aider's credential script.
//
// Aider has no config file OneAgent can merge into, so this is a shell script
// the user sources. It holds the key in plain text, hence the secret path, and
// every value is quoted for the platform's shell.
func (w *Writer) WriteAider(agent catalog.Agent, settings Settings) (string, error) {
	path := ConfigPath(w.Runtime, agent)
	if path == "" {
		return "", oerr.New("CONFIG_WRITE_FAILED", "The manifest declares no config path for this Agent")
	}
	apiBase := provider.OpenAIBaseURL(settings.BaseURL)

	var content string
	if w.Runtime.OSID == "windows" {
		content = "$env:OPENAI_API_BASE = " + shellquote.PowerShell(apiBase) + "\n" +
			"$env:OPENAI_API_KEY = " + shellquote.PowerShell(settings.APIKey) + "\n"
	} else {
		content = "export OPENAI_API_BASE=" + shellquote.Posix(apiBase) + "\n" +
			"export OPENAI_API_KEY=" + shellquote.Posix(settings.APIKey) + "\n"
	}
	if _, err := w.FS.Write(path, content, true); err != nil {
		return "", err
	}
	return path, nil
}
