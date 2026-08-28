package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

type marketplaceRecommendationRunner struct {
	available map[string]bool
	result    process.Result
	argv      []string
	input     string
}

func (r *marketplaceRecommendationRunner) LookPath(command string) (string, bool) {
	return command, r.available[command]
}

func (r *marketplaceRecommendationRunner) Run(_ context.Context, argv []string, _ map[string]string, timeout time.Duration) (process.Result, error) {
	r.argv = append([]string(nil), argv...)
	if timeout <= 0 || timeout > 2*time.Minute {
		return process.Result{}, context.DeadlineExceeded
	}
	return r.result, nil
}

func (r *marketplaceRecommendationRunner) RunPrivateInput(_ context.Context, argv []string, _ map[string]string, timeout time.Duration, input string) (process.Result, error) {
	r.argv = append([]string(nil), argv...)
	r.input = input
	if timeout <= 0 || timeout > 2*time.Minute {
		return process.Result{}, context.DeadlineExceeded
	}
	return r.result, nil
}

func recommendationCore(t *testing.T, runner *marketplaceRecommendationRunner) *UseCases {
	t.Helper()
	return NewUseCases(StatusOptions{
		Home:        t.TempDir(),
		Platform:    platform.For("linux", "amd64"),
		Runner:      runner,
		Environment: map[string]string{"PATH": "/usr/bin"},
	})
}

func TestMarketplaceRecommendationAgentsOnlyListsSupportedInstalledCLIs(t *testing.T) {
	runner := &marketplaceRecommendationRunner{available: map[string]bool{
		"codex": true, "claude": true, "pi": true, "opencode": true,
	}}
	agents, err := recommendationCore(t, runner).MarketplaceRecommendationAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents[0].ID != "codex" || agents[1].ID != "claude-code" {
		t.Fatalf("agents = %#v", agents)
	}
}

