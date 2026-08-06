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
	"hermes":      ProtocolOpenAI,
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
		Note:     "npm 官方 registry，默认使用",
	},
	{
		ID:       "npmmirror",
		Name:     "npmmirror（阿里云）",
		Registry: "https://registry.npmmirror.com/",
		Upstream: officialNPMRegistry,
		Note:     "npm 官方源的公开只读镜像；官方源不可达时可用",
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
	return defaultFallbackProbeModel
}

// DefaultModel is the model a Provider suggests when the user has not chosen
// one. Unlike FallbackProbeModel there is no global default: a custom Provider
// is an endpoint we know nothing about, and guessing a model ID for it would
// produce a config that fails on first use. An empty result means the caller
// must ask.
func DefaultModel(providerID string) string {
	return providerDefinitions[providerID].DefaultModel
}

// KeyManagementURL is where a signed-in user creates an API key. Empty when the
// Provider publishes no such page, in which case callers fall back to Home.
func KeyManagementURL(providerID string) string {
	return providerDefinitions[providerID].KeyManagementURL
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
		var protocol *string
		if agent.ConfigMode == "auto" {
			value := ProtocolForAdapter(agent.ConfigAdapter)
			protocol = &value
		}
		platformNote := ""
		if platformID == "windows" {
			platformNote = agent.WindowsNote
		}
		packageManager, packageName := "", ""
		if agent.Package != nil {
			packageManager = agent.Package.Manager
			packageName = agent.Package.Name
		}
		items = append(items, CatalogItem{
			ID:             id,
			Name:           agent.Name,
			Group:          agent.Group,
			ConfigMode:     agent.ConfigMode,
			GuideOnly:      agent.ConfigMode == "guide",
			LockedVersion:  nil,
			Protocol:       protocol,
			Platforms:      append([]string(nil), agent.Platforms...),
			PlatformNote:   platformNote,
			Rank:           agent.Rank,
			PackageManager: packageManager,
			PackageName:    packageName,
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
