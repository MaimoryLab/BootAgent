package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/modality"
	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/provider"
	"github.com/MaimoryLab/BootAgent/internal/skill"
	"github.com/MaimoryLab/BootAgent/internal/skillgen"
	"github.com/MaimoryLab/BootAgent/internal/transfer"
)

type blockingMultimodalListing struct{ entered, release chan struct{} }

func (g *blockingMultimodalListing) Do(request *http.Request) (*http.Response, error) {
	close(g.entered)
	<-g.release
	return appProviderResponse(http.StatusOK, gatewayListing), nil
}

func TestMultimodalInstallRejectsProviderChangedDuringListing(t *testing.T) {
	gateway := &blockingMultimodalListing{make(chan struct{}), make(chan struct{})}
	home := t.TempDir()
	core := NewUseCasesWithProviderClient(StatusOptions{Home: home, Platform: platform.Info{OS: "darwin", Arch: "arm64"}, Lookup: func(command string) (string, bool) { return "/fake/" + command, true }}, provider.NewClient(gateway))
	ctx := context.Background()
	if _, err := core.SaveProvider(ctx, provider.Entry{ID: "gw", Name: "Gateway", BaseURL: "https://old.example/v1", APIKey: multimodalKey}, true, false); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := core.InstallMultimodal(ctx, MultimodalInstallRequest{Provider: "gw", Model: "qwen/qwen3-vl-8b", Capability: modality.Vision, Agents: []string{"codex"}})
		done <- err
	}()
	<-gateway.entered
	// This also proves the model listing does not hold the configuration lock.
	_, err := core.SaveProvider(ctx, provider.Entry{ID: "gw", Name: "Gateway", BaseURL: "https://new.example/v1", APIKey: "sk-rotated-key"}, false, false)
	close(gateway.release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("an obsolete Provider snapshot was published")
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "skills", "bootagent-vision")); !os.IsNotExist(err) {
		t.Fatal("a Skill was written after the Provider changed")
	}
}

func TestAllCredentialWritesRefreshGeneratedSkillKeys(t *testing.T) {
	for _, operation := range []string{"profile", "activate", "install"} {
		t.Run(operation, func(t *testing.T) {
			core, home := multimodalInstallCore(t, &multimodalGateway{listing: gatewayListing})
			core.status.Platform = platform.For("macos", "arm64")
			installSkill(t, core, "qwen/qwen3-vl-8b", modality.Vision, "codex", "opencode")
			if err := writeMultimodalCredential(core, operation); err != nil {
				t.Fatal(err)
			}
			if codexSet(t, home)["BOOTAGENT_GW_API_KEY"] != "sk-rotated-key" {
				t.Fatal("the native Skill variable kept the previous credential")
			}
			data, err := os.ReadFile(filepath.Join(home, ".bootagent", "skill-env", "gw.env"))
			if err != nil || !strings.Contains(string(data), "sk-rotated-key") {
				t.Fatal("the fallback Skill variable kept the previous credential")
			}
		})
	}
}

func writeMultimodalCredential(core *UseCases, operation string) error {
	ctx := context.Background()
	var err error
	switch operation {
	case "profile":
		_, err = core.SaveProfile(ctx, SaveProfileOptions{ID: "p", Label: "P", Provider: "gw", APIKey: "sk-rotated-key", Model: "qwen/qwen3-vl-8b", ConfigMode: "provider"})
	case "activate":
		_, err = core.ActivateAgent(ctx, ActivateAgentOptions{AgentID: "codex", Provider: "gw", APIKey: "sk-rotated-key", Model: "qwen/qwen3-vl-8b"})
	case "install":
		_, err = core.InstallAgents(ctx, InstallAgentsOptions{Agents: []string{"codex"}, Provider: "gw", APIKey: "sk-rotated-key", Model: "qwen/qwen3-vl-8b", Configure: true, SkipTest: true, Timeout: time.Minute})
	}
	return err
}

