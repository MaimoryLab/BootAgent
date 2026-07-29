package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

func TestAnAgentIdThatCouldEscapeItsDirectoryIsRefused(t *testing.T) {
	// The id names a file holding a plaintext credential. A traversal would put
	// that key outside the private directory -- somewhere readable, and somewhere
	// the user would never think to look for it.
	for _, id := range []string{
		"../escape",
		"..",
		".",
		"a/b",
		`a\b`,
		"/absolute",
		"with space",
		"UPPER",
		"-leading-dash",
		"_leading-underscore",
		"",
		strings.Repeat("a", 65),
		"trailing/",
		"null\x00byte",
		"dot.dot",
	} {
		t.Run(id, func(t *testing.T) {
			if _, err := ValidateAgentID(id); err == nil {
				t.Errorf("%q should be refused", id)
			} else {
				assertCode(t, err, "INVALID_REQUEST")
			}
		})
	}
}

func TestTheRealAgentIdsAreAccepted(t *testing.T) {
	// The rule has to admit every id the manifest actually declares, or an Agent
	// becomes unconfigurable.
	for _, id := range catalog.MustLoad().IDs() {
		if _, err := ValidateAgentID(id); err != nil {
			t.Errorf("%s is in the manifest but rejected: %v", id, err)
		}
	}
	// And the shapes the pattern is meant to allow.
	for _, id := range []string{"a", "a1", "a-b", "a_b", strings.Repeat("a", 64)} {
		if _, err := ValidateAgentID(id); err != nil {
			t.Errorf("%q should be accepted: %v", id, err)
		}
	}
}

func TestAnEscapingIdCannotProduceAPathOutsideTheHome(t *testing.T) {
	// The validation is enforced where the path is built, so every caller
	// inherits it rather than each having to remember.
	rt := runtime.New(runtime.WithHome("/home/user"), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{}))
	if _, err := AgentEnvPath(rt, "../../etc/passwd"); err == nil {
		t.Fatal("a traversing id must not yield a path")
	}
	path, err := AgentEnvPath(rt, "codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(path, filepath.Join("/home/user", ".oneagent", "agents")+string(filepath.Separator)) {
		t.Errorf("path = %q, want it inside the private directory", path)
	}
}

func TestEachAgentGetsItsOwnCredentialVariable(t *testing.T) {
	// A single shared ONEAGENT_API_KEY made it impossible for three
	// OpenAI-compatible Agents to point at different providers in one shell:
	// whichever env file was sourced last won.
	seen := map[string]string{}
	for _, id := range catalog.MustLoad().AutoAgents() {
		name := AgentEnvVar(id, "API_KEY")
		if previous, clash := seen[name]; clash {
			t.Errorf("%s and %s share the variable %s", previous, id, name)
		}
		seen[name] = id
	}
	if len(seen) < 2 {
		t.Fatal("expected several auto Agents; the assertion would be vacuous")
	}
}

func TestTheVariableNameIsAValidShellIdentifier(t *testing.T) {
	// It is assigned in a sourced script, so a character the shell cannot accept
	// in a name would break the file rather than merely look odd.
	for _, id := range []string{"kilo-cli", "claude-code", "a.b.c", "a--b", "9lead"} {
		name := AgentEnvVar(id, "API_KEY")
		for index, char := range name {
			valid := char == '_' ||
				(char >= 'A' && char <= 'Z') ||
				(char >= '0' && char <= '9' && index > 0)
			if !valid {
				t.Errorf("%s yields %q, which is not a shell identifier", id, name)
				break
			}
		}
	}
}

func TestTheVariableNameSuffixDefaultsToTheKey(t *testing.T) {
	if got := AgentEnvVar("codex", ""); got != "ONEAGENT_API_KEY_CODEX" {
		t.Errorf("got %q, want the API_KEY default", got)
	}
	if got := AgentEnvVar("codex", "BASE_URL"); got != "ONEAGENT_BASE_URL_CODEX" {
		t.Errorf("got %q", got)
	}
}