func TestRecommendMarketplaceValidatesModelOutputAgainstKnowledgeIDs(t *testing.T) {
	runner := &marketplaceRecommendationRunner{
		available: map[string]bool{"codex": true},
		result: process.Result{ExitCode: 0, Stdout: `{"recommendations":[
			{"item_id":"skill-safe","reason":"适合整理项目知识"},
			{"item_id":"not-in-catalog","reason":"ignore me"}
		]}`},
	}
	core := recommendationCore(t, runner)
	result, err := core.RecommendMarketplace(context.Background(), MarketplaceRecommendRequest{
		AgentID: "codex",
		Need:    "我需要整理项目知识",
		Locale:  "zh-CN",
		Items: []MarketplaceKnowledgeItem{{
			ID: "skill-safe", Name: "Safe Skill", Description: "整理知识", Category: "skill", Tags: []string{"知识"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Recommendations) != 1 || result.Recommendations[0].ItemID != "skill-safe" {
		t.Fatalf("recommendations = %#v", result.Recommendations)
	}
	if len(runner.argv) < 2 || runner.argv[0] != "codex" || strings.Contains(strings.Join(runner.argv, " "), "skill-safe") {
		t.Fatalf("private knowledge leaked into argv: %#v", runner.argv)
	}
	workingDirectory := argumentValue(runner.argv, "-C")
	if workingDirectory == "" {
		t.Fatalf("Codex recommendation did not use an isolated working directory: %#v", runner.argv)
	}
	if _, err := os.Stat(workingDirectory); !os.IsNotExist(err) {
		t.Fatalf("temporary recommendation directory was not removed: %q (%v)", workingDirectory, err)
	}
	if !strings.Contains(runner.input, `"skill-safe"`) {
		t.Fatalf("stdin did not contain the bounded knowledge payload: %q", runner.input)
	}
}

func TestRecommendMarketplaceExecutesResolvedAgentPath(t *testing.T) {
	runner := &marketplaceRecommendationRunner{
		available: map[string]bool{"codex": true},
		result:    process.Result{ExitCode: 0, Stdout: `{"recommendations":[{"item_id":"skill-safe","reason":"matches"}]}`},
	}
	core := recommendationCore(t, runner)
	// The fake normally returns the command name; make the lookup return an
	// absolute path to model a desktop PATH that is only available at discovery.
	runnerPath := "/private/runtime/bin/codex"
	runner.available = map[string]bool{"codex": true}
	// A path-aware runner is used below so the assertion covers argv[0].
	pathRunner := &marketplaceRecommendationRunnerWithPath{marketplaceRecommendationRunner: *runner, path: runnerPath}
	core = recommendationCoreWithRunner(t, pathRunner)
	if _, err := core.RecommendMarketplace(context.Background(), MarketplaceRecommendRequest{
		AgentID: "codex", Need: "find a tool", Items: []MarketplaceKnowledgeItem{{ID: "skill-safe", Name: "Safe", Description: "Useful", Category: "skill"}},
	}); err != nil {
		t.Fatal(err)
	}
	if pathRunner.argv[0] != runnerPath {
		t.Fatalf("argv[0] = %q, want resolved executable %q", pathRunner.argv[0], runnerPath)
	}
}

type marketplaceRecommendationRunnerWithPath struct {
	marketplaceRecommendationRunner
	path string
}

func (r *marketplaceRecommendationRunnerWithPath) LookPath(command string) (string, bool) {
	return r.path, r.available[command]
}

func recommendationCoreWithRunner(t *testing.T, runner process.Runner) *UseCases {
	t.Helper()
	return NewUseCases(StatusOptions{Home: t.TempDir(), Platform: platform.For("linux", "amd64"), Runner: runner, Environment: map[string]string{"PATH": "/usr/bin"}})
}

func argumentValue(argv []string, name string) string {
	for index := 0; index+1 < len(argv); index++ {
		if argv[index] == name {
			return argv[index+1]
		}
	}
	return ""
}

func TestRecommendMarketplaceRejectsOversizedOrUnknownRequests(t *testing.T) {
	runner := &marketplaceRecommendationRunner{available: map[string]bool{"codex": true}}
	core := recommendationCore(t, runner)
	for _, request := range []MarketplaceRecommendRequest{
		{AgentID: "opencode", Need: "recommend", Items: []MarketplaceKnowledgeItem{{ID: "a", Name: "A", Description: "A", Category: "skill"}}},
		{AgentID: "codex", Need: strings.Repeat("x", marketplaceRecommendationNeedMax+1), Items: []MarketplaceKnowledgeItem{{ID: "a", Name: "A", Description: "A", Category: "skill"}}},
		{AgentID: "codex", Need: "recommend", Items: []MarketplaceKnowledgeItem{{ID: "../escape", Name: "A", Description: "A", Category: "skill"}}},
	} {
		if _, err := core.RecommendMarketplace(context.Background(), request); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
			t.Fatalf("request %#v was not rejected: %v", request, err)
		}
	}
}

func TestRecommendMarketplaceTruncatesLongCatalogDescriptions(t *testing.T) {
	runner := &marketplaceRecommendationRunner{
		available: map[string]bool{"codex": true},
		result:    process.Result{ExitCode: 0, Stdout: `{"recommendations":[{"item_id":"long-doc","reason":"matches"}]}`},
	}
	result, err := recommendationCore(t, runner).RecommendMarketplace(context.Background(), MarketplaceRecommendRequest{
		AgentID: "codex",
		Need:    "find a tool",
		Items: []MarketplaceKnowledgeItem{{
			ID: "long-doc", Name: "Long document", Description: strings.Repeat("说明", marketplaceRecommendationFieldMax+100), Category: "skill",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Recommendations) != 1 || result.Recommendations[0].ItemID != "long-doc" {
		t.Fatalf("recommendations = %#v", result.Recommendations)
	}
	if strings.Contains(runner.input, strings.Repeat("说明", marketplaceRecommendationFieldMax+1)) {
		t.Fatal("long catalog description was not bounded")
	}
}

func TestMarketplaceRecommendationHistoryPersistsAndManagesRecords(t *testing.T) {
	home := t.TempDir()
	core := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Runner: &marketplaceRecommendationRunner{}})
	first, err := core.SaveMarketplaceRecommendationHistory(context.Background(), MarketplaceRecommendationHistory{
		AgentID: "codex", Need: "远程管理 Agent", CatalogVersion: "2.3.0",
		Results: []MarketplaceRecommendationSnapshot{{ItemID: "github-remote", Name: "Remote Agent", Reason: "支持远程管理", Category: "ai-product", Source: "github"}},
	})
	if err != nil || !strings.HasPrefix(first.ID, "rec_") || first.CreatedAt == "" {
		t.Fatalf("saved history = %#v, err = %v", first, err)
	}
	reloaded := NewUseCases(StatusOptions{Home: home, Platform: platform.For("linux", "amd64"), Runner: &marketplaceRecommendationRunner{}})
	records, err := reloaded.ListMarketplaceRecommendationHistory(context.Background())
	if err != nil || len(records) != 1 || records[0].ID != first.ID {
		t.Fatalf("loaded history = %#v, err = %v", records, err)
	}
	if err := reloaded.DeleteMarketplaceRecommendationHistory(context.Background(), first.ID); err != nil {
		t.Fatal(err)
	}
	records, err = reloaded.ListMarketplaceRecommendationHistory(context.Background())
	if err != nil || len(records) != 0 {
		t.Fatalf("after delete = %#v, err = %v", records, err)
	}
	if err := reloaded.ClearMarketplaceRecommendationHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
}
