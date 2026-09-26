package config

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaimoryLab/BootAgent/internal/provider"
	"gopkg.in/yaml.v3"
)

// DeepSeek Harness 0.1.7 removed $DSH_HOME/settings.yaml. Live configuration now
// lives in the profile the launcher boots: $DSH_HOME/profiles/<profile>/
// cordis.patch.yml, a top-level YAML sequence of loader patch rows. Each row
// addresses one plugin entry by id and replaces that entry's whole config, so a
// write that touches llm-pi-ai has to carry every provider the user declared
// there, not just the one BootAgent owns.
//
// The old document is imported once by dsh itself -- on first start it copies
// each section into the profile patch and renames the file settings.yaml.imported
// -- after which nothing reads settings.yaml again. A BootAgent write into it
// would be re-imported on the next launch, but agent-default-model has no import
// mapping, so the selection would be lost. The profile patch is therefore the
// only layout that works once the new release has run.
//
// Both layouts stay supported: the npm `latest` tag still ships 0.1.5, which
// reads settings.yaml, while the desktop application and `next` ship 0.1.7.
const (
	DSHProfilePatchName = "cordis.patch.yml"
	DSHLegacySettings   = "settings.yaml"
	// DSHWebProfile is what `dsh web` boots; DSHDesktopProfile is what the
	// Electron shell owns. They share $DSH_HOME but not configuration.
	DSHWebProfile     = "web"
	DSHDesktopProfile = "desktop"

	dshPiAIEntryID           = "llm-pi-ai"
	dshPiAIEntryName         = "@deepseek-ai/dsh-llm-pi-ai"
	dshDefaultModelEntryID   = "agent-default-model"
	dshDefaultModelEntryName = "@deepseek-ai/dsh-agent-default-model"
)

// ResolveDSHConfigPath picks the document BootAgent should write for one dsh
// profile.
//
// The desktop profile always resolves to its patch. The Electron shell exists
// only in the 0.1.7 line, so nothing that reads settings.yaml ever boots that
// profile; and the install flow leaves the app on disk without launching it,
// so at the moment the user configures it the profile directory does not exist
// yet. Falling back to the legacy file there would write a document the app
// imports only partially on first start. Creating the patch ahead of the app is
// safe: its initProfile fills in package.json, pnpm-workspace.yaml and a
// template patch only where each file is absent, and the directory is made
// with mkdir -p semantics.
//
// The web profile is what `dsh web` boots, and which release that is depends
// on the npm tag installed: `latest` is still 0.1.5, which reads settings.yaml.
// There the patch wins only once dsh itself has created the profile directory;
// otherwise the legacy file is written so the older CLI keeps working.
func ResolveDSHConfigPath(home, profile string) string {
	patch := filepath.Join(home, ".dsh", "profiles", profile, DSHProfilePatchName)
	if profile == DSHDesktopProfile {
		return patch
	}
	if info, err := os.Stat(filepath.Dir(patch)); err == nil && info.IsDir() {
		return patch
	}
	return filepath.Join(home, ".dsh", DSHLegacySettings)
}

// dshUsesProfilePatch reports whether path names the 0.1.7 profile layout.
func dshUsesProfilePatch(path string) bool {
	return filepath.Base(path) == DSHProfilePatchName
}

// dshCredentialsPath locates $DSH_HOME/.credentials.yaml from either config
// document. The credential store did not move with the settings: it stays at the
// harness home and is shared by every profile, so from a profile patch it is two
// directories up.
func dshCredentialsPath(configPath string) string {
	home := filepath.Dir(configPath)
	if dshUsesProfilePatch(configPath) {
		home = filepath.Dir(filepath.Dir(home))
	}
	return filepath.Join(home, ".credentials.yaml")
}