func TestWhetherAnEnvFileIsNeededComesFromTheManifest(t *testing.T) {
	// Read from credential_delivery rather than a set of ids: that set had left
	// Claude Code out while it was the one Agent that could not authenticate
	// without one, and nothing in the code said what the set was for.
	for delivery, want := range map[string]bool{
		DeliveryOneAgentEnv: true,
		DeliveryNativeEnv:   true,
		DeliveryConfigFile:  false,
		"":                  false,
		"something-new":     false,
	} {
		agent := catalog.Agent{CredentialDelivery: delivery}
		if got := NeedsEnvFile(agent); got != want {
			t.Errorf("delivery=%q: got %v, want %v", delivery, got, want)
		}
	}
}

func TestClaudeCodeStillNeedsAnEnvFile(t *testing.T) {
	// Stated explicitly because this is the regression: it reads native
	// variables, so without the file it reports configured and then starts up
	// unauthenticated.
	agent, present := catalog.MustLoad().Agent("claude-code")
	if !present {
		t.Skip("claude-code is not in the manifest")
	}
	if !NeedsEnvFile(agent) {
		t.Fatalf("claude-code declares %q, which yields no env file", agent.CredentialDelivery)
	}
}

func TestTheCredentialFileUsesThePlatformsShellExtension(t *testing.T) {
	// It is sourced, so a .ps1 on POSIX or an extensionless file on Windows
	// would be unusable rather than merely misnamed.
	for _, testCase := range []struct{ osID, want string }{
		{"linux", "env"},
		{"macos", "env"},
		{"windows", "env.ps1"},
	} {
		rt := runtime.New(runtime.WithHome("/home/user"), runtime.WithOSID(testCase.osID), runtime.WithEnv(map[string]string{}))
		if got := filepath.Base(EnvPath(rt)); got != testCase.want {
			t.Errorf("os=%s shared env file = %q, want %q", testCase.osID, got, testCase.want)
		}
		path, err := AgentEnvPath(rt, "codex")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := filepath.Base(path); got != "codex."+testCase.want {
			t.Errorf("os=%s per-agent file = %q, want codex.%s", testCase.osID, got, testCase.want)
		}
	}
}

func TestTheWindowsConfigPathOnlyAppliesOnWindows(t *testing.T) {
	agent := catalog.Agent{ConfigPath: "shared/path.json", WindowsConfigPath: "windows/path.json"}
	posix := runtime.New(runtime.WithHome("/home/user"), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{}))
	windows := runtime.New(runtime.WithHome(`C:\Users\u`), runtime.WithOSID("windows"), runtime.WithEnv(map[string]string{}))

	if got := ConfigPath(posix, agent); !strings.Contains(got, "shared") {
		t.Errorf("posix path = %q, want the shared location", got)
	}
	if got := ConfigPath(windows, agent); !strings.Contains(got, "windows") {
		t.Errorf("windows path = %q, want the Windows location", got)
	}
}

func TestWindowsFallsBackToTheSharedPathWhenNoOverrideIsDeclared(t *testing.T) {
	// Most Agents use the same location on every platform, so a missing override
	// is normal rather than an error.
	agent := catalog.Agent{ConfigPath: "shared/path.json"}
	windows := runtime.New(runtime.WithHome(`C:\Users\u`), runtime.WithOSID("windows"), runtime.WithEnv(map[string]string{}))
	if got := ConfigPath(windows, agent); !strings.Contains(got, "shared") {
		t.Errorf("path = %q, want the shared location", got)
	}
}

func TestAnAgentWithNoDeclaredPathYieldsNothing(t *testing.T) {
	// Guide-only Agents have no config file, and inventing one would create a
	// file no Agent reads.
	rt := runtime.New(runtime.WithHome("/home/user"), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{}))
	if got := ConfigPath(rt, catalog.Agent{}); got != "" {
		t.Errorf("path = %q, want empty", got)
	}
}

func TestNewWriterUsesTheRuntimeItIsGiven(t *testing.T) {
	rt := runtime.New(runtime.WithHome("/home/user"), runtime.WithOSID("linux"), runtime.WithEnv(map[string]string{}))
	writer := NewWriter(rt)
	if writer.Runtime != rt || writer.FS == nil {
		t.Fatalf("writer = %+v", writer)
	}
}