func TestCredentialWritesReportSkillEnvironmentFailureAndRetryTheSameKey(t *testing.T) {
	for _, operation := range []string{"profile", "activate", "install"} {
		t.Run(operation, func(t *testing.T) {
			core, home := multimodalInstallCore(t, &multimodalGateway{listing: gatewayListing})
			core.status.Platform = platform.For("macos", "arm64")
			installSkill(t, core, "qwen/qwen3-vl-8b", modality.Vision, "codex")
			path := filepath.Join(home, ".codex", "config.toml")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("invalid = ["), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := writeMultimodalCredential(core, operation); err == nil || !strings.Contains(err.Error(), "generated Skills could not be updated") {
				t.Fatalf("failed environment update was hidden: %v", err)
			}
			entry, err := core.GetProvider(context.Background(), "gw")
			if err != nil || entry.APIKey != "sk-rotated-key" {
				t.Fatal("the saved Provider credential was lost")
			}
			if err := os.WriteFile(path, before, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := writeMultimodalCredential(core, operation); err != nil {
				t.Fatal(err)
			}
			if codexSet(t, home)["BOOTAGENT_GW_API_KEY"] != "sk-rotated-key" {
				t.Fatal("retrying the same saved key did not repair the Skill environment")
			}
		})
	}
}

func TestNativeSkillKeyUsesTheNativeEndpointWhenOpenAIRefreshFails(t *testing.T) {
	core, home := jiekouCore(t, &multimodalGateway{listing: `{"data":[]}`})
	ctx := context.Background()
	if check, err := core.ProbeMultimodal(ctx, "jiekou", "minimax-speech-2.8-turbo", modality.TextToSpeech, modality.AdapterNovitaAudio, false); err != nil || check.Status != ModelCheckAvailable {
		t.Fatalf("probe = %#v, %v", check, err)
	}
	if result, err := core.InstallMultimodal(ctx, MultimodalInstallRequest{Provider: "jiekou", Model: "minimax-speech-2.8-turbo", Capability: modality.TextToSpeech, Agents: []string{"codex"}}); err != nil || !result.Results[0].EnvWritten {
		t.Fatalf("install = %#v, %v", result, err)
	}
	path := filepath.Join(home, ".codex", "skills", "bootagent-text-to-speech", "SKILL.md")
	if err := os.WriteFile(path, []byte("user edited this Skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := core.SaveProvider(ctx, provider.Entry{ID: "jiekou", Name: "JieKou.AI", BaseURL: "https://new.example/v1", APIKey: "sk-rotated-key"}, false, false)
	if err != nil || result.Failures["codex"] == "" {
		t.Fatalf("expected the user-edited tree to retain its refresh failure: %v", err)
	}
	if codexSet(t, home)["BOOTAGENT_JIEKOU_API_KEY"] != "sk-rotated-key" {
		t.Fatal("the key was blocked despite an unchanged native request endpoint")
	}
}

func TestUnchangedCredentialDoesNotRequireAReadableSkillRegistry(t *testing.T) {
	for _, operation := range []string{"profile", "activate", "install"} {
		t.Run(operation, func(t *testing.T) {
			core, _ := multimodalInstallCore(t, &multimodalGateway{listing: gatewayListing})
			core.status.Platform = platform.For("macos", "arm64")
			installSkill(t, core, "qwen/qwen3-vl-8b", modality.Vision, "codex")
			// Make the unchanged-key request match the saved Provider.
			if err := core.providers.SaveKey(context.Background(), "gw", "sk-rotated-key"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(core.skillStore().RegistryPath(), []byte("invalid"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := writeMultimodalCredential(core, operation); err != nil {
				t.Fatalf("the unchanged credential required an unrelated Registry: %v", err)
			}
		})
	}
}

func TestMultimodalGeneratedOutputPreservesReinstallAndUninstall(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]string{"b64_json": "aW1hZ2U="}}})
	}))
	defer server.Close()
	core, home := multimodalInstallCore(t, &multimodalGateway{listing: gatewayListing})
	ctx := context.Background()
	if _, err := core.SaveProvider(ctx, provider.Entry{ID: "gw", Name: "Gateway", BaseURL: server.URL + "/v1", APIKey: multimodalKey}, false, false); err != nil {
		t.Fatal(err)
	}
	if check, err := core.ProbeMultimodal(ctx, "gw", "gpt-image-1", modality.ImageGeneration, modality.AdapterImages, true); err != nil || check.Status != ModelCheckAvailable {
		t.Fatalf("probe = %#v, %v", check, err)
	}
	result := installSkill(t, core, "gpt-image-1", modality.ImageGeneration, "codex")
	if !result.Results[0].EnvWritten {
		t.Fatalf("install = %#v", result)
	}
	dir := filepath.Join(home, ".codex", "skills", "bootagent-image-generation")
	before, err := skill.HashTree(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(runCtx, node, filepath.Join(dir, "scripts", "run.mjs"), "generate", "--prompt", "a lighthouse")
	command.Dir = dir
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "BOOTAGENT_GW_API_KEY=" + multimodalKey}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generated script failed: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(output)), filepath.Join(home, ".bootagent", "output")+string(os.PathSeparator)) {
		t.Fatal("generation did not write to the separate output directory")
	}
	after, err := skill.HashTree(ctx, dir)
	if err != nil || before != after {
		t.Fatal("generation changed the managed Skill tree")
	}
	if result := installSkill(t, core, "gpt-image-1", modality.ImageGeneration, "codex"); !result.Results[0].Installed || result.Results[0].Error != "" {
		t.Fatalf("reinstall = %#v", result)
	}
	if result := core.UninstallSkill(ctx, "bootagent-image-generation"); result.Error != "" {
		t.Fatalf("uninstall = %#v", result)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the generated Skill was not removed")
	}
}

