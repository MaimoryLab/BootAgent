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
	ConfigAdapterZCode     = "zcode"

	// Edition labels a regional build of a product that ships as two separate
	// applications. It exists so the UI can tell them apart without the region
	// being folded into Name, which is also what error messages and logs use.
	// Empty means the product has only one build.
	EditionChina         = "cn"
	EditionInternational = "intl"
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
	// ManualInstall marks an app BootAgent can detect and configure but not fetch,
	// because no verifiable installer URL is known for it. Home is where the user
	// gets it instead. Both exist so the UI can avoid offering an install action
	// that only returns a link.
	ManualInstall bool
	Home          string
	// Edition distinguishes regional builds of the same product, which install
	// side by side and are separate Agents here. Empty for single-build products.
	Edition string
}

type implementation struct {
	Definition
	inspect func(context.Context, Options) Status
	install func(context.Context, Options) (ActionResult, error)
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
			Edition:        EditionChina,
		},
		inspect: inspectWorkBuddy(workBuddyCN),
		install: installWorkBuddy(workBuddyCN),
		open:    openWorkBuddy(workBuddyCN),
	},
	{
		Definition: Definition{
			ID:             WorkBuddyIntlID,
			Name:           WorkBuddyIntlName,
			ProfileAgentID: WorkBuddyIntlID,
			// Not ~/.codebuddy, which is what the vendor's English documentation
			// says: the shipped build resolves this from customUserDataDir in its
			// own cli/product.json. See WorkBuddyIntlConfigDir.
			ConfigPath:    WorkBuddyIntlConfigDir + "/models.json",
			ConfigAdapter: ConfigAdapterWorkBuddy,
			Protocol:      "openai",
			Edition:       EditionInternational,
		},
		inspect: inspectWorkBuddy(workBuddyIntl),
		install: installWorkBuddy(workBuddyIntl),
		open:    openWorkBuddy(workBuddyIntl),
	},
	{
		Definition: Definition{
			ID:             ZCodeID,
			Name:           ZCodeName,
			ProfileAgentID: ZCodeID,
			ConfigPath:     ".zcode/v2/config.json",
			ConfigAdapter:  ConfigAdapterZCode,
			// ZCode accepts either protocol per provider entry, and its own builtin
			// entries use anthropic. openai is pinned here because that is the
			// protocol every Provider in providers.lock.json serves, while only
			// some serve Anthropic.
			Protocol: "openai",
			Home:     ZCodeHome,
		},
		inspect: inspectZCode,
		install: installZCode,
		open:    openZCode,
	},
}

func Definitions() []Definition {
	result := make([]Definition, 0, len(implementations))
	for _, agent := range implementations {
		result = append(result, agent.Definition)
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
	return agent.install(ctx, options)
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
