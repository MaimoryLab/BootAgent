package catalog

type Manifest struct {
	SchemaVersion   int              `json:"schema_version"`
	OneAgentVersion string           `json:"oneagent_version"`
	GeneratedAt     string           `json:"generated_at"`
	Agents          map[string]Agent `json:"agents"`
	AgentOrder      []string         `json:"-"`
}

type Agent struct {
	Name                 string            `json:"name"`
	Group                string            `json:"group"`
	Command              string            `json:"command"`
	ConfigMode           string            `json:"config_mode"`
	ConfigAdapter        string            `json:"config_adapter"`
	CredentialDelivery   string            `json:"credential_delivery"`
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
	Manager    string  `json:"manager"`
	Name       string  `json:"name"`
	Version    string  `json:"version"`
	Integrity  *string `json:"integrity"`
	Source     string  `json:"source"`
	License    string  `json:"license"`
	LicenseURL string  `json:"license_url"`
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
	fallbackModel    string
}

type Mirror struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Registry string `json:"registry"`
	Upstream string `json:"upstream"`
	Note     string `json:"note"`
}
