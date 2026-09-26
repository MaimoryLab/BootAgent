package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The shape dsh 0.1.7 leaves after importing a legacy settings.yaml into the
// desktop profile: a header comment, one row per imported section, the user's
// own pi-ai route, and the bundle's `!!js` tags on rows the import copied.
const dshProfilePatchFixture = `# Your patch layer for this dsh profile, applied after every bundle layer:
# a top-level YAML array of loader patch entries.
- id: ui-settings-general
  name: "@deepseek-ai/dsh-client-ui-settings-general"
  config:
    welcomeNoticeVersion: 2026-08-13.1
- id: llm-pi-ai
  name: "@deepseek-ai/dsh-llm-pi-ai"
  config:
    providers:
      paigod:
        displayName: paigod
        apiKeyEnv: PAIGOD_API_KEY
        api: openai-completions
        baseURL: https://apiproxy.paigod.work/v1
        models:
          - id: gpt-5.4
- id: session-persistence-jsonl
  config:
    root: !!js dshHomePath('sessions')
- id: agent-default-model
  name: "@deepseek-ai/dsh-agent-default-model"
  config:
    provider: deepseek-official
    model: deepseek-v4-flash
    reasoningEffort: high
`

// The credential store as the desktop application leaves it after an account
// login: refs beside records the Models page never touches. Values are shaped
// like the real ones without being real.
const dshCredentialsWithRecordsFixture = `version: 1
refs:
  PAIGOD_API_KEY: sk-users-own
records:
  client-connection/browser-session:
    kind: grant
    payload:
      version: 1
      secret: browser-session-secret
  deepseek-account-platform/device:
    kind: grant
    payload:
      id: 00000000-0000-0000-0000-000000000000
  deepseek-account-platform/default:
    kind: grant
    payload:
      version: 1
      token: account-token
      issuer: https://platform.deepseek.com
`

