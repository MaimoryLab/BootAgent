package catalog

import (
	"errors"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
)

func TestTheEmbeddedManifestLoads(t *testing.T) {
	manifest := MustLoad()
	if manifest.SchemaVersion != SchemaVersion {
		t.Errorf("schema version = %d, want %d", manifest.SchemaVersion, SchemaVersion)
	}
	if len(manifest.Agents) == 0 {
		t.Fatal("the embedded manifest declares no agents")
	}
}

func TestLoadingTwiceReturnsTheSameParsedManifest(t *testing.T) {
	// It cannot change at runtime, so parsing it repeatedly would be waste.
	first, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, _ := Load()
	if first != second {
		t.Error("Load reparsed the manifest instead of reusing it")
	}
}

func TestAnUnsupportedSchemaIsRefusedRatherThanPartiallyRead(t *testing.T) {
	// A newer manifest may mean something different by the same key. Reading
	// what we recognise and ignoring the rest is how a version pin gets
	// silently dropped.
	_, err := Parse([]byte(`{"schema_version": 2, "agents": {"x": {"name": "X"}}}`))
	if err == nil {
		t.Fatal("a future schema version must be refused")
	}
	assertCode(t, err, "INVALID_REQUEST")
}

func TestAManifestWithNoAgentsIsRefused(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version": 1, "agents": {}}`,
		`{"schema_version": 1}`,
	} {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("%s should be refused", raw)
		}
	}
}

func TestUnparseableJSONReportsTheSameCodeAsPython(t *testing.T) {
	_, err := Parse([]byte("{not json"))
	if err == nil {
		t.Fatal("malformed JSON must be refused")
	}
	assertCode(t, err, "INVALID_REQUEST")
}

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	var oneAgentErr *oerr.Error
	if !errors.As(err, &oneAgentErr) {
		t.Fatalf("err = %v, want an *oerr.Error", err)
	}
	if oneAgentErr.Code != want {
		t.Errorf("code = %q, want %q", oneAgentErr.Code, want)
	}
}

func TestAdapterDecidesTheProtocolRatherThanTheAgentId(t *testing.T) {
	// Keyed on the adapter so two Agents sharing a config format share the
	// entry, and so nothing above has an excuse to branch on an id.
	for adapter, want := range map[string]string{
		"codex":       ProtocolResponses,
		"claude-code": ProtocolAnthropic,
		"opencode":    ProtocolOpenAI,
		"kilo-cli":    ProtocolOpenAI,
		"aider":       ProtocolOpenAI,
	} {
		if got := AgentProtocol(adapter); got != want {
			t.Errorf("AgentProtocol(%q) = %q, want %q", adapter, got, want)
		}
	}
}

func TestAnUnknownAdapterFallsBackToOpenAICompatible(t *testing.T) {
	// The documented default. An adapter added without an entry must not
	// silently probe a protocol it does not speak -- it gets the one most
	// endpoints serve, and the probe reports the truth either way.
	if got := AgentProtocol("brand-new-adapter"); got != ProtocolOpenAI {
		t.Errorf("got %q, want %q", got, ProtocolOpenAI)
	}
	if got := AgentProtocol(""); got != ProtocolOpenAI {
		t.Errorf("empty adapter: got %q, want %q", got, ProtocolOpenAI)
	}
}

func TestEveryAutoAgentDeclaresWhatTheCoreNeedsToActOnIt(t *testing.T) {
	// The lock is the single source of truth, which only holds if the fields
	// the core reads are actually there. Claude Code once reported "configured"
	// while unable to authenticate because behaviour was inferred from an id
	// instead of read from here.
	manifest := MustLoad()
	for _, id := range manifest.AutoAgents() {
		agent, _ := manifest.Agent(id)
		t.Run(id, func(t *testing.T) {
			if agent.Command == "" {
				t.Error("no command: the start hint and restart hint are derived from it")
			}
			if agent.ConfigPath == "" {
				t.Error("no config_path")
			}
			if agent.ConfigAdapter == "" {
				t.Error("no config_adapter: the protocol is derived from it")
			}
			if agent.CredentialDelivery == "" {
				t.Error("no credential_delivery: how the key reaches the Agent would have to be guessed")
			}
			if agent.Package == nil || agent.Package.Version == "" {
				t.Error("no pinned version")
			}
		})
	}
}

func TestGuideOnlyAgentsAreNeitherInstalledNorConfigured(t *testing.T) {
	manifest := MustLoad()
	auto := map[string]bool{}
	for _, id := range manifest.AutoAgents() {
		auto[id] = true
	}
	for id, agent := range manifest.Agents {
		if agent.ConfigMode == "auto" {
			if !auto[id] {
				t.Errorf("%s is auto but missing from AutoAgents", id)
			}
			continue
		}
		if auto[id] {
			t.Errorf("%s is guide-only but listed as auto", id)
		}
		if !agent.GuideOnly() {
			t.Errorf("%s has config_mode %q but does not report as guide-only", id, agent.ConfigMode)
		}
	}
}

func TestAnAbsentRankSortsLastRatherThanFirst(t *testing.T) {
	// Prominence is independent of whether OneAgent can install the Agent, and
	// a missing rank must not accidentally promote one to the first screen.
	agent := Agent{}
	if got := agent.RankOrDefault(); got != 99 {
		t.Errorf("rank = %d, want 99", got)
	}
	zero := 0
	if got := (Agent{Rank: &zero}).RankOrDefault(); got != 0 {
		t.Errorf("an explicit rank of 0 must survive; got %d", got)
	}
}

func TestCatalogOrderIsRankThenIdSoItIsTotal(t *testing.T) {
	// Ties broken by id, otherwise two clients sorting the same ranks could
	// disagree and the order would not be a contract at all.
	manifest := &Manifest{Agents: map[string]Agent{
		"zebra":  {Rank: intPtr(1)},
		"alpha":  {Rank: intPtr(1)},
		"middle": {Rank: intPtr(0)},
		"absent": {},
	}}
	got := manifest.IDs()
	want := []string{"middle", "alpha", "zebra", "absent"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestAskingForAnUnknownAgentReportsAbsenceRatherThanAZeroValue(t *testing.T) {
	if _, present := MustLoad().Agent("no-such-agent"); present {
		t.Error("an unknown id must report present=false")
	}
}

func intPtr(value int) *int { return &value }