func providerTransfer(t *testing.T) []byte {
	t.Helper()
	config, err := json.Marshal(map[string]any{"providers": map[string]any{"providers": []provider.Entry{{ID: "gw", Name: "Gateway", BaseURL: "https://new.example/v1", APIKey: "sk-rotated-key"}}}})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := transfer.Build(map[string][]byte{"config.json": config})
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func TestTransferProviderChangeRefreshesInstalledGeneratedSkills(t *testing.T) {
	core, home := multimodalInstallCore(t, &multimodalGateway{listing: gatewayListing})
	installSkill(t, core, "qwen/qwen3-vl-8b", modality.Vision, "codex", "opencode")
	if err := core.ImportTransferV2(context.Background(), providerTransfer(t)); err != nil {
		t.Fatal(err)
	}
	cfg, err := skillgen.ReadConfig(filepath.Join(home, ".codex", "skills", "bootagent-vision"))
	if err != nil || cfg.BaseURL != "https://new.example/v1" {
		t.Fatal("the imported Provider endpoint did not reach its Skill")
	}
	if codexSet(t, home)["BOOTAGENT_GW_API_KEY"] != "sk-rotated-key" {
		t.Fatal("the imported Provider credential did not reach its Skill")
	}
}

func TestTransferRollsBackGeneratedSkillChangesWhenEnvironmentWriteFails(t *testing.T) {
	core, home := multimodalInstallCore(t, &multimodalGateway{listing: gatewayListing})
	installSkill(t, core, "qwen/qwen3-vl-8b", modality.Vision, "claude-code", "codex")
	broken := filepath.Join(home, ".codex", "config.toml")
	if err := os.WriteFile(broken, []byte("invalid = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	registryBefore, err := os.ReadFile(core.skillStore().RegistryPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := core.ImportTransferV2(context.Background(), providerTransfer(t)); err == nil {
		t.Fatal("import reported success despite a failed Skill environment write")
	}
	entry, err := core.GetProvider(context.Background(), "gw")
	if err != nil || entry.BaseURL != "https://gw.example/v1" || entry.APIKey != multimodalKey {
		t.Fatal("the Provider was not rolled back")
	}
	for _, target := range []string{".claude", ".codex"} {
		cfg, err := skillgen.ReadConfig(filepath.Join(home, target, "skills", "bootagent-vision"))
		if err != nil || cfg.BaseURL != "https://gw.example/v1" {
			t.Fatal("a published Skill was not rolled back")
		}
	}
	if claudeEnv(t, home)["BOOTAGENT_GW_API_KEY"] != multimodalKey {
		t.Fatal("a successful environment update was not rolled back")
	}
	registryAfter, _ := os.ReadFile(core.skillStore().RegistryPath())
	if string(registryBefore) != string(registryAfter) {
		t.Fatal("the generated variant registry was not rolled back")
	}
}

func TestTransferNormalizedProviderIDKeepsGeneratedRollbackSnapshots(t *testing.T) {
	core, home := multimodalInstallCore(t, &multimodalGateway{listing: gatewayListing})
	installSkill(t, core, "qwen/qwen3-vl-8b", modality.Vision, "claude-code", "codex")
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("invalid = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(map[string]any{"providers": map[string]any{"providers": []provider.Entry{{ID: " gw ", Name: "Gateway", BaseURL: "https://new.example/v1", APIKey: "sk-rotated-key"}}}})
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := transfer.Build(map[string][]byte{"config.json": config})
	if err != nil {
		t.Fatal(err)
	}
	if err := core.ImportTransferV2(context.Background(), bundle); err == nil {
		t.Fatal("import reported success despite failed Skill environment refresh")
	}
	cfg, err := skillgen.ReadConfig(filepath.Join(home, ".claude", "skills", "bootagent-vision"))
	if err != nil || cfg.BaseURL != "https://gw.example/v1" || claudeEnv(t, home)["BOOTAGENT_GW_API_KEY"] != multimodalKey {
		t.Fatal("normalizing the imported ID skipped the generated Skill rollback snapshot")
	}
}

func TestProviderRefreshIgnoresUnrelatedStaleSkillFailures(t *testing.T) {
	core, home := jiekouCore(t, &multimodalGateway{listing: gatewayListing})
	ctx := context.Background()
	// The two Providers share both the guide pseudo-target and the native Agent.
	if result, err := core.InstallMultimodal(ctx, MultimodalInstallRequest{Provider: "gw", Model: "qwen/qwen3-vl-8b", Capability: modality.Vision, Agents: []string{"codex"}, Guide: true}); err != nil || !result.Results[0].EnvWritten {
		t.Fatalf("install = %#v, %v", result, err)
	}
	if result, err := core.InstallMultimodal(ctx, MultimodalInstallRequest{Provider: "jiekou", Model: "kling-v3.0-4k-t2v", Capability: modality.VideoGeneration, Agents: []string{"codex"}, Guide: true}); err != nil || !result.Results[0].EnvWritten {
		t.Fatalf("install = %#v, %v", result, err)
	}
	// Simulate an independently changed Provider without synchronizing its Skill.
	if _, err := core.providers.Save(ctx, provider.Entry{ID: "gw", Name: "Gateway", BaseURL: "https://old.example/v1", APIKey: multimodalKey}); err != nil {
		t.Fatal(err)
	}
	result, err := core.SaveProvider(ctx, provider.Entry{ID: "jiekou", Name: "JieKou.AI", BaseURL: "https://api.highwayapi.ai/openai", APIKey: "sk-rotated-key"}, false, false)
	if err != nil || len(result.Failures) != 0 {
		t.Fatalf("unrelated stale Skill blocked the selected Provider refresh: %#v, %v", result.Failures, err)
	}
	if codexSet(t, home)["BOOTAGENT_JIEKOU_API_KEY"] != "sk-rotated-key" {
		t.Fatal("the selected Provider key was not updated")
	}
}

func TestMultimodalInstallReportsBlockedFallbackDirectory(t *testing.T) {
	core, home := multimodalInstallCore(t, &multimodalGateway{listing: gatewayListing})
	if err := os.WriteFile(filepath.Join(home, ".bootagent", "skill-env"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := installSkill(t, core, "qwen/qwen3-vl-8b", modality.Vision, "opencode")
	if !result.Results[0].Installed || result.Results[0].EnvWritten || result.Results[0].Error == "" {
		t.Fatalf("fallback write failure was hidden: %#v", result)
	}
}

func TestFailedEndpointRefreshNeverWritesNewKeyToOldSkill(t *testing.T) {
	core, home := multimodalInstallCore(t, &multimodalGateway{listing: gatewayListing})
	installSkill(t, core, "qwen/qwen3-vl-8b", modality.Vision, "codex", "opencode")
	for _, target := range []string{".codex", ".config/opencode"} {
		path := filepath.Join(home, target, "skills", "bootagent-vision", "SKILL.md")
		if err := os.WriteFile(path, []byte("user edited this Skill"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := core.SaveProvider(context.Background(), provider.Entry{ID: "gw", Name: "Gateway", BaseURL: "https://new.example/v1", APIKey: "sk-rotated-key"}, false, false)
	if err != nil || len(result.Failures) == 0 {
		t.Fatalf("expected per-target refresh failures, got %v", err)
	}
	if codexSet(t, home)["BOOTAGENT_GW_API_KEY"] == "sk-rotated-key" {
		t.Fatal("the new credential was written for a Skill still using the old endpoint")
	}
	if data, _ := os.ReadFile(filepath.Join(home, ".bootagent", "skill-env", "gw.env")); strings.Contains(string(data), "sk-rotated-key") {
		t.Fatal("the new credential was written to the fallback for an old endpoint")
	}
}