func (w Writer) writeDSHProfileRoute(ctx context.Context, path, providerName, baseURL, apiKey, model, protocolID string) error {
	root, err := yamlSequenceDocument(path, "DeepSeek Harness profile patch")
	if err != nil {
		return err
	}
	piAI := dshPatchRow(root.Content[0], dshPiAIEntryID, dshPiAIEntryName)
	config := yamlMappingChild(piAI, "config")
	providers := yamlMappingChild(config, "providers")

	route := &yaml.Node{Kind: yaml.MappingNode}
	apiName := "openai-completions"
	if protocolID == provider.ProtocolResponses {
		apiName = "openai-responses"
	}
	for _, item := range []struct{ key, value string }{
		{"displayName", providerName},
		{"apiKeyEnv", dshCredentialReference},
		{"api", apiName},
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
	yamlReplace(providers, dshOwnedRoute, route)

	selection := &yaml.Node{Kind: yaml.MappingNode}
	yamlSet(selection, "provider", dshOwnedRoute)
	yamlSet(selection, "model", model)
	yamlReplace(dshPatchRow(root.Content[0], dshDefaultModelEntryID, dshDefaultModelEntryName), "config", selection)

	data, err := encodeDSHProfilePatch(root)
	if err != nil {
		return configError("Cannot encode YAML configuration %s: %v", path, err)
	}
	return w.write(ctx, path, data, false)
}

func (w Writer) writeDSHProfileOfficial(ctx context.Context, path, model, reasoningEffort string) error {
	root, err := yamlSequenceDocument(path, "DeepSeek Harness profile patch")
	if err != nil {
		return err
	}
	// Lookup without creating, for the same reason as the legacy writer: when no
	// bootagent route exists there is nothing to clean up, and the check must
	// not leave an empty llm-pi-ai row behind.
	if piAI := dshFindPatchRow(root.Content[0], dshPiAIEntryID); piAI != nil {
		if config := yamlChild(piAI, "config"); config != nil {
			if providers := yamlChild(config, "providers"); providers != nil {
				yamlDelete(providers, dshOwnedRoute)
			}
		}
	}
	selection := &yaml.Node{Kind: yaml.MappingNode}
	yamlSet(selection, "provider", dshOfficialRoute)
	yamlSet(selection, "model", model)
	// Already validated by WriteDSHOfficial, before the credential was written.
	if reasoningEffort != "" {
		yamlSet(selection, "reasoningEffort", reasoningEffort)
	}
	yamlReplace(dshPatchRow(root.Content[0], dshDefaultModelEntryID, dshDefaultModelEntryName), "config", selection)

	data, err := encodeDSHProfilePatch(root)
	if err != nil {
		return configError("Cannot encode YAML configuration %s: %v", path, err)
	}
	return w.write(ctx, path, data, false)
}

// encodeDSHProfilePatch serializes the patch with two-space indentation, which
// is what dsh's own scaffold and Models page write. yaml.Marshal defaults to
// four, so a rewrite through it would re-indent every row the user had -- a
// semantically identical file, but a noisy diff for someone who keeps the
// profile in version control, and not what "leaves the rest untouched" should
// mean.
func encodeDSHProfilePatch(root *yaml.Node) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(root); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// dshFindPatchRow returns the row addressing entry id, or nil.
func dshFindPatchRow(sequence *yaml.Node, id string) *yaml.Node {
	for _, row := range sequence.Content {
		if row.Kind != yaml.MappingNode {
			continue
		}
		if value := yamlLookup(row, "id"); value != nil && value.Value == id {
			return row
		}
	}
	return nil
}

// dshPatchRow returns the row addressing entry id, appending one when absent.
// A new row names the plugin package too: the bundle already mounts the entry,
// so the name is redundant there, but dsh's own Models page writes it and a row
// that carries it stays readable to someone editing the file by hand.
func dshPatchRow(sequence *yaml.Node, id, name string) *yaml.Node {
	if row := dshFindPatchRow(sequence, id); row != nil {
		return row
	}
	row := &yaml.Node{Kind: yaml.MappingNode}
	yamlSet(row, "id", id)
	yamlSet(row, "name", name)
	sequence.Content = append(sequence.Content, row)
	return row
}

// yamlMappingChild returns the mapping under key, replacing a non-mapping value
// and creating the entry when absent. Unlike yamlMapping it does not fail on a
// scalar: inside a patch row a stray `config:` with no value is a null the user
// left behind, and the write is about to give it content.
func yamlMappingChild(parent *yaml.Node, key string) *yaml.Node {
	if child := yamlChild(parent, key); child != nil {
		return child
	}
	child := &yaml.Node{Kind: yaml.MappingNode}
	yamlReplace(parent, key, child)
	return child
}

// yamlSequenceDocument parses a file whose document is a top-level sequence,
// the shape of a cordis.patch.yml. An absent or blank file starts an empty
// sequence. The bundle patches use `!!js` tagged scalars; yaml.v3 keeps unknown
// tags on the node, so they round-trip through this without being evaluated or
// rewritten.
func yamlSequenceDocument(path, label string) (*yaml.Node, error) {
	text, err := readText(path)
	if err != nil {
		return nil, configError("Cannot read existing %s %s: %v", label, path, err)
	}
	if isBlankYAML(text) {
		// dsh's scaffold writes a header comment and nothing else. yaml.v3 parses
		// that as an empty document, so the comment is carried over by hand: it
		// is the user's orientation in a file dsh tells them to edit directly.
		sequence := &yaml.Node{Kind: yaml.SequenceNode, HeadComment: strings.TrimRight(text, "\n")}
		return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{sequence}}, nil
	}
	root := &yaml.Node{}
	if err := yaml.Unmarshal([]byte(text), root); err != nil {
		return nil, configError("Existing %s is invalid: %s: %v", label, path, err)
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.SequenceNode {
		return nil, configError("Existing %s must contain a list: %s", label, path)
	}
	// The desktop app's scaffold ends in a flow-style `[]`. Rows appended to it
	// would inherit that style and land on one line in a file meant for hand
	// editing, so an empty list is written back in block style.
	if sequence := root.Content[0]; len(sequence.Content) == 0 {
		sequence.Style &^= yaml.FlowStyle
	}
	return root, nil
}

// isBlankYAML is true for a file with no content besides comments, which is what
// dsh's scaffold leaves in a fresh cordis.patch.yml: a header comment and
// nothing else. yaml.v3 parses that as an empty document, so it has to be
// caught before Unmarshal, which would otherwise report no sequence.
func isBlankYAML(text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return false
		}
	}
	return true
}
