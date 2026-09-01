package catalog

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	bootagent "github.com/MaimoryLab/BootAgent"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
)

const MarketplaceSchemaVersion = 1

var (
	marketplaceOnce     sync.Once
	marketplaceManifest MarketplaceManifest
	marketplaceErr      error
)

type MarketplaceManifest struct {
	SchemaVersion int               `json:"schema_version"`
	Version       string            `json:"version"`
	BuiltAt       string            `json:"built_at"`
	Items         []MarketplaceItem `json:"items"`
}

type MarketplaceItem struct {
	ID               string   `json:"id"`
	Category         string   `json:"category"`
	Categories       []string `json:"categories,omitempty"`
	Type             string   `json:"type"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	DescriptionEn    string   `json:"descriptionEn,omitempty"`
	Icon             string   `json:"icon"`
	IconColor        string   `json:"iconColor"`
	Tags             []string `json:"tags,omitempty"`
	TagKeys          []string `json:"tagKeys,omitempty"`
	Capabilities     []string `json:"capabilities,omitempty"`
	Integrations     []string `json:"integrations,omitempty"`
	DeploymentModes  []string `json:"deploymentModes,omitempty"`
	TrustLevel       string   `json:"trustLevel,omitempty"`
	License          string   `json:"license,omitempty"`
	UpdatedAt        string   `json:"updatedAt,omitempty"`
	Scene            string   `json:"scene,omitempty"`
	Scenes           []string `json:"scenes,omitempty"`
	Source           string   `json:"source,omitempty"`
	RequiresAPIKey   bool     `json:"requiresApiKey,omitempty"`
	SourceLabel      string   `json:"sourceLabel,omitempty"`
	SourceURL        string   `json:"sourceUrl,omitempty"`
	RepositoryURL    string   `json:"repositoryUrl,omitempty"`
	DocumentationURL string   `json:"documentationUrl,omitempty"`
	InstallationURL  string   `json:"installationUrl,omitempty"`
	InstallableKind  string   `json:"installableKind,omitempty"`
	InstallPrompt    string   `json:"installPrompt,omitempty"`
	TargetHint       string   `json:"targetHint,omitempty"`
	ExternalURL      string   `json:"externalUrl,omitempty"`
	ReadmeURL        string   `json:"readmeUrl,omitempty"`
	IconURL          string   `json:"iconUrl,omitempty"`
	SocialPreviewURL string   `json:"socialPreviewUrl,omitempty"`
	Stars            int      `json:"stars,omitempty"`
	Downloads        int      `json:"downloads,omitempty"`
	Score            int      `json:"score,omitempty"`
	GitHubStars      int      `json:"githubStars,omitempty"`
	GitHubForks      int      `json:"githubForks,omitempty"`
	GitHubLicense    string   `json:"githubLicense,omitempty"`
	GitHubUpdatedAt  string   `json:"githubUpdatedAt,omitempty"`
}

func LoadEmbeddedMarketplace() (MarketplaceManifest, error) {
	marketplaceOnce.Do(func() {
		data, err := bootagent.EmbeddedMarketplaceLock()
		if err != nil {
			marketplaceErr = oneerrors.New(oneerrors.InvalidRequest, "Cannot load embedded marketplace lock manifest", oneerrors.WithCause(err))
			return
		}
		marketplaceManifest, marketplaceErr = ParseMarketplace(data)
	})
	if marketplaceErr != nil {
		return MarketplaceManifest{}, marketplaceErr
	}
	return cloneMarketplaceManifest(marketplaceManifest), nil
}

func ParseMarketplace(data []byte) (MarketplaceManifest, error) {
	var manifest MarketplaceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return MarketplaceManifest{}, oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Cannot load marketplace lock manifest: %v", err), oneerrors.WithCause(err))
	}
	if manifest.SchemaVersion != MarketplaceSchemaVersion || strings.TrimSpace(manifest.Version) == "" || strings.TrimSpace(manifest.BuiltAt) == "" {
		return MarketplaceManifest{}, oneerrors.New(oneerrors.InvalidRequest, "Unsupported marketplace lock manifest schema")
	}
	if len(manifest.Items) == 0 {
		return MarketplaceManifest{}, oneerrors.New(oneerrors.InvalidRequest, "Marketplace lock manifest has no items")
	}
	seen := make(map[string]struct{}, len(manifest.Items))
	for _, item := range manifest.Items {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Description) == "" || strings.TrimSpace(item.Icon) == "" || strings.TrimSpace(item.IconColor) == "" {
			return MarketplaceManifest{}, oneerrors.New(oneerrors.InvalidRequest, "Marketplace item is missing required metadata")
		}
		if _, ok := seen[item.ID]; ok {
			return MarketplaceManifest{}, oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Marketplace item %q is duplicated", item.ID))
		}
		seen[item.ID] = struct{}{}
		for field, value := range map[string]string{
			"sourceUrl": item.SourceURL, "repositoryUrl": item.RepositoryURL, "documentationUrl": item.DocumentationURL,
			"installationUrl": item.InstallationURL, "externalUrl": item.ExternalURL, "readmeUrl": item.ReadmeURL,
			"iconUrl": item.IconURL, "socialPreviewUrl": item.SocialPreviewURL,
		} {
			if value != "" && !httpsURL(value) {
				return MarketplaceManifest{}, oneerrors.New(oneerrors.InvalidRequest, fmt.Sprintf("Marketplace item %q has an invalid %s", item.ID, field))
			}
		}
	}
	return cloneMarketplaceManifest(manifest), nil
}

func cloneMarketplaceManifest(source MarketplaceManifest) MarketplaceManifest {
	result := source
	result.Items = make([]MarketplaceItem, len(source.Items))
	for index, item := range source.Items {
		result.Items[index] = item
		result.Items[index].Categories = append([]string(nil), item.Categories...)
		result.Items[index].Tags = append([]string(nil), item.Tags...)
		result.Items[index].TagKeys = append([]string(nil), item.TagKeys...)
		result.Items[index].Capabilities = append([]string(nil), item.Capabilities...)
		result.Items[index].Integrations = append([]string(nil), item.Integrations...)
		result.Items[index].DeploymentModes = append([]string(nil), item.DeploymentModes...)
		result.Items[index].Scenes = append([]string(nil), item.Scenes...)
	}
	return result
}
