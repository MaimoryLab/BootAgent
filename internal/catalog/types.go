// Package catalog parses the embedded Agent and Provider catalog files and
// projects them for the desktop shell and the CLI. These hand-edited files keep
// runtime code from inventing package names, URLs, or models.
package catalog

type Manifest struct {
	SchemaVersion   int              `json:"schema_version"`
	OneAgentVersion string           `json:"oneagent_version"`
	GeneratedAt     string           `json:"generated_at"`
	Agents          map[string]Agent `json:"agents"`
	AgentOrder      []string         `json:"-"`
}

type ProviderManifest struct {
	SchemaVersion             int                 `json:"schema_version"`
	DefaultFallbackProbeModel string              `json:"default_fallback_probe_model"`
	Providers                 map[string]Provider `json:"providers"`
}

type Agent struct {
	Name                 string            `json:"name"`
	Group                string            `json:"group"`
	Command              string            `json:"command"`
	ConfigMode           string            `json:"config_mode"`
	ConfigAdapter        string            `json:"config_adapter"`
	EnvVars              map[string]string `json:"env_vars"`
	ConfigPath           string            `json:"config_path"`
	WindowsConfigPath    string            `json:"windows_config_path"`
	Package              *Package          `json:"package"`
	VersionArgs          []string          `json:"version_args"`
	Platforms            []string          `json:"platforms"`
	WindowsPrerequisites []string          `json:"windows_prerequisites"`
	WindowsNote          string            `json:"windows_note"`
	Guide                string            `json:"guide"`
	Rank                 int               `json:"rank"`
}

type Package struct {
	Manager    string `json:"manager"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	License    string `json:"license"`
	LicenseURL string `json:"license_url"`
}

type CatalogItem struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Group         string   `json:"group"`
	ConfigMode    string   `json:"configMode"`
	GuideOnly     bool     `json:"guideOnly"`
	LockedVersion *string  `json:"lockedVersion"`
	Protocol      *string  `json:"protocol"`
	Platforms     []string `json:"platforms"`
	PlatformNote  string   `json:"platformNote"`
	Rank          int      `json:"rank"`
}

type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Provider struct {
	Name             string `json:"name"`
	Home             string `json:"home"`
	BaseURL          string `json:"base_url"`
	AnthropicBaseURL string `json:"anthropic_base_url,omitempty"`
	Custom           bool   `json:"custom,omitempty"`
	HasKey           bool   `json:"has_key,omitempty"`
	fallbackModel    string
}

type Mirror struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Registry string `json:"registry"`
	Upstream string `json:"upstream"`
	Note     string `json:"note"`
}
