// Package catalog parses the embedded Agent and Provider catalog files and
// projects them for the desktop application. These hand-edited files keep
// runtime code from inventing package names, URLs, or models.
package catalog

type Manifest struct {
	SchemaVersion    int              `json:"schema_version"`
	BootAgentVersion string           `json:"bootagent_version"`
	GeneratedAt      string           `json:"generated_at"`
	Agents           map[string]Agent `json:"agents"`
	AgentOrder       []string         `json:"-"`
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
	MCPAdapter           string            `json:"mcp_adapter,omitempty"`
	MCPSection           string            `json:"mcp_section,omitempty"`
	MCPConfigPath        string            `json:"mcp_config_path,omitempty"`
	MCPWindowsConfigPath string            `json:"mcp_windows_config_path,omitempty"`
	SkillsPath           string            `json:"skills_path,omitempty"`
	SkillsWindowsPath    string            `json:"skills_windows_path,omitempty"`
	// ModelSelection says who picks the model. "provider" (the default, and what
	// an empty value means) is BootAgent's own flow: the wizard offers the
	// Provider's models and writes the choice into the Agent's config. "agent"
	// means the Agent owns the decision and has nowhere for us to put it, so the
	// wizard skips the model step instead of collecting an answer it would drop
	// on the floor. Named rather than a bool because the wizard has to *explain*
	// the skip, and "the Agent chooses" is the explanation.
	ModelSelection       string   `json:"model_selection,omitempty"`
	Package              *Package `json:"package"`
	VersionArgs          []string `json:"version_args"`
	Platforms            []string `json:"platforms"`
	WindowsPrerequisites []string `json:"windows_prerequisites"`
	WindowsNote          string   `json:"windows_note"`
	Guide                string   `json:"guide"`
	Rank                 int      `json:"rank"`
}

type Package struct {
	Manager               string `json:"manager"`
	Name                  string `json:"name"`
	Source                string `json:"source"`
	License               string `json:"license"`
	LicenseURL            string `json:"license_url"`
	InstallCommand        string `json:"install_command"`
	WindowsInstallCommand string `json:"windows_install_command"`
}

type CatalogItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Group      string `json:"group"`
	ConfigMode string `json:"configMode"`
	// SelectsModel is false when the Agent owns its own model choice, so the
	// wizard can skip that step. Derived from Agent.ModelSelection rather than
	// exposed raw: the UI needs the decision, not the vocabulary, and a bool it
	// cannot misread beats a string it can compare against the wrong literal.
	SelectsModel  bool     `json:"selectsModel"`
	GuideOnly     bool     `json:"guideOnly"`
	LockedVersion *string  `json:"lockedVersion"`
	Protocol      *string  `json:"protocol"`
	Platforms     []string `json:"platforms"`
	PlatformNote  string   `json:"platformNote"`
	Rank          int      `json:"rank"`
	// PackageManager and PackageName describe how this Agent is installed, so the
	// UI can decide whether to offer an update without keeping its own list of
	// npm Agents. That list existed and had already drifted: OpenClaw shipped as
	// an npm Agent and was missing from it, so it silently lost its update
	// button. Empty means the Agent has no package contract.
	PackageManager string `json:"packageManager,omitempty"`
	PackageName    string `json:"packageName,omitempty"`
}

type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Provider struct {
	Name string `json:"name"`
	Home string `json:"home"`
	// KeyManagementURL and DefaultModel are public on purpose: the frontend
	// needs both to spare a first-time user from hunting for a key page and
	// inventing a model ID. Contrast fallbackModel below.
	KeyManagementURL string `json:"key_management_url,omitempty"`
	BaseURL          string `json:"base_url"`
	AnthropicBaseURL string `json:"anthropic_base_url,omitempty"`
	DefaultModel     string `json:"default_model,omitempty"`
	// Display precedence among built-in Providers, ascending. Absent for a
	// custom Provider, which the frontend sorts by creation time instead.
	Order     int    `json:"order,omitempty"`
	Custom    bool   `json:"custom,omitempty"`
	HasKey    bool   `json:"has_key,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	// Unexported, and cleared again by PublicProviders: which model we probe
	// with is an internal implementation detail, not a recommendation.
	fallbackModel string
}

type Mirror struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Registry string `json:"registry"`
	Upstream string `json:"upstream"`
	Note     string `json:"note"`
}