func dshProfileHome(t *testing.T, profile, patch, credentials string) (home, patchPath, credentialsPath string) {
	t.Helper()
	home = t.TempDir()
	patchPath = filepath.Join(home, ".dsh", "profiles", profile, DSHProfilePatchName)
	credentialsPath = filepath.Join(home, ".dsh", ".credentials.yaml")
	if err := os.MkdirAll(filepath.Dir(patchPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if patch != "" {
		if err := os.WriteFile(patchPath, []byte(patch), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if credentials != "" {
		if err := os.WriteFile(credentialsPath, []byte(credentials), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home, patchPath, credentialsPath
}

// dshPatchRows reads a profile patch back as id → config, so a test can check
// the row it cares about without restating the file.
func dshPatchRows(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID     string         `yaml:"id"`
		Config map[string]any `yaml:"config"`
	}
	if err := yaml.Unmarshal(data, &rows); err != nil {
		t.Fatalf("profile patch is not a YAML list: %v\n%s", err, data)
	}
	result := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		if _, duplicate := result[row.ID]; duplicate {
			t.Fatalf("row %q appears twice:\n%s", row.ID, data)
		}
		result[row.ID] = row.Config
	}
	return result
}

func TestResolveDSHConfigPathFollowsWhatEachProfileReads(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, ".dsh", DSHLegacySettings)
	desktopPatch := filepath.Join(home, ".dsh", "profiles", DSHDesktopProfile, DSHProfilePatchName)
	webPatch := filepath.Join(home, ".dsh", "profiles", DSHWebProfile, DSHProfilePatchName)

	// Nothing on disk yet. The desktop app is 0.1.7-only and the install flow
	// does not launch it, so this is exactly the state at first configuration:
	// the patch it will read, not a legacy file it would half-import. The web
	// profile may be a 0.1.5 CLI, which reads settings.yaml.
	if got := ResolveDSHConfigPath(home, DSHDesktopProfile); got != desktopPatch {
		t.Fatalf("fresh desktop profile resolves to %q, want %q", got, desktopPatch)
	}
	if got := ResolveDSHConfigPath(home, DSHWebProfile); got != legacy {
		t.Fatalf("fresh web profile resolves to %q, want %q", got, legacy)
	}
	// Once `dsh web` from 0.1.7 has created its profile, the web profile
	// switches too.
	if err := os.MkdirAll(filepath.Dir(webPatch), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := ResolveDSHConfigPath(home, DSHWebProfile); got != webPatch {
		t.Fatalf("web profile resolves to %q, want %q", got, webPatch)
	}
}

// The desktop profile directory does not exist before the app's first launch.
// The write has to create it, and leave only the patch behind: the app's own
// initProfile fills in package.json and pnpm-workspace.yaml where absent, and
// must not find files BootAgent guessed at.
func TestWriteDSHProfileCreatesTheDesktopProfileAheadOfTheApp(t *testing.T) {
	home := t.TempDir()
	path := ResolveDSHConfigPath(home, DSHDesktopProfile)
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("profile directory exists before the write: %v", err)
	}
	if err := testWriter(t, home, "linux").WriteDSH(context.Background(), path, "PPIO", "https://api.example", "sk", "m"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if len(names) != 1 || names[0] != DSHProfilePatchName {
		t.Fatalf("profile directory contains %v, want only %s", names, DSHProfilePatchName)
	}
	// And the legacy file was not touched: nothing reads it for this profile.
	if _, err := os.Stat(filepath.Join(home, ".dsh", DSHLegacySettings)); !os.IsNotExist(err) {
		t.Fatalf("legacy settings.yaml was written: %v", err)
	}
	rows := dshPatchRows(t, path)
	if rows["agent-default-model"]["provider"] != "bootagent" {
		t.Fatalf("default selection = %v", rows["agent-default-model"])
	}
}

func TestDSHCredentialsPathIsTheHarnessHomeFromEitherLayout(t *testing.T) {
	want := filepath.Join("home", ".dsh", ".credentials.yaml")
	for _, path := range []string{
		filepath.Join("home", ".dsh", DSHLegacySettings),
		filepath.Join("home", ".dsh", "profiles", "desktop", DSHProfilePatchName),
	} {
		if got := dshCredentialsPath(path); got != want {
			t.Errorf("dshCredentialsPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestWriteDSHProfileRegistersARouteBesideTheUsersOwn(t *testing.T) {
	home, path, credentials := dshProfileHome(t, DSHDesktopProfile, dshProfilePatchFixture, dshCredentialsWithRecordsFixture)
	if err := testWriter(t, home, "linux").WriteDSH(context.Background(), path, "PPIO", "https://api.example/openai", "sk-new", "deepseek-v4-pro"); err != nil {
		t.Fatal(err)
	}

	rows := dshPatchRows(t, path)
	providers, _ := rows["llm-pi-ai"]["providers"].(map[string]any)
	if _, ok := providers["paigod"]; !ok {
		t.Errorf("the user's own route was lost: %v", providers)
	}
	route, _ := providers["bootagent"].(map[string]any)
	if route == nil {
		t.Fatalf("no bootagent route was written: %v", providers)
	}
	for key, want := range map[string]any{
		"displayName": "PPIO",
		"apiKeyEnv":   "BOOTAGENT_API_KEY",
		"api":         "openai-completions",
		"baseURL":     "https://api.example/openai/v1",
	} {
		if route[key] != want {
			t.Errorf("route %s = %v, want %v", key, route[key], want)
		}
	}
	models, _ := route["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("route models = %v, want exactly the resolved model", route["models"])
	}
	if entry, _ := models[0].(map[string]any); entry["id"] != "deepseek-v4-pro" {
		t.Errorf("seeded model = %v, want deepseek-v4-pro", models[0])
	}

	// The default selection moves to the route, and the effort the replaced
	// DeepSeek model carried does not ride along.
	selection := rows["agent-default-model"]
	if selection["provider"] != "bootagent" || selection["model"] != "deepseek-v4-pro" {
		t.Errorf("default selection = %v, want the bootagent route", selection)
	}
	if _, stale := selection["reasoningEffort"]; stale {
		t.Errorf("stale reasoningEffort survived: %v", selection)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Rows BootAgent does not own keep their place, and the bundle's tagged
	// scalar survives untouched rather than being evaluated or quoted away.
	for _, want := range []string{"ui-settings-general", "welcomeNoticeVersion", "!!js dshHomePath('sessions')"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("unrelated row content %q was lost:\n%s", want, data)
		}
	}
	// The header comment is the user's orientation in a file dsh tells them to
	// edit by hand.
	if !strings.HasPrefix(string(data), "# Your patch layer") {
		t.Errorf("header comment was lost:\n%s", data)
	}

	// The credential lands in the shared store two directories up, beside the
	// user's own reference -- and beside the account grants the desktop
	// application keeps in the same document, which must come through intact.
	stored := dshCredentials(t, credentials)
	if stored["BOOTAGENT_API_KEY"] != "sk-new" || stored["PAIGOD_API_KEY"] != "sk-users-own" {
		t.Errorf("credentials = %v", stored)
	}
	var document struct {
		Records map[string]struct {
			Kind    string         `yaml:"kind"`
			Payload map[string]any `yaml:"payload"`
		} `yaml:"records"`
	}
	raw, err := os.ReadFile(credentials)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Records) != 3 {
		t.Fatalf("account records = %d, want all 3 preserved:\n%s", len(document.Records), raw)
	}
	if grant := document.Records["deepseek-account-platform/default"]; grant.Kind != "grant" || grant.Payload["token"] != "account-token" || grant.Payload["issuer"] != "https://platform.deepseek.com" {
		t.Errorf("account grant was disturbed: %+v", grant)
	}

	detected := ReadDSHConfig(string(data))
	if detected.BaseURL != "https://api.example/openai/v1" || detected.Model != "deepseek-v4-pro" || !detected.ManagedByBootAgent {
		t.Fatalf("profile round-trip = %#v", detected)
	}
	for _, target := range []string{path, credentials} {
		info, err := os.Stat(target)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, err=%v", target, info.Mode().Perm(), err)
		}
	}
}

// A fresh profile patch is a header comment and nothing else; yaml.v3 reads that
// as an empty document, which must start a sequence rather than fail.
func TestWriteDSHProfileStartsFromACommentOnlyPatch(t *testing.T) {
	home, path, _ := dshProfileHome(t, DSHDesktopProfile, "# Your patch layer for this dsh profile.\n# Edit freely.\n", "")
	if err := testWriter(t, home, "linux").WriteDSH(context.Background(), path, "PPIO", "https://api.example", "sk", "m"); err != nil {
		t.Fatal(err)
	}
	rows := dshPatchRows(t, path)
	if len(rows) != 2 {
		t.Fatalf("rows = %v, want llm-pi-ai and agent-default-model only", rows)
	}
	data, _ := os.ReadFile(path)
	// A row BootAgent creates names its plugin, as dsh's own Models page does.
	if !strings.Contains(string(data), dshPiAIEntryName) || !strings.Contains(string(data), dshDefaultModelEntryName) {
		t.Errorf("new rows do not name their plugins:\n%s", data)
	}
	// The scaffold's header comment is the user's orientation in the file and
	// has to survive the first write, which is the one that finds no rows.
	if !strings.HasPrefix(string(data), "# Your patch layer for this dsh profile.\n# Edit freely.\n- id: ") {
		t.Errorf("header comment was lost or displaced:\n%s", data)
	}
}

// What DeepSeek Harness 0.1.7-rc.2 leaves on Windows after its first launch and
// before the user saves any key: an empty flow sequence under the scaffold
// comment, and a credential document with records but no refs yet.
func TestWriteDSHProfileIntoAFreshDesktopInstall(t *testing.T) {
	const patch = "# Your patch layer for this dsh profile, applied after every bundle layer:\n" +
		"# a top-level YAML array of loader patch entries (id-targeted config\n" +
		"# overrides, disables, and insert lists; `!!js` expressions allowed).\n" +
		"[]\n"
	const credentials = "version: 1\n" +
		"records:\n" +
		"  client-connection/browser-session:\n" +
		"    kind: grant\n" +
		"    payload:\n" +
		"      version: 1\n" +
		"      secret: browser-session-secret\n"
	home, path, credentialsPath := dshProfileHome(t, DSHDesktopProfile, patch, credentials)
	if err := testWriter(t, home, "linux").WriteDSH(context.Background(), path, "PPIO", "https://api.example", "sk-new", "m"); err != nil {
		t.Fatal(err)
	}
	if rows := dshPatchRows(t, path); rows["llm-pi-ai"] == nil || rows["agent-default-model"] == nil {
		t.Fatalf("rows = %v, want llm-pi-ai and agent-default-model", rows)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(written), strings.TrimSuffix(patch, "[]\n")+"- id: ") {
		t.Errorf("patch was not written as a block list under the header:\n%s", written)
	}
	data, err := os.ReadFile(credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Version int                       `yaml:"version"`
		Refs    map[string]string         `yaml:"refs"`
		Records map[string]map[string]any `yaml:"records"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Version != 1 || document.Refs["BOOTAGENT_API_KEY"] != "sk-new" || document.Records["client-connection/browser-session"] == nil {
		t.Fatalf("credentials after write:\n%s", data)
	}
}

// The rest of the file is rewritten with the indentation dsh itself uses, so a
// write changes only the rows it touched. A row the write never addresses
// comes out byte-for-byte as it went in.
func TestWriteDSHProfileKeepsTheFilesIndentation(t *testing.T) {
	untouched := "- id: session-persistence-jsonl\n  config:\n    root: !!js dshHomePath('sessions')\n- id: ui-settings-general\n  name: \"@deepseek-ai/dsh-client-ui-settings-general\"\n  config:\n    welcomeNoticeVersion: 2026-08-13.1\n"
	home, path, _ := dshProfileHome(t, DSHDesktopProfile, untouched, "")
	if err := testWriter(t, home, "linux").WriteDSH(context.Background(), path, "PPIO", "https://api.example", "sk", "m"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(data), untouched) {
		t.Fatalf("rows the write did not address were re-indented or reordered:\nwant prefix:\n%s\ngot:\n%s", untouched, data)
	}
}

// A hand edit that repeats a row id must read as the writer would treat it:
// the first row wins. Decoding both into one struct would merge them.
func TestReadDSHConfigTakesTheFirstRowPerID(t *testing.T) {
	text := "- id: llm-pi-ai\n  config:\n    providers:\n      bootagent:\n        baseURL: https://first.example/v1\n        models:\n          - id: first\n" +
		"- id: llm-pi-ai\n  config:\n    providers:\n      other:\n        baseURL: https://second.example/v1\n"
	got := ReadDSHConfig(text)
	if got.BaseURL != "https://first.example/v1" || got.Model != "first" || !got.ManagedByBootAgent {
		t.Fatalf("duplicate rows read as %#v, want the first row alone", got)
	}
}

func TestWriteDSHProfileIsIdempotent(t *testing.T) {
	home, path, credentials := dshProfileHome(t, DSHDesktopProfile, "", "")
	writer := testWriter(t, home, "linux")
	for range 3 {
		if err := writer.WriteDSH(context.Background(), path, "PPIO", "https://api.example", "sk", "m"); err != nil {
			t.Fatal(err)
		}
	}
	rows := dshPatchRows(t, path)
	providers, _ := rows["llm-pi-ai"]["providers"].(map[string]any)
	if len(providers) != 1 {
		t.Errorf("providers = %v, want exactly one after repeated writes", providers)
	}
	route, _ := providers["bootagent"].(map[string]any)
	if models, _ := route["models"].([]any); len(models) != 1 {
		t.Errorf("models accumulated: %v", route["models"])
	}
	data, _ := os.ReadFile(credentials)
	if strings.Count(string(data), "BOOTAGENT_API_KEY") != 1 {
		t.Errorf("credential entries accumulated:\n%s", data)
	}
}

func TestWriteDSHProfileOfficialUsesTheShippedRouteAndCleansUp(t *testing.T) {
	home, path, credentials := dshProfileHome(t, DSHDesktopProfile, dshProfilePatchFixture, dshCredentialsWithRecordsFixture)
	writer := testWriter(t, home, "linux")
	// An earlier activation against a gateway left a bootagent route behind.
	if err := writer.WriteDSH(context.Background(), path, "PPIO", "https://api.example", "sk-gateway", "m"); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteDSHOfficial(context.Background(), path, "sk-deepseek", "deepseek-v4-pro", "max"); err != nil {
		t.Fatal(err)
	}

	rows := dshPatchRows(t, path)
	providers, _ := rows["llm-pi-ai"]["providers"].(map[string]any)
	if _, stale := providers["bootagent"]; stale {
		t.Errorf("stale bootagent route survived the official activation: %v", providers)
	}
	if _, ok := providers["paigod"]; !ok {
		t.Errorf("the user's own route was lost: %v", providers)
	}
	selection := rows["agent-default-model"]
	if selection["provider"] != "deepseek-official" || selection["model"] != "deepseek-v4-pro" || selection["reasoningEffort"] != "max" {
		t.Errorf("default selection = %v", selection)
	}

	stored := dshCredentials(t, credentials)
	if stored["DEEPSEEK_API_KEY"] != "sk-deepseek" {
		t.Errorf("official credential = %q", stored["DEEPSEEK_API_KEY"])
	}
	// The unreferenced bootagent key is left alone: this write touches only the
	// entry it has to.
	if stored["BOOTAGENT_API_KEY"] != "sk-gateway" {
		t.Errorf("bootagent credential = %q, want untouched", stored["BOOTAGENT_API_KEY"])
	}

	data, _ := os.ReadFile(path)
	detected := ReadDSHConfig(string(data))
	if detected.Model != "deepseek-v4-pro" || detected.BaseURL != "" || detected.ManagedByBootAgent {
		t.Fatalf("official round-trip = %#v", detected)
	}
}

// An official activation on a patch with no llm-pi-ai row has nothing to clean
// up and must not create an empty row as a side effect.
func TestWriteDSHProfileOfficialLeavesNoEmptyPiAIRow(t *testing.T) {
	home, path, _ := dshProfileHome(t, DSHDesktopProfile, "", "")
	if err := testWriter(t, home, "linux").WriteDSHOfficial(context.Background(), path, "sk", "deepseek-v4-flash", ""); err != nil {
		t.Fatal(err)
	}
	rows := dshPatchRows(t, path)
	if _, created := rows["llm-pi-ai"]; created {
		t.Errorf("an empty llm-pi-ai row was created: %v", rows)
	}
	if _, stale := rows["agent-default-model"]["reasoningEffort"]; stale {
		t.Errorf("an empty reasoningEffort was written: %v", rows["agent-default-model"])
	}
}

func TestWriteDSHProfileOfficialRejectsAnEffortBeforeTouchingCredentials(t *testing.T) {
	home, path, credentials := dshProfileHome(t, DSHDesktopProfile, "", dshCredentialsWithRecordsFixture)
	err := testWriter(t, home, "linux").WriteDSHOfficial(context.Background(), path, "sk-x", "deepseek-v4-pro", "medium")
	if err == nil || !strings.Contains(err.Error(), "must be one of off, high, max") {
		t.Fatalf("unsupported effort = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("a rejected effort still wrote the patch: err=%v", err)
	}
	after, _ := os.ReadFile(credentials)
	if string(after) != dshCredentialsWithRecordsFixture {
		t.Errorf("a rejected effort still rewrote the credential store:\n%s", after)
	}
}

func TestWriteDSHProfileRefusesANonListDocument(t *testing.T) {
	home, path, _ := dshProfileHome(t, DSHDesktopProfile, "llm-pi-ai:\n  providers: {}\n", "")
	err := testWriter(t, home, "linux").WriteDSH(context.Background(), path, "PPIO", "https://api.example", "sk", "m")
	if err == nil || !strings.Contains(err.Error(), "must contain a list") {
		t.Fatalf("mapping-shaped patch was accepted: %v", err)
	}
}

// Both layouts read back through one function; the container is told apart by
// the document's root kind, not the path.
func TestReadDSHConfigReadsBothLayouts(t *testing.T) {
	legacy := "llm-pi-ai:\n  providers:\n    bootagent:\n      baseURL: https://legacy.example/v1\n      models:\n        - id: legacy-model\n"
	if got := ReadDSHConfig(legacy); got.BaseURL != "https://legacy.example/v1" || got.Model != "legacy-model" || !got.ManagedByBootAgent {
		t.Errorf("legacy layout = %#v", got)
	}
	patch := "- id: llm-pi-ai\n  config:\n    providers:\n      bootagent:\n        baseURL: https://patch.example/v1\n        models:\n          - id: patch-model\n"
	if got := ReadDSHConfig(patch); got.BaseURL != "https://patch.example/v1" || got.Model != "patch-model" || !got.ManagedByBootAgent {
		t.Errorf("profile layout = %#v", got)
	}
	// A patch that only selects the shipped route reports the model without
	// claiming management, as the legacy reader does.
	official := "- id: agent-default-model\n  config:\n    provider: deepseek-official\n    model: deepseek-v4-pro\n"
	if got := ReadDSHConfig(official); got.Model != "deepseek-v4-pro" || got.ManagedByBootAgent || got.BaseURL != "" {
		t.Errorf("official-only layout = %#v", got)
	}
}
