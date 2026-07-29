package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
	"github.com/MaimoryLab/OneAgent/desktop/internal/securefs"
)

// This is the test the migration's stop-loss checkpoint depends on: for each
// adapter, both implementations are given the same starting file and the same
// settings, and the files they produce are compared byte for byte.
//
// Porting the Python tests proves the cases someone wrote down. This proves the
// rest -- and it has already found things no ported test would have, because a
// config file is mostly the user's content and the ways of reformatting it are
// invisible until the bytes are compared.

func pythonBin(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3.12", "python3"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if os.Getenv("ONEAGENT_REQUIRE_PARITY") != "" {
		t.Fatal("no Python on PATH, but ONEAGENT_REQUIRE_PARITY demands the comparison run")
	}
	t.Skip("no Python available to compare against")
	return ""
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine the working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "agents.lock.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("walked to the filesystem root without finding agents.lock.json")
		}
		dir = parent
	}
}

// settings are the values both sides are configured with. Deliberately include
// a non-ASCII provider name and model, because that is where the two JSON
// encodings and the TOML escaping diverge.
type paritySettings struct {
	ProviderName   string `json:"provider_name"`
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	Model          string `json:"model"`
	SmallFastModel string `json:"small_fast_model"`
}

// pythonWrite runs the real Python adapter into its own temporary HOME and
// returns the bytes it wrote. Separate directories per side: the migration plan
// forbids letting the two implementations write into one HOME, because then a
// difference could come from one observing the other's leftovers.
func pythonWrite(t *testing.T, agentID, existingRelative, existingContent string, s paritySettings) []byte {
	t.Helper()
	root := repoRoot(t)
	home := t.TempDir()

	if existingContent != "" {
		full := filepath.Join(home, existingRelative)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatalf("cannot prepare: %v", err)
		}
		if err := os.WriteFile(full, []byte(existingContent), 0o600); err != nil {
			t.Fatalf("cannot prepare: %v", err)
		}
	}

	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("cannot encode settings: %v", err)
	}
	script := `
import json, sys
from pathlib import Path
sys.path.insert(0, sys.argv[1])
from oneagent.catalog import agent_catalog
from oneagent.installer import Runtime, _write_agent_config

home = Path(sys.argv[2])
agent_id = sys.argv[3]
s = json.loads(sys.argv[4])
meta = agent_catalog()[agent_id]
runtime = Runtime.create(home=home, os_id="linux", env={"HOME": str(home)})
path = _write_agent_config(
    runtime, agent_id, meta,
    s["provider_name"], s["base_url"], s["api_key"], s["model"], s["small_fast_model"],
)
print(json.dumps(str(path.relative_to(home))))
`
	cmd := exec.Command(pythonBin(t), "-c", script, root, home, agentID, string(encoded))
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python _write_agent_config failed for %s: %v\n%s", agentID, err, output)
	}
	var relative string
	// CombinedOutput may carry warnings; the JSON is on the last line.
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &relative); err != nil {
		t.Fatalf("cannot read python output %q: %v", output, err)
	}
	raw, err := os.ReadFile(filepath.Join(home, relative))
	if err != nil {
		t.Fatalf("python wrote no file at %s: %v", relative, err)
	}
	return raw
}

func goWrite(t *testing.T, agentID, existingRelative, existingContent string, s paritySettings) []byte {
	t.Helper()
	home := t.TempDir()
	if existingContent != "" {
		full := filepath.Join(home, existingRelative)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatalf("cannot prepare: %v", err)
		}
		if err := os.WriteFile(full, []byte(existingContent), 0o600); err != nil {
			t.Fatalf("cannot prepare: %v", err)
		}
	}

	rt := runtime.New(
		runtime.WithHome(home),
		runtime.WithOSID("linux"),
		runtime.WithEnv(map[string]string{"HOME": home}),
	)
	writer := &Writer{Runtime: rt, FS: securefs.New(rt)}
	agent, present := catalog.MustLoad().Agent(agentID)
	if !present {
		t.Fatalf("%s is not in the manifest", agentID)
	}
	path, err := writer.Write(agent, Settings{
		AgentID:        agentID,
		ProviderName:   s.ProviderName,
		BaseURL:        s.BaseURL,
		APIKey:         s.APIKey,
		Model:          s.Model,
		SmallFastModel: s.SmallFastModel,
	})
	if err != nil {
		t.Fatalf("go write failed for %s: %v", agentID, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("go wrote no file: %v", err)
	}
	return raw
}

var baseSettings = paritySettings{
	ProviderName: "PPIO",
	BaseURL:      "https://api.ppio.com/openai",
	APIKey:       "sk-parity-fixture",
	Model:        "deepseek/deepseek-v3",
}

// agentPaths maps each auto Agent to its config location, read from the manifest
// so this table cannot drift from what the adapters actually write.
func agentPaths(t *testing.T) map[string]string {
	t.Helper()
	paths := map[string]string{}
	manifest := catalog.MustLoad()
	for _, id := range manifest.AutoAgents() {
		agent, _ := manifest.Agent(id)
		paths[id] = agent.ConfigPath
	}
	return paths
}

func TestParityEveryAdapterWritesIdenticalBytesToANewFile(t *testing.T) {
	// The baseline: no existing file, so this compares the managed content alone.
	for id := range agentPaths(t) {
		t.Run(id, func(t *testing.T) {
			want := pythonWrite(t, id, "", "", baseSettings)
			got := goWrite(t, id, "", "", baseSettings)
			if string(got) != string(want) {
				t.Fatalf("bytes differ:\n  Go (%d):\n%s\n  Python (%d):\n%s", len(got), got, len(want), want)
			}
		})
	}
}

