package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
)

// LatestVersionTimeout bounds one registry lookup. Short on purpose: this
// answers a decoration in the UI, so a slow registry must not hold up the
// status call that carries it.
const LatestVersionTimeout = 6 * time.Second

// LatestVersion reports the newest published version of an Agent's package, or
// "" when the answer is not knowable. An empty string is not an error the user
// needs to see: the update dot is an affordance, and a registry that is
// unreachable, rate-limiting, or behind a captive portal should leave the UI
// exactly as it was rather than surfacing a failure for something nobody asked
// for.
//
// Only npm packages are supported. Aider is installed with uv from PyPI, whose
// metadata shape is different, and the desktop Agents are not packages at all.
func LatestVersion(ctx context.Context, client Doer, agent catalog.Agent, registry string) string {
	if client == nil || agent.Package == nil || agent.Package.Manager != "npm" {
		return ""
	}
	endpoint, err := packageEndpoint(registry, agent.Package.Name)
	if err != nil {
		return ""
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ""
	}
	// The abbreviated document is a fraction of the full packument -- the full
	// one carries every version's metadata, which for a package like
	// @anthropic-ai/claude-code is megabytes to answer a single string.
	request.Header.Set("Accept", "application/vnd.npm.install-v1+json")
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	latest := strings.TrimSpace(decodeLatestTag(response.Body))
	// Validate before returning: this value reaches the UI and is compared
	// against the installed version, and a registry is not something to trust
	// with arbitrary strings. ValidateVersion accepts "" as "unspecified", which
	// is meaningful for an install request but not for an answer, so the empty
	// case is rejected here rather than left to it.
	if latest == "" || ValidateVersion(latest) != nil {
		return ""
	}
	return latest
}

// decodeLatestTag streams the document and stops at dist-tags.latest, returning
// "" if it is not there.
//
// Decoding the whole object into a struct is the obvious approach and does not
// work: even the abbreviated document is 13 MB for opencode-ai, because it
// carries an entry per published version. Reading all of it to answer one string
// meant either a size cap that truncated the JSON -- failing with unexpected EOF
// while the field sat 24 bytes in -- or no cap at all, letting a registry decide
// how much memory this spends. Streaming needs neither: dist-tags appears near
// the front, and the rest is never read.
func decodeLatestTag(body io.Reader) string {
	// The cap still bounds a registry that sends an endless stream with no
	// dist-tags in it; it is generous because it now only has to cover the
	// distance to that key, not the whole document.
	decoder := json.NewDecoder(io.LimitReader(body, 8<<20))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return ""
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return ""
		}
		name, _ := key.(string)
		if name != "dist-tags" {
			// Skip the value without materialising it: json.RawMessage would hold the
			// entire versions map, which is the size problem all over again.
			if err := skipValue(decoder); err != nil {
				return ""
			}
			continue
		}
		var tags map[string]string
		if err := decoder.Decode(&tags); err != nil {
			return ""
		}
		return tags["latest"]
	}
	return ""
}

// skipValue consumes the next value, descending through objects and arrays.
func skipValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if _, isDelimiter := token.(json.Delim); !isDelimiter {
		// A scalar is one token and is now consumed.
		return nil
	}
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'), json.Delim('['):
			depth++
		case json.Delim('}'), json.Delim(']'):
			depth--
		}
	}
	return nil
}

// packageEndpoint joins a package name onto a registry root. Scoped names keep
// their slash unescaped, which is what registries serve, so the path is built
// rather than delegated to url.JoinPath.
func packageEndpoint(registry, name string) (string, error) {
	root := strings.TrimSpace(registry)
	if root == "" {
		root = officialRegistry()
	}
	parsed, err := url.Parse(root)
	if err != nil || parsed.Scheme != "https" {
		// Plain HTTP would expose which Agents a user has installed to anyone on
		// the path, and a mirror that only speaks HTTP is not worth that.
		return "", fmt.Errorf("registry must be an https URL: %q", registry)
	}
	packageName := strings.TrimSpace(name)
	if packageName == "" || strings.Contains(packageName, "..") {
		return "", fmt.Errorf("invalid package name: %q", name)
	}
	return strings.TrimSuffix(parsed.String(), "/") + "/" + packageName, nil
}
