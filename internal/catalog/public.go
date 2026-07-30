package catalog

import "sort"

const (
	ProtocolOpenAI    = "openai"
	ProtocolAnthropic = "anthropic"
	ProtocolResponses = "responses"
)

var adapterProtocols = map[string]string{
	"codex":       ProtocolResponses,
	"claude-code": ProtocolAnthropic,
	"opencode":    ProtocolOpenAI,
	"kilo-cli":    ProtocolOpenAI,
	"aider":       ProtocolOpenAI,
}

var providerDefinitions = map[string]Provider{
	"ppio": {
		Name:             "PPIO",
		Home:             "https://ppio.com/",
		BaseURL:          "https://api.ppio.com/openai",
		AnthropicBaseURL: "https://api.ppio.com/anthropic",
		fallbackModel:    "deepseek/deepseek-v3",
	},
	"novita": {
		Name:             "Novita",
		Home:             "https://novita.ai/",
		BaseURL:          "https://api.novita.ai/openai",
		AnthropicBaseURL: "https://api.novita.ai/anthropic",
		fallbackModel:    "deepseek/deepseek_v3",
	},
}

var groups = []Group{
	{ID: "auto", Name: "One-click configurable"},
	{ID: "gateway", Name: "Gateway agents"},
	{ID: "platform", Name: "Official account agents"},
	{ID: "ide", Name: "IDE extensions"},
}

const officialNPMRegistry = "https://registry.npmjs.org/"

var mirrors = []Mirror{
	{
		ID:       "official",
		Name:     "官方源",
		Registry: officialNPMRegistry,
		Upstream: officialNPMRegistry,
		Note:     "npm 官方 registry，默认使用。",
	},
	{
		ID:       "npmmirror",
		Name:     "npmmirror（阿里云）",
		Registry: "https://registry.npmmirror.com/",
		Upstream: officialNPMRegistry,
		Note:     "官方源的公开只读镜像，包体与校验值均与官方一致；官方源不可达时可用。",
	},
}

func ProtocolForAdapter(adapter string) string {
	if protocol, ok := adapterProtocols[adapter]; ok {
		return protocol
	}
	return ProtocolOpenAI
}

func FallbackProbeModel(providerID string) string {
	if provider, ok := providerDefinitions[providerID]; ok {
		return provider.fallbackModel
	}
	return "deepseek/deepseek-v3"
}

func PublicProviders() map[string]Provider {
	result := make(map[string]Provider, len(providerDefinitions))
	for id, provider := range providerDefinitions {
		// fallbackModel is unexported and therefore cannot leak through JSON,
		// but clear it here as an additional boundary for non-JSON callers.
		provider.fallbackModel = ""
		result[id] = provider
	}
	return result
}

func ProviderByID(providerID string) (Provider, bool) {
	provider, ok := providerDefinitions[providerID]
	if !ok {
		return Provider{}, false
	}
	provider.fallbackModel = ""
	return provider, true
}

func Groups() []Group {
	return append([]Group(nil), groups...)
}

func Mirrors() []Mirror {
	return append([]Mirror(nil), mirrors...)
}

func PublicCatalog(manifest Manifest, platformID string) []CatalogItem {
	items := make([]CatalogItem, 0, len(manifest.Agents))
	for id, agent := range manifest.Agents {
		var lockedVersion *string
		if agent.Package != nil {
			version := agent.Package.Version
			lockedVersion = &version
		}
		var protocol *string
		if agent.ConfigMode == "auto" {
			value := ProtocolForAdapter(agent.ConfigAdapter)
			protocol = &value
		}
		platformNote := ""
		if platformID == "windows" {
			platformNote = agent.WindowsNote
		}
		items = append(items, CatalogItem{
			ID:            id,
			Name:          agent.Name,
			Group:         agent.Group,
			ConfigMode:    agent.ConfigMode,
			GuideOnly:     agent.ConfigMode == "guide",
			LockedVersion: lockedVersion,
			Protocol:      protocol,
			Platforms:     append([]string(nil), agent.Platforms...),
			PlatformNote:  platformNote,
			Rank:          agent.Rank,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Rank != items[j].Rank {
			return items[i].Rank < items[j].Rank
		}
		return items[i].ID < items[j].ID
	})
	return items
}