// existingFiles are realistic starting files: a user's own settings, in their own
// order, with keys OneAgent does not manage. Preserving these is a product
// promise, and reordering them is the failure the byte comparison exists to find.
var existingFiles = map[string]map[string]string{
	"codex": {
		"a hand-written provider": "model_provider = \"vendor\"\nmodel = \"gpt-5-mini\"\n\n" +
			"[model_providers.vendor]\nname = \"Vendor\"\nbase_url = \"https://vendor.example/v1\"\n",
		"comments and spacing": "# my notes\nmodel = \"old\"\n\n# a table\n[tui]\ntheme = \"dark\"  # inline\n",
		"our table already present": "model_provider = \"oneagent\"\nmodel = \"old\"\n\n" +
			"[model_providers.oneagent]\nname = \"Old\"\nbase_url = \"https://old.example\"\n" +
			"env_key = \"X\"\nwire_api = \"responses\"\n\n[other]\nkeep = true\n",
		"unrelated tables only": "[tui]\ntheme = \"dark\"\n\n[history]\npersistence = \"none\"\n",
	},
	"claude-code": {
		"user keys in their own order": `{"theme":"dark","env":{"MY_VAR":"1","ANTHROPIC_MODEL":"old"},"other":true}`,
		"empty object":                 `{}`,
		"no env key":                   `{"theme":"dark"}`,
		"nested user data":             `{"permissions":{"allow":["Bash(ls)"],"deny":[]},"env":{"KEEP":"yes"}}`,
		"numbers the user set":         `{"maxTokens":4096,"temperature":0.7,"env":{}}`,
	},
	"opencode": {
		"existing provider kept": `{"$schema":"https://opencode.ai/config.json","provider":{"mine":{"npm":"x","options":{"baseURL":"https://mine.example"}}},"theme":"dark"}`,
		"empty object":           `{}`,
		"our provider present":   `{"provider":{"oneagent":{"npm":"old","name":"Old"}},"model":"oneagent/old"}`,
	},
	"kilo-cli": {
		"existing provider kept": `{"provider":{"mine":{"npm":"x"}},"other":1}`,
		"empty object":           `{}`,
	},
	"aider": {
		// Aider's file is ours outright, so there is nothing to merge; an
		// existing one is simply replaced.
		"replaced wholesale": "export OPENAI_API_BASE='https://old.example/v1'\nexport OPENAI_API_KEY='sk-old'\n",
	},
}

func TestParityEveryAdapterMergesIntoAnExistingFileIdentically(t *testing.T) {
	paths := agentPaths(t)
	for id, cases := range existingFiles {
		relative := paths[id]
		if relative == "" {
			t.Fatalf("%s has no config path in the manifest", id)
		}
		for name, existing := range cases {
			t.Run(id+"/"+name, func(t *testing.T) {
				want := pythonWrite(t, id, relative, existing, baseSettings)
				got := goWrite(t, id, relative, existing, baseSettings)
				if string(got) != string(want) {
					t.Fatalf("bytes differ:\n  Go:\n%s\n  Python:\n%s", got, want)
				}
			})
		}
	}
}

// awkwardSettings carry the values where an encoding difference hides: non-ASCII
// in a field that reaches both a TOML string and a JSON string, and a key with
// characters a shell would interpret.
var awkwardSettings = []struct {
	name     string
	settings paritySettings
}{
	{"non-ascii model and provider", paritySettings{
		ProviderName: "通义千问",
		BaseURL:      "https://api.ppio.com/openai",
		APIKey:       "sk-parity",
		Model:        "通义-max",
	}},
	{"url with query and ampersand", paritySettings{
		ProviderName: "PPIO",
		BaseURL:      "https://api.ppio.com/openai?region=cn&tier=pro",
		APIKey:       "sk-parity",
		Model:        "m",
	}},
	{"key with shell metacharacters", paritySettings{
		ProviderName: "PPIO",
		BaseURL:      "https://api.ppio.com/openai",
		APIKey:       "sk-'quote$dollar`tick",
		Model:        "m",
	}},
	{"model with a slash and dots", paritySettings{
		ProviderName: "PPIO",
		BaseURL:      "https://api.ppio.com/openai",
		APIKey:       "sk-parity",
		Model:        "org.name/model-v1.5",
	}},
	{"quotes in the provider name", paritySettings{
		ProviderName: `He said "hi"`,
		BaseURL:      "https://api.ppio.com/openai",
		APIKey:       "sk-parity",
		Model:        "m",
	}},
	{"separate small fast model", paritySettings{
		ProviderName:   "PPIO",
		BaseURL:        "https://api.ppio.com/openai",
		APIKey:         "sk-parity",
		Model:          "big-model",
		SmallFastModel: "small-model",
	}},
	{"emoji in the model", paritySettings{
		ProviderName: "PPIO",
		BaseURL:      "https://api.ppio.com/openai",
		APIKey:       "sk-parity",
		Model:        "model-✅",
	}},
}

func TestParityAwkwardValuesEncodeIdenticallyInEveryAdapter(t *testing.T) {
	// This is where the two JSON encodings and the TOML escaping part company:
	// the codex adapter quotes with ensure_ascii on, the JSON adapters with it
	// off, and the aider script uses shell quoting. One helper for all three
	// would pass every hand-written test and fail here.
	for id := range agentPaths(t) {
		for _, testCase := range awkwardSettings {
			t.Run(id+"/"+testCase.name, func(t *testing.T) {
				want := pythonWrite(t, id, "", "", testCase.settings)
				got := goWrite(t, id, "", "", testCase.settings)
				if string(got) != string(want) {
					t.Fatalf("bytes differ:\n  Go:\n%s\n  Python:\n%s", got, want)
				}
			})
		}
	}
}
