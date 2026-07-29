// Package provider derives and validates the endpoints OneAgent writes into
// Agent configuration.
//
// Everything here is pure: no network, no filesystem. The probes that do reach
// an endpoint are built on top of these, so a URL bug surfaces in a test rather
// than as a confusing HTTP failure.
package provider

import (
	"net/url"
	"strings"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
)

// CustomProvider is the id meaning "the user supplied the endpoint". It is not
// in catalog.Providers because it has no fixed URL.
const CustomProvider = "custom"

// ValidateBaseURL checks a user-supplied endpoint and returns it without the
// trailing slash.
//
// The rejections are not stylistic. A control character can split a header or a
// config line; credentials in a URL would be written to a config file in plain
// text and would appear in any log that echoed the endpoint.
func ValidateBaseURL(value string) (string, error) {
	if value == "" {
		return "", oerr.New("INVALID_REQUEST", "Custom base URL is required")
	}
	for _, char := range value {
		if char < 32 || char == 127 {
			return "", oerr.New("INVALID_REQUEST", "Custom base URL contains control characters")
		}
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", oerr.New("INVALID_REQUEST", "Custom base URL must start with http:// or https://")
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", oerr.New("INVALID_REQUEST", "Custom base URL must start with http:// or https://")
	}
	if HasCredentials(parsed) {
		return "", oerr.New("INVALID_REQUEST", "Custom base URL must not contain credentials")
	}
	return strings.TrimRight(value, "/"), nil
}

// HasCredentials reports whether a URL carries a username or password.
//
// Not `parsed.User != nil`: Go builds a Userinfo for an empty userinfo section,
// so "https://@host/" and "https://:@host/" would be refused, while Python tests
// the truthiness of username and password and accepts both. Neither URL carries a
// credential, so Python is right and the difference is only in how the two
// parsers represent "present but empty".
//
// Shared with the registry check rather than duplicated, so one corpus covers
// both call sites.
func HasCredentials(parsed *url.URL) bool {
	if parsed.User == nil {
		return false
	}
	if parsed.User.Username() != "" {
		return true
	}
	password, _ := parsed.User.Password()
	return password != ""
}

// Base resolves the OpenAI-compatible endpoint for a provider. A custom base
// always wins, so a user can point a managed provider elsewhere.
func Base(provider, customBase string) (string, error) {
	if _, known := catalog.Providers[provider]; !known && provider != CustomProvider {
		return "", oerr.New("INVALID_REQUEST", "Provider must be ppio, novita, or custom")
	}
	if customBase != "" {
		return ValidateBaseURL(customBase)
	}
	if provider == CustomProvider {
		// Reported as a missing URL rather than an unknown provider: the
		// provider is fine, the endpoint is what is absent.
		return ValidateBaseURL("")
	}
	return catalog.Providers[provider].BaseURL, nil
}

// ConfigBase is the endpoint a config write or probe targets for one protocol.
//
// Anthropic-speaking Agents are served from a separate route on the managed
// providers, so the decision is keyed on the protocol -- which callers derive
// from the Agent's adapter -- rather than on an Agent id. A custom base or a
// non-Anthropic protocol keeps the OpenAI-compatible URL.
func ConfigBase(provider, customBase, protocol string) (string, error) {
	base, err := Base(provider, customBase)
	if err != nil {
		return "", err
	}
	if protocol != catalog.ProtocolAnthropic || customBase != "" {
		return base, nil
	}
	if meta, known := catalog.Providers[provider]; known {
		return meta.AnthropicBaseURL, nil
	}
	return base, nil
}

// Home is the registration page for a managed provider. A custom endpoint has
// none, and guessing one would send the user somewhere OneAgent does not know.
func Home(provider string) (string, error) {
	meta, known := catalog.Providers[provider]
	if !known {
		return "", oerr.New("INVALID_REQUEST", "Registration is only available for ppio or novita")
	}
	return meta.Home, nil
}

// openAIRouteSuffixes are the endpoint paths a user may have pasted along with
// the base. Stripped so the base can be rebuilt exactly once.
var openAIRouteSuffixes = []string{"/chat/completions", "/responses", "/models"}

// OpenAIBaseURL normalises any pasted OpenAI-compatible URL to end in /v1.
//
// Users paste whatever their provider's documentation showed, which is often a
// full route. Appending /v1 to that would produce
// .../v1/chat/completions/v1/chat/completions.
func OpenAIBaseURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	// Not a loop-until-stable: each suffix is checked once, matching the Python
	// implementation, so ".../models/models" keeps one segment on both sides.
	for _, suffix := range openAIRouteSuffixes {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimRight(strings.TrimSuffix(base, suffix), "/")
		}
	}
	if strings.HasSuffix(base, "/v1") {
		return base
	}
	return base + "/v1"
}

// AnthropicMessagesURL normalises any Anthropic base to exactly one
// /v1/messages.
//
// The bases arrive in both shapes: the managed providers expose .../anthropic
// with no version segment, while a custom base is commonly pasted already
// ending in /v1.
func AnthropicMessagesURL(baseURL string) string {
	base := strings.TrimRight(baseURL, "/")
	// First match wins, so "/v1/messages" is not then re-stripped as
	// "/messages" -- which would remove the version segment too.
	for _, suffix := range []string{"/v1/messages", "/messages"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimRight(strings.TrimSuffix(base, suffix), "/")
			break
		}
	}
	if strings.HasSuffix(base, "/v1") {
		return base + "/messages"
	}
	return base + "/v1/messages"
}
