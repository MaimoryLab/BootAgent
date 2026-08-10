package binding

import (
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// extractableArchiveSuffixes are the formats the updater unpacks before the
// helper swaps the result over the installed application. The list mirrors
// updater.detectArchive; anything absent from it is handed to the helper
// verbatim, which for a container format means the container file itself
// replaces the application.
var extractableArchiveSuffixes = []string{".zip", ".tar.gz", ".tgz"}

// containerSuffixes never name something the helper can swap into place. A
// staged artifact still carrying one of these did not get unpacked, so
// installing it would overwrite the application with the container.
var containerSuffixes = []string{
	".zip", ".tar.gz", ".tgz", // an archive that survived extraction
	".dmg", ".pkg", ".msi", ".deb", ".rpm", ".appimage", // never extracted at all
}

func hasAnySuffix(name string, suffixes []string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range suffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// ExtractableAssetMatcher picks a release asset the updater can actually
// unpack, then defers to the upstream matcher for platform and architecture.
//
// The default matcher takes the first asset whose name contains both the
// platform and the architecture, and the GitHub API returns assets in
// alphabetical order -- so "OneAgent-darwin-arm64.dmg" wins over the sibling
// ".zip". A .dmg is not an archive the updater recognises, so it reached the
// helper unextracted and replaced the installed OneAgent.app with the disk
// image file. Filtering first keeps the upstream sidecar and architecture
// handling intact while making that outcome unreachable.
func ExtractableAssetMatcher(request updater.CheckRequest, assets []github.ReleaseAsset) int {
	candidates := make([]github.ReleaseAsset, 0, len(assets))
	origin := make([]int, 0, len(assets))
	for index, asset := range assets {
		if !hasAnySuffix(asset.Name, extractableArchiveSuffixes) {
			continue
		}
		candidates = append(candidates, asset)
		origin = append(origin, index)
	}
	picked := github.DefaultAssetMatcher(request, candidates)
	if picked < 0 {
		return -1
	}
	return origin[picked]
}

// stagedArtifactError reports a staged update that must not be installed. It
// is returned before the user is offered a restart, because the swap happens
// after this process exits and there is no interface left to report it.
func stagedArtifactError(path string) error {
	if path == "" {
		return nil
	}
	if !hasAnySuffix(path, containerSuffixes) {
		return nil
	}
	return &unusableArtifactError{name: filepath.Base(path)}
}

type unusableArtifactError struct {
	name string
}

func (e *unusableArtifactError) Error() string {
	return "updater staged " + e.name + ", which is not an installable application"
}
