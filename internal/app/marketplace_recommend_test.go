package app

import (
	"context"
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
	if !strings.Contains(runner.input, `"skill-safe"`) {
		t.Fatalf("stdin did not contain the bounded knowledge payload: %q", runner.input)
	}
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
