package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/jsonorder"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
	"github.com/MaimoryLab/OneAgent/desktop/internal/testutil"
)

func newStore(t *testing.T, osID string) (*Store, string) {
	t.Helper()
	home := t.TempDir()
	runner := testutil.NewRecordingRunner(t)
	options := []runtime.Option{
		runtime.WithHome(home),
		runtime.WithOSID(osID),
		runtime.WithEnv(map[string]string{"HOME": home}),
	}
	if osID == "windows" {
		// The Windows write hardens the file with icacls and refuses to publish a
		// secret it could not harden, so the tool has to be present and answer for
		// that path to be exercised at all.
		runner.Respond(testutil.Succeed("", "icacls"))
		options = append(options, runtime.WithLookup(func(name string) (string, bool) {
			return `C:\Windows\System32\` + name + ".exe", name == "icacls"
		}))
	}
	options = append(options, runtime.WithRunner(runner.Runner()))
	return NewStore(runtime.New(options...)), home
}

func TestAStoredProfileNeverHoldsTheKey(t *testing.T) {
	// The whole reason the secret lives in a separate file. A key in the profile
	// would be reported by List, which the status payload serialises -- so this is
	// checked on the bytes rather than on the returned object.
	const key = "sk-must-not-appear-anywhere"
	store, home := newStore(t, "linux")
	if _, err := store.Save(SaveRequest{
		ID: "team", Provider: "ppio", Model: "m", AgentIDs: []string{"codex"}, APIKey: key,
	}); err != nil {
		t.Fatalf("cannot save: %v", err)
	}

	stored, err := os.ReadFile(filepath.Join(home, ".oneagent", "profiles", "team.json"))
	if err != nil {
		t.Fatalf("cannot read the stored profile: %v", err)
	}
	if strings.Contains(string(stored), key) {
		t.Errorf("the stored profile carries the key: %s", stored)
	}

	// And the assertion is not vacuous: the key really was written somewhere.
	secret, err := os.ReadFile(filepath.Join(home, ".oneagent", "secrets", "team.env"))
	if err != nil {
		t.Fatalf("cannot read the secret: %v", err)
	}
	if !strings.Contains(string(secret), key) {
		t.Errorf("the key was not stored at all, so the check above proves nothing")
	}
}

func TestTheSecretFileIsPrivateToTheUser(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores the permission bits this asserts")
	}
	store, home := newStore(t, "linux")
	if _, err := store.Save(SaveRequest{
		ID: "team", Provider: "ppio", Model: "m", AgentIDs: []string{"codex"}, APIKey: "sk-a",
	}); err != nil {
		t.Fatalf("cannot save: %v", err)
	}
	info, err := os.Stat(filepath.Join(home, ".oneagent", "secrets", "team.env"))
	if err != nil {
		t.Fatalf("cannot stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("the secret file is %v, want 0600", mode)
	}
}

func TestAKeyRoundTripsThroughBothShells(t *testing.T) {
	// The values that need quoting are the point: a key with a quote in it was
	// written escaped, so reading it back has to reverse the escaping rather than
	// strip the outer quotes.
	keys := []string{
		"sk-plain",
		"key with space",
		"key'with'quotes",
		`key"with"double`,
		"key$with$dollar",
		"key\\with\\backslash",
		"key#with#hash",
		"密钥",
	}
	for _, osID := range []string{"linux", "windows"} {
		for _, key := range keys {
			t.Run(osID+" "+key, func(t *testing.T) {
				store, _ := newStore(t, osID)
				if err := store.WriteSecret("p", key, "https://example.com/v1"); err != nil {
					t.Fatalf("cannot write: %v", err)
				}
				got, err := store.ReadSecret("p")
				if err != nil {
					t.Fatalf("cannot read: %v", err)
				}
				if got != key {
					t.Errorf("round trip gave %q, want %q", got, key)
				}
			})
		}
	}
}

func TestAHandEditedSecretFileCannotBreakTheCaller(t *testing.T) {
	// These files are user-editable by design, and shlex.split raises on an
	// unbalanced quote. In Python that ValueError escaped: `oneagent agent set`
	// printed a traceback and the server answered 500. Reporting no key held makes
	// the caller say "API key is required", which names something fixable.
	cases := map[string]string{
		"unbalanced single": "export ONEAGENT_API_KEY='unterminated\n",
		"unbalanced double": "export ONEAGENT_API_KEY=\"unterminated\n",
		"trailing escape":   "export ONEAGENT_API_KEY=trailing\\\n",
		"no assignment":     "# nothing here\n",
		"empty file":        "",
		"wrong variable":    "export SOMETHING_ELSE=value\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			store, home := newStore(t, "linux")
			path := filepath.Join(home, ".oneagent", "secrets")
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatalf("cannot prepare: %v", err)
			}
			if err := os.WriteFile(filepath.Join(path, "p.env"), []byte(content), 0o600); err != nil {
				t.Fatalf("cannot prepare: %v", err)
			}
			got, err := store.ReadSecret("p")
			if err != nil {
				t.Fatalf("a broken file must not fail the call: %v", err)
			}
			if got != "" {
				t.Errorf("read %q out of a broken file", got)
			}
		})
	}
}

func TestAPointerOutsideTheStoreIsRefused(t *testing.T) {
	// The pointer is a file, so its value is untrusted even though we wrote it.
	// It reaches SecretPath, so a traversal would name a key path outside the
	// secrets directory.
	for _, active := range []string{
		"../escape", "../../etc/passwd", "/absolute", "UPPER", "with space", "", "dot.dot",
	} {
		t.Run(active, func(t *testing.T) {
			store, home := newStore(t, "linux")
			if err := os.MkdirAll(filepath.Join(home, ".oneagent"), 0o700); err != nil {
				t.Fatalf("cannot prepare: %v", err)
			}
			pointer := `{"schema_version": 2, "active": ` + quoteJSON(active) + "}\n"
			if err := os.WriteFile(PointerPath(store.Runtime), []byte(pointer), 0o600); err != nil {
				t.Fatalf("cannot prepare: %v", err)
			}
			if id := store.ActiveID(); id != "" {
				t.Errorf("ActiveID accepted %q", id)
			}
			value, reason, err := store.Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if value != nil {
				t.Errorf("Load returned a profile for pointer %q", active)
			}
			if reason == "" {
				t.Error("a refused pointer must say why")
			}
		})
	}
}

func TestALegacyProfileIsMigratedRatherThanRefused(t *testing.T) {
	store, home := newStore(t, "linux")
	if err := os.MkdirAll(filepath.Join(home, ".oneagent"), 0o700); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	legacy := `{"schema_version": 1, "provider": "ppio", "base_url": null, ` +
		`"model": "gpt-5-mini", "agent_ids": ["codex"], "activated_at": "2026-01-01T00:00:00Z"}` + "\n"
	if err := os.WriteFile(PointerPath(store.Runtime), []byte(legacy), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}

	value, reason, err := store.Load()
	if err != nil || reason != "" {
		t.Fatalf("migration failed: reason=%q err=%v", reason, err)
	}
	if value == nil {
		t.Fatal("no profile came back")
	}
	if got := value.GetString("id"); got != "default" {
		t.Errorf("migrated id = %q, want default", got)
	}
	if got := value.GetString("model"); got != "gpt-5-mini" {
		t.Errorf("the legacy model was lost: %q", got)
	}
	// The timestamp is carried over, not reset: the profile was activated then,
	// and rewriting it would tell the user they just switched providers.
	if got := value.GetString("activated_at"); got != "2026-01-01T00:00:00Z" {
		t.Errorf("activated_at = %q, want the legacy value", got)
	}
	// Migration is in place, so a second read goes through the v2 path.
	again, reason, err := store.Load()
	if err != nil || reason != "" || again == nil {
		t.Fatalf("the migrated profile did not read back: reason=%q err=%v", reason, err)
	}
	if store.ActiveID() != "default" {
		t.Errorf("the pointer was not rewritten: %q", store.ActiveID())
	}
}

func TestListAnnotatesKeyPresenceWithoutReadingTheKey(t *testing.T) {
	store, _ := newStore(t, "linux")
	if _, err := store.Save(SaveRequest{
		ID: "withkey", Provider: "ppio", Model: "m", AgentIDs: []string{"codex"}, APIKey: "sk-a",
	}); err != nil {
		t.Fatalf("cannot save: %v", err)
	}
	if _, err := store.Save(SaveRequest{
		ID: "nokey", Provider: "ppio", Model: "m", AgentIDs: []string{"codex"},
	}); err != nil {
		t.Fatalf("cannot save: %v", err)
	}

	profiles, err := store.List()
	if err != nil {
		t.Fatalf("cannot list: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("got %d profiles, want 2", len(profiles))
	}
	// Sorted by file name, so nokey comes first.
	keyed := map[string]bool{}
	for _, item := range profiles {
		value, _ := item.Get("has_key")
		truth, _ := value.(bool)
		keyed[item.GetString("id")] = truth
	}
	if !keyed["withkey"] {
		t.Error("the profile with a key reports none")
	}
	if keyed["nokey"] {
		t.Error("the profile without a key reports one")
	}

	// And the summary the client sees says only that much about it.
	for _, item := range profiles {
		summary := PublicSummary(item)
		for _, key := range summary.Keys() {
			if key == "apiKey" || key == "api_key" || key == "secret" {
				t.Errorf("the public summary exposes %q", key)
			}
		}
		if _, present := summary.Get("hasKey"); !present {
			t.Error("the summary drops hasKey, which the wizard needs")
		}
	}
}

func TestAnUnreadableProfileIsSkippedRatherThanFailingTheList(t *testing.T) {
	// One corrupt file must not blank the whole overview, which is the same
	// guarantee the config readers give.
	store, home := newStore(t, "linux")
	if _, err := store.Save(SaveRequest{
		ID: "good", Provider: "ppio", Model: "m", AgentIDs: []string{"codex"},
	}); err != nil {
		t.Fatalf("cannot save: %v", err)
	}
	broken := filepath.Join(home, ".oneagent", "profiles", "broken.json")
	if err := os.WriteFile(broken, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}

	profiles, err := store.List()
	if err != nil {
		t.Fatalf("cannot list: %v", err)
	}
	if len(profiles) != 1 || profiles[0].GetString("id") != "good" {
		t.Errorf("got %d profiles, want only the readable one", len(profiles))
	}
}

func TestActivatingTheSameProviderAddsAgentsRatherThanReplacingThem(t *testing.T) {
	store, _ := newStore(t, "linux")
	first := ActivateRequest{
		AgentIDs: []string{"codex"}, Configure: true, Provider: "ppio",
		BaseURL: "https://api.ppio.com/openai/v1", Model: "m", APIKey: "sk-a",
	}
	if _, err := store.Activate(first); err != nil {
		t.Fatalf("cannot activate: %v", err)
	}
	second := first
	second.AgentIDs = []string{"aider"}
	if _, err := store.Activate(second); err != nil {
		t.Fatalf("cannot activate: %v", err)
	}

	value, _, err := store.Load()
	if err != nil || value == nil {
		t.Fatalf("cannot load: %v", err)
	}
	ids := []string{}
	for _, item := range agentIDsOf(value) {
		ids = append(ids, item.(string))
	}
	if len(ids) != 2 || ids[0] != "aider" || ids[1] != "codex" {
		t.Errorf("agent_ids = %v, want both Agents in order", ids)
	}

	// A different model is a different set: the earlier Agents are no longer
	// pointed where this profile says they are.
	third := first
	third.AgentIDs = []string{"opencode"}
	third.Model = "different"
	if _, err := store.Activate(third); err != nil {
		t.Fatalf("cannot activate: %v", err)
	}
	value, _, err = store.Load()
	if err != nil || value == nil {
		t.Fatalf("cannot load: %v", err)
	}
	ids = nil
	for _, item := range agentIDsOf(value) {
		ids = append(ids, item.(string))
	}
	if len(ids) != 1 || ids[0] != "opencode" {
		t.Errorf("agent_ids = %v, want only the newly configured Agent", ids)
	}
}

func TestABindingKeepsItsProfileRefAndCreationTime(t *testing.T) {
	store, _ := newStore(t, "linux")
	store.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 1, 0, time.UTC) }
	if _, err := store.WriteBinding("codex", "PPIO", "https://api.ppio.com/openai/v1", "m", "team"); err != nil {
		t.Fatalf("cannot write: %v", err)
	}

	store.Now = func() time.Time { return time.Date(2026, 6, 1, 0, 0, 1, 0, time.UTC) }
	// Re-pointing without naming a profile must not detach the binding from the
	// profile it came from.
	updated, err := store.WriteBinding("codex", "Novita", "https://api.novita.ai/openai/v1", "m2", "")
	if err != nil {
		t.Fatalf("cannot write: %v", err)
	}
	if got := updated.GetString("profile_ref"); got != "team" {
		t.Errorf("profile_ref = %q, want it carried over", got)
	}
	if got := updated.GetString("created_at"); got != "2026-01-01T00:00:01Z" {
		t.Errorf("created_at = %q, want the original", got)
	}
	if got := updated.GetString("updated_at"); got != "2026-06-01T00:00:01Z" {
		t.Errorf("updated_at = %q, want the new time", got)
	}
	if got := updated.GetString("model"); got != "m2" {
		t.Errorf("model = %q, want the new one", got)
	}
}

func TestABindingWithAnUnsupportedSchemaIsReportedNotGuessed(t *testing.T) {
	store, home := newStore(t, "linux")
	if err := os.MkdirAll(filepath.Join(home, ".oneagent", "agents"), 0o700); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}
	path := filepath.Join(home, ".oneagent", "agents", "codex.json")
	if err := os.WriteFile(path, []byte(`{"schema_version": 99, "model": "m"}`+"\n"), 0o600); err != nil {
		t.Fatalf("cannot prepare: %v", err)
	}

	value, reason, err := store.ReadBinding("codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != nil {
		t.Error("a future schema was read as if it were understood")
	}
	if !strings.Contains(reason, "schema") {
		t.Errorf("reason = %q, want it to name the schema", reason)
	}
	// And it does not appear in the listing, which feeds the overview.
	bindings, err := store.ListBindings()
	if err != nil {
		t.Fatalf("cannot list: %v", err)
	}
	if _, present := bindings["codex"]; present {
		t.Error("an unreadable binding reached the listing")
	}
}

func TestAnIllegalProfileIDIsRefusedEverywhereAPathIsBuilt(t *testing.T) {
	store, _ := newStore(t, "linux")
	for _, id := range []string{"../escape", "UPPER", "with space", "", "-leading", "a/b"} {
		t.Run(id, func(t *testing.T) {
			if _, err := StorePath(store.Runtime, id); err == nil {
				t.Error("StorePath accepted it")
			}
			if _, err := SecretPath(store.Runtime, id); err == nil {
				t.Error("SecretPath accepted it")
			}
			if _, err := store.Save(SaveRequest{
				ID: id, Provider: "ppio", Model: "m", AgentIDs: []string{"codex"}, APIKey: "sk-a",
			}); err == nil {
				t.Error("Save accepted it")
			}
		})
	}
}

func TestTheTimestampDropsTheFractionWhenItIsZero(t *testing.T) {
	// Python's isoformat omits the fractional part at an exact second, so a Go
	// format that always emits six digits would differ once in a million writes.
	store, _ := newStore(t, "linux")
	store.Now = func() time.Time { return time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC) }
	if got := store.timestamp(); got != "2026-07-29T12:00:00Z" {
		t.Errorf("timestamp = %q, want no fractional part", got)
	}
	store.Now = func() time.Time { return time.Date(2026, 7, 29, 12, 0, 0, 500000000, time.UTC) }
	if got := store.timestamp(); got != "2026-07-29T12:00:00.500000Z" {
		t.Errorf("timestamp = %q, want six fractional digits", got)
	}
}

// quoteJSON renders a string as a JSON value, so a traversal fixture can be
// written without hand-escaping it.
func quoteJSON(value string) string {
	encoded, err := jsonorder.Marshal(func() *jsonorder.Object {
		object := jsonorder.NewObject()
		object.Set("v", value)
		return object
	}())
	if err != nil {
		return `""`
	}
	text := string(encoded)
	start := strings.Index(text, `: `) + 2
	end := strings.LastIndex(text, "\n}")
	return strings.TrimSpace(text[start:end])
}
