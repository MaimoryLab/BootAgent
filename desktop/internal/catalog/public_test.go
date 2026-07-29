package catalog

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestTheDeclaredOrderIsNotTheRankOrder(t *testing.T) {
	// Both orders reach the frontend and they are genuinely different -- cursor is
	// third by rank and eighth by declaration -- so a single accessor would silently
	// change one of the two arrays the UI renders.
	manifest := MustLoad()
	declared := manifest.DeclaredIDs()
	ranked := manifest.IDs()

	if len(declared) != len(manifest.Agents) {
		t.Fatalf("DeclaredIDs returned %d ids for %d agents", len(declared), len(manifest.Agents))
	}
	if len(declared) != len(ranked) {
		t.Fatalf("the two orders cover different sets: %d and %d", len(declared), len(ranked))
	}
	same := true
	for index := range declared {
		if declared[index] != ranked[index] {
			same = false
			break
		}
	}
	if same {
		t.Skip("the manifest currently declares Agents in rank order, so this cannot be distinguished")
	}

	// Same set, different order.
	seen := map[string]int{}
	for _, id := range declared {
		seen[id]++
	}
	for _, id := range ranked {
		seen[id]++
	}
	for id, count := range seen {
		if count != 2 {
			t.Errorf("%s appears in only one of the two orders", id)
		}
	}
	// And the returned slice is a copy, so a caller cannot reorder the manifest.
	declared[0] = "mutated"
	if manifest.DeclaredIDs()[0] == "mutated" {
		t.Error("DeclaredIDs handed out the manifest's own slice")
	}
}

func TestTheDeclaredOrderMatchesTheFile(t *testing.T) {
	// Read straight from the embedded bytes, so this checks the order-preserving
	// decode rather than agreeing with itself.
	var raw struct {
		Agents json.RawMessage `json:"agents"`
	}
	if err := json.Unmarshal(manifestJSON, &raw); err != nil {
		t.Fatalf("cannot read the manifest: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw.Agents))
	if _, err := decoder.Token(); err != nil {
		t.Fatalf("cannot read the agents object: %v", err)
	}
	wanted := []string{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			t.Fatalf("cannot read a key: %v", err)
		}
		wanted = append(wanted, token.(string))
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			t.Fatalf("cannot skip a value: %v", err)
		}
	}

	got := MustLoad().DeclaredIDs()
	if len(got) != len(wanted) {
		t.Fatalf("got %d ids, the file lists %d", len(got), len(wanted))
	}
	for index := range wanted {
		if got[index] != wanted[index] {
			t.Fatalf("order differs at %d:\n  got:  %v\n  file: %v", index, got, wanted)
		}
	}
}

func TestThePublicCatalogCarriesNothingInternal(t *testing.T) {
	// The projection exists so an internal field cannot leak by being added to the
	// Agent struct: the pinned package's integrity, source and command are none of
	// the client's business.
	items := MustLoad().PublicCatalog("linux")
	encoded, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	for _, forbidden := range []string{
		"integrity", "sha512", "credential_delivery", "env_vars", "config_path",
		"config_adapter", "windows_prerequisites", "command",
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("the public catalog exposes %q", forbidden)
		}
	}
	// And it is not vacuous: the entries are there.
	if len(items) != len(MustLoad().Agents) {
		t.Errorf("got %d items for %d agents", len(items), len(MustLoad().Agents))
	}
}

func TestThePublicCatalogIsSortedByRankThenID(t *testing.T) {
	items := MustLoad().PublicCatalog("linux")
	for index := 1; index < len(items); index++ {
		previous, current := items[index-1], items[index]
		if previous.Rank > current.Rank {
			t.Errorf("%s (rank %d) sorts after %s (rank %d)",
				previous.ID, previous.Rank, current.ID, current.Rank)
		}
		if previous.Rank == current.Rank && previous.ID > current.ID {
			t.Errorf("%s sorts after %s at equal rank", previous.ID, current.ID)
		}
	}
}

func TestTheWindowsNoteAppearsOnlyOnWindows(t *testing.T) {
	// A Windows caveat shown on macOS is misleading rather than merely useless.
	onWindows := MustLoad().PublicCatalog("windows")
	onLinux := MustLoad().PublicCatalog("linux")
	notes := 0
	for _, item := range onWindows {
		if item.PlatformNote != "" {
			notes++
		}
	}
	if notes == 0 {
		t.Skip("no Agent declares a Windows note, so this cannot be distinguished")
	}
	for _, item := range onLinux {
		if item.PlatformNote != "" {
			t.Errorf("%s shows a platform note on linux: %q", item.ID, item.PlatformNote)
		}
	}
}

func TestOnlyManagedAgentsReportAProtocol(t *testing.T) {
	// A guide-only Agent has no managed config, so claiming a protocol for it would
	// imply OneAgent can probe and configure it.
	manifest := MustLoad()
	for _, item := range manifest.PublicCatalog("linux") {
		agent, _ := manifest.Agent(item.ID)
		if agent.GuideOnly() {
			if item.Protocol != nil {
				t.Errorf("%s is guide-only but reports protocol %q", item.ID, *item.Protocol)
			}
			continue
		}
		if item.Protocol == nil {
			t.Errorf("%s is managed but reports no protocol", item.ID)
			continue
		}
		if *item.Protocol != AgentProtocol(agent.ConfigAdapter) {
			t.Errorf("%s protocol = %q, want the adapter's", item.ID, *item.Protocol)
		}
	}
}

func TestADuplicateAgentCollapsesToTheLastOneJustAsPythonDoes(t *testing.T) {
	// I first wrote this expecting a refusal, on the reasoning that a duplicate key
	// means the file is wrong. Python's json.loads keeps the last of two identical
	// keys and reports one Agent, so refusing here would be a divergence in what
	// counts as a loadable manifest -- and the manifest is the same file on both
	// sides. The declared order has to agree with that, which is what this pins.
	raw := []byte(`{"schema_version":1,"agents":{` +
		`"codex":{"name":"A","config_mode":"guide"},` +
		`"codex":{"name":"B","config_mode":"guide"}}}`)
	manifest, err := Parse(raw)
	if err != nil {
		t.Fatalf("a manifest Python loads was refused: %v", err)
	}
	if len(manifest.Agents) != 1 {
		t.Fatalf("got %d agents, want the duplicate collapsed", len(manifest.Agents))
	}
	agent, _ := manifest.Agent("codex")
	if agent.Name != "B" {
		t.Errorf("name = %q, want the last declaration", agent.Name)
	}
	if declared := manifest.DeclaredIDs(); len(declared) != 1 || declared[0] != "codex" {
		t.Errorf("declared order = %v, want one entry matching the parsed map", declared)
	}
}

func TestAManifestWithoutAnAgentsObjectIsRefused(t *testing.T) {
	if _, err := Parse([]byte(`{"schema_version":1,"agents":[]}`)); err == nil {
		t.Error("an agents array was accepted")
	}
	if _, err := Parse([]byte(`not json`)); err == nil {
		t.Error("unparseable bytes were accepted")
	}
	if _, err := Parse([]byte(`{"schema_version":99,"agents":{"a":{}}}`)); err == nil {
		t.Error("a future schema was accepted")
	}
}
