package catalog

// Provider is a managed inference provider. FallbackProbeModel is internal:
// sending this struct wholesale to the frontend leaked it the moment it was
// added, so PublicProviders projects the public fields explicitly instead.
type Provider struct {
	Name string
	Home string
	// BaseURL is the OpenAI-compatible route.
	BaseURL string
	// AnthropicBaseURL is a separate route on the managed providers, which is
	// why the config write is keyed on the protocol rather than on an Agent id.
	AnthropicBaseURL string
	// FallbackProbeModel is the last resort when the endpoint's model list
	// cannot be fetched. It must still exist on this provider, and the IDs
	// differ per provider -- PPIO publishes deepseek-v3 while Novita publishes
	// deepseek_v3 -- so this cannot be one shared constant.
	FallbackProbeModel string
}

// Providers is the managed set. "custom" is not here: it has no fixed URL, and
// treating it as an entry would invite code to look up a base it must validate.
var Providers = map[string]Provider{
	"ppio": {
		Name:               "PPIO",
		Home:               "https://ppio.com/",
		BaseURL:            "https://api.ppio.com/openai",
		AnthropicBaseURL:   "https://api.ppio.com/anthropic",
		FallbackProbeModel: "deepseek/deepseek-v3",
	},
	"novita": {
		Name:               "Novita",
		Home:               "https://novita.ai/",
		BaseURL:            "https://api.novita.ai/openai",
		AnthropicBaseURL:   "https://api.novita.ai/anthropic",
		FallbackProbeModel: "deepseek/deepseek_v3",
	},
}

// DefaultFallbackProbeModel is what a custom endpoint gets: the most widely
// published ID, as a best guess. The user can probe again after choosing a real
// model, so a wrong guess costs one round trip rather than blocking the wizard.
const DefaultFallbackProbeModel = "deepseek/deepseek-v3"

// FallbackProbeModel is the last-resort probe model for a provider. Normal
// probes resolve a live model from the endpoint's own list; this covers only the
// narrow path where that discovery fails.
func FallbackProbeModel(provider string) string {
	if meta, known := Providers[provider]; known {
		return meta.FallbackProbeModel
	}
	return DefaultFallbackProbeModel
}

// PublicProvider is the provider shape a client may see.
type PublicProvider struct {
	Name             string `json:"name"`
	Home             string `json:"home"`
	BaseURL          string `json:"base_url"`
	AnthropicBaseURL string `json:"anthropic_base_url,omitempty"`
}

// PublicProviders projects the fields the API exposes.
//
// Explicit projection rather than serialising Providers: the struct also holds
// the fallback probe model, and sending it wholesale put an internal decision
// into the status payload the moment one was added.
func PublicProviders() map[string]PublicProvider {
	public := make(map[string]PublicProvider, len(Providers))
	for id, meta := range Providers {
		public[id] = PublicProvider{
			Name:             meta.Name,
			Home:             meta.Home,
			BaseURL:          meta.BaseURL,
			AnthropicBaseURL: meta.AnthropicBaseURL,
		}
	}
	return public
}

// OfficialNpmRegistry is the default, and the upstream every mirror must name.
const OfficialNpmRegistry = "https://registry.npmjs.org/"

// Mirror is a package registry a user may install from when the official one is
// not reachable.
//
// The product boundary allows an authorised mirror (priority 2 in the
// software-acquisition policy) provided it carries a licence, a pinned version,
// a checksum and the upstream address -- so each entry records its upstream, and
// the install path verifies the manifest's integrity value against whatever the
// mirror served.
//
// This changes where a package is fetched from, not how the user reaches the
// network: OneAgent opens no tunnel and forwards no traffic, which is what
// separates it from the proxying the boundary forbids. Nothing here may point at
// storage OneAgent operates -- redistributing a proprietary Agent needs a
// licence that pointing at a public read-only mirror does not.
type Mirror struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Registry string `json:"registry"`
	Upstream string `json:"upstream"`
	Note     string `json:"note"`
}

// mirrorOrder fixes the order the UI shows, with the official source first.
// Deriving it from a map would let the default move between builds.
var mirrorOrder = []string{"official", "npmmirror"}

var mirrors = map[string]Mirror{
	"official": {
		ID:       "official",
		Name:     "官方源",
		Registry: OfficialNpmRegistry,
		Upstream: OfficialNpmRegistry,
		Note:     "npm 官方 registry，默认使用。",
	},
	"npmmirror": {
		ID:       "npmmirror",
		Name:     "npmmirror（阿里云）",
		Registry: "https://registry.npmmirror.com/",
		Upstream: OfficialNpmRegistry,
		Note:     "官方源的公开只读镜像，包体与校验值均与官方一致；官方源不可达时可用。",
	},
}

// Mirror returns one registry choice by id.
func MirrorByID(id string) (Mirror, bool) {
	mirror, present := mirrors[id]
	return mirror, present
}

// PublicMirrors lists the registry choices for the UI, upstream included so the
// origin of a package stays visible to the user rather than implied.
func PublicMirrors() []Mirror {
	listed := make([]Mirror, 0, len(mirrorOrder))
	for _, id := range mirrorOrder {
		listed = append(listed, mirrors[id])
	}
	return listed
}
