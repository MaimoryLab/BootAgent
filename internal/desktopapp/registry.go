package desktopapp

import (
	"context"
	"fmt"
	"strings"
)

const (
	ChatGPTDesktopID       = "chatgpt-desktop"
	ChatGPTDesktopName     = "ChatGPT Desktop"
	CodexAgentID           = "codex"
	ConfigAdapterCodex     = "codex"
	ConfigAdapterWorkBuddy = "workbuddy"
)

// Definition contains the metadata shared by the desktop lifecycle and the
// profile/configuration UI. Installation behavior stays private to each agent.
type Definition struct {
	ID                  string
	Name                string
	ProfileAgentID      string
	SharedConfigAgentID string
	ConfigPath          string
	ConfigAdapter       string
	Protocol            string
}

type implementation struct {
	Definition
	inspect func(context.Context, Options) Status
	install func(context.Context, Options, bool) (ActionResult, error)
	open    func(context.Context, Options) error
}

var implementations = []implementation{
	{
		Definition: Definition{
			ID:                  ChatGPTDesktopID,
			Name:                ChatGPTDesktopName,
			ProfileAgentID:      CodexAgentID,
			SharedConfigAgentID: CodexAgentID,
			ConfigAdapter:       ConfigAdapterCodex,
		},
		inspect: inspectChatGPT,
		install: installChatGPT,
		open:    openChatGPT,
	},
	{
		Definition: Definition{
			ID:             WorkBuddyID,
			Name:           WorkBuddyName,
			ProfileAgentID: WorkBuddyID,
			ConfigPath:     ".workbuddy/models.json",
			ConfigAdapter:  ConfigAdapterWorkBuddy,
			Protocol:       "openai",
		},
		inspect: inspectWorkBuddy,
		install: installWorkBuddy,
		open:    openWorkBuddy,
	},
}

func Definitions() []Definition {
	result := make([]Definition, 0, len(implementations))
	for _, agent := range implementations {
		result = append(result, agent.Definition)
	}
	return result
}

func IDs() []string {
	result := make([]string, 0, len(implementations))
	for _, agent := range implementations {
		result = append(result, agent.ID)
	}
	return result
}

func DefinitionFor(agentID string) (Definition, bool) {
	agent, ok := implementationFor(agentID)
	if !ok {
		return Definition{}, false
	}
	return agent.Definition, true
}

func ProfileAgentID(agentID string) string {
	agentID = strings.TrimSpace(agentID)
	if definition, ok := DefinitionFor(agentID); ok && definition.ProfileAgentID != "" {
		return definition.ProfileAgentID
	}
	return agentID
}

func SharesProfile(agentID string) bool {
	agentID = strings.TrimSpace(agentID)
	definition, ok := DefinitionFor(agentID)
	return ok && definition.ProfileAgentID != "" && definition.ProfileAgentID != agentID
}

func implementationFor(agentID string) (implementation, bool) {
	agentID = strings.TrimSpace(agentID)
	for _, agent := range implementations {
		if agent.ID == agentID {
			return agent, true
		}
	}
	return implementation{}, false
}

func unknownAppStatus(agentID string) Status {
	agentID = strings.TrimSpace(agentID)
	message := fmt.Sprintf("unknown desktop agent %q", agentID)
	return Status{ID: agentID, Name: agentID, Source: SourceUnknown, InspectionUnavailable: &message}
}

func Inspect(ctx context.Context, agentID string, options Options) Status {
	agent, ok := implementationFor(agentID)
	if !ok {
		return unknownAppStatus(agentID)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return agent.inspect(ctx, options)
}

func Install(ctx context.Context, agentID string, options Options) (ActionResult, error) {
	agent, ok := implementationFor(agentID)
	if !ok {
		return ActionResult{}, fmt.Errorf("unknown desktop agent %q", strings.TrimSpace(agentID))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	return agent.install(ctx, options, false)
}

func OpenInstaller(ctx context.Context, agentID string, options Options) (ActionResult, error) {
	agent, ok := implementationFor(agentID)
	if !ok {
		return ActionResult{}, fmt.Errorf("unknown desktop agent %q", strings.TrimSpace(agentID))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return ActionResult{}, err
	}
	return agent.install(ctx, options, true)
}

func Open(ctx context.Context, agentID string, options Options) error {
	agent, ok := implementationFor(agentID)
	if !ok {
		return fmt.Errorf("unknown desktop agent %q", strings.TrimSpace(agentID))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	return agent.open(ctx, options)
}
