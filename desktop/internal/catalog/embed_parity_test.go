package catalog

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The manifest is the single source of truth for every Agent's behaviour, and
// go:embed forces a copy of it into this package. A copy that can drift is a
// second source of truth wearing a disguise, so these tests hold it to the
// original: byte-for-byte, and then again through the Python parser, because
// identical bytes still leave room for the two implementations to read them
// differently.

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

func TestParityEmbeddedManifestIsByteIdenticalToTheRealOne(t *testing.T) {
	source := filepath.Join(repoRoot(t), "agents.lock.json")
	want, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("cannot read %s: %v", source, err)
	}
	if string(manifestJSON) != string(want) {
		t.Fatalf(
			"the embedded manifest has drifted from %s (%d bytes embedded, %d at the root).\n"+
				"Run: cd desktop && go generate ./internal/catalog/",
			source, len(manifestJSON), len(want),
		)
	}
}

// pythonBin finds an interpreter, or fails on CI where the comparison is
// mandatory. Skipping would retire a cross-language gate without anything
// going red.
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

// pythonCatalog runs the real public_catalog() from the Python core. Calling it
// rather than reimplementing it is the point: a reimplementation would only
// prove the test agrees with itself.
func pythonCatalog(t *testing.T) []map[string]any {
	t.Helper()
	root := repoRoot(t)
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from oneagent import catalog
print(json.dumps(catalog.public_catalog()))
`
	cmd := exec.Command(pythonBin(t), "-c", script, root)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python public_catalog failed: %v", err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}
	return parsed
}

func TestParityPublicProvidersMatchPython(t *testing.T) {
	// Both sides project the same subset deliberately, and the frontend reads
	// it. A field added to one projection and not the other would surface as a
	// missing base URL in the wizard rather than as a build failure.
	root := repoRoot(t)
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from oneagent import catalog
print(json.dumps(catalog.public_providers()))
`
	cmd := exec.Command(pythonBin(t), "-c", script, root)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python public_providers failed: %v", err)
	}
	var python map[string]map[string]string
	if err := json.Unmarshal(output, &python); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}

	got := PublicProviders()
	if len(got) != len(python) {
		t.Fatalf("Go exposes %d providers, Python exposes %d", len(got), len(python))
	}
	for id, want := range python {
		entry, present := got[id]
		if !present {
			t.Errorf("%s is in the Python projection but not the Go one", id)
			continue
		}
		if entry.Name != want["name"] || entry.Home != want["home"] || entry.BaseURL != want["base_url"] {
			t.Errorf("%s: Go %+v, Python %v", id, entry, want)
		}
		if entry.AnthropicBaseURL != want["anthropic_base_url"] {
			t.Errorf("%s: anthropic base Go=%q Python=%q", id, entry.AnthropicBaseURL, want["anthropic_base_url"])
		}
	}

	// The key sets must match too, or one side is exposing something the other
	// decided to keep internal.
	for id := range got {
		if _, present := python[id]; !present {
			t.Errorf("%s is in the Go projection but not the Python one", id)
		}
	}
	var goKeys map[string]map[string]any
	encoded, _ := json.Marshal(got)
	_ = json.Unmarshal(encoded, &goKeys)
	for id, fields := range goKeys {
		for key := range fields {
			if _, present := python[id][key]; !present {
				t.Errorf("%s: Go emits field %q that Python does not", id, key)
			}
		}
	}
}

func TestParityMirrorsMatchPython(t *testing.T) {
	root := repoRoot(t)
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from oneagent import catalog
print(json.dumps(catalog.public_mirrors()))
`
	cmd := exec.Command(pythonBin(t), "-c", script, root)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("python public_mirrors failed: %v", err)
	}
	var python []map[string]string
	if err := json.Unmarshal(output, &python); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}

	got := PublicMirrors()
	if len(got) != len(python) {
		t.Fatalf("Go lists %d mirrors, Python lists %d", len(got), len(python))
	}
	// Order included: the first entry is the default a user gets without
	// choosing, so a reordering is a behaviour change.
	for index, want := range python {
		mirror := got[index]
		if mirror.ID != want["id"] || mirror.Registry != want["registry"] || mirror.Upstream != want["upstream"] {
			t.Errorf("mirror %d: Go %+v, Python %v", index, mirror, want)
		}
		if mirror.Name != want["name"] || mirror.Note != want["note"] {
			t.Errorf("mirror %d: user-visible text differs:\n  Go:     %q / %q\n  Python: %q / %q",
				index, mirror.Name, mirror.Note, want["name"], want["note"])
		}
	}
}

func TestParityCatalogOrderMatchesPython(t *testing.T) {
	// Order is a contract: it is sorted in one place so every client shows the
	// same list without re-deriving it, and two clients cannot disagree.
	python := pythonCatalog(t)
	wantOrder := make([]string, 0, len(python))
	for _, item := range python {
		wantOrder = append(wantOrder, item["id"].(string))
	}

	manifest := MustLoad()
	gotOrder := manifest.IDs()

	if strings.Join(gotOrder, ",") != strings.Join(wantOrder, ",") {
		t.Fatalf("catalog order differs:\n  Go:     %v\n  Python: %v", gotOrder, wantOrder)
	}
}

func TestParityEveryAgentFieldTheCoreReadsMatchesPython(t *testing.T) {
	// Identical bytes do not guarantee identical reads: a missing JSON tag or a
	// wrong type would surface here rather than in whatever the field decides
	// several phases from now.
	python := pythonCatalog(t)
	manifest := MustLoad()

	for _, item := range python {
		id := item["id"].(string)
		t.Run(id, func(t *testing.T) {
			agent, present := manifest.Agent(id)
			if !present {
				t.Fatalf("%s is in the Python catalog but not the Go manifest", id)
			}
			if agent.Name != item["name"].(string) {
				t.Errorf("name = %q, Python says %q", agent.Name, item["name"])
			}
			if agent.Group != item["group"].(string) {
				t.Errorf("group = %q, Python says %q", agent.Group, item["group"])
			}
			if agent.ConfigMode != item["configMode"].(string) {
				t.Errorf("configMode = %q, Python says %q", agent.ConfigMode, item["configMode"])
			}
			if agent.GuideOnly() != item["guideOnly"].(bool) {
				t.Errorf("guideOnly = %v, Python says %v", agent.GuideOnly(), item["guideOnly"])
			}
			if got, want := float64(agent.RankOrDefault()), item["rank"].(float64); got != want {
				t.Errorf("rank = %v, Python says %v", got, want)
			}
			// lockedVersion is null for guide-only Agents, which is how the
			// frontend tells "pinned" from "nothing to pin".
			if raw, present := item["lockedVersion"]; present && raw != nil {
				if agent.Package == nil {
					t.Fatalf("Python reports version %v but Go parsed no package", raw)
				}
				if agent.Package.Version != raw.(string) {
					t.Errorf("version = %q, Python says %q", agent.Package.Version, raw)
				}
			} else if agent.Package != nil && agent.Package.Version != "" {
				t.Errorf("Go parsed version %q but Python reports none", agent.Package.Version)
			}
			// protocol is null for guide-only Agents; for the rest it must be
			// derived from the adapter, not from the id.
			if raw := item["protocol"]; raw != nil {
				if got := AgentProtocol(agent.ConfigAdapter); got != raw.(string) {
					t.Errorf("protocol = %q, Python says %q", got, raw)
				}
			} else if !agent.GuideOnly() {
				t.Errorf("Python reports no protocol for an auto Agent")
			}
		})
	}
}
