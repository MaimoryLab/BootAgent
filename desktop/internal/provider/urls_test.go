package provider

import (
	"errors"
	"strings"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
)

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	var oneAgentErr *oerr.Error
	if !errors.As(err, &oneAgentErr) {
		t.Fatalf("err = %v, want an *oerr.Error", err)
	}
	if oneAgentErr.Code != want {
		t.Errorf("code = %q, want %q", oneAgentErr.Code, want)
	}
}

func TestABaseURLWithoutAUsableSchemeOrHostIsRefused(t *testing.T) {
	for _, value := range []string{"", "ftp://example.com", "https:///missing-host", "example.com", "//example.com"} {
		t.Run(value, func(t *testing.T) {
			if _, err := ValidateBaseURL(value); err == nil {
				t.Fatalf("%q should be refused", value)
			} else {
				assertCode(t, err, "INVALID_REQUEST")
			}
		})
	}
}

func TestControlCharactersAreRefusedBecauseTheyCanSplitAHeaderOrConfigLine(t *testing.T) {
	for _, value := range []string{
		"https://example.com/\nHost: evil",
		"https://example.com/\rX: y",
		"https://example.com/\x00",
		"https://example.com/\x7f",
		"https://example.com/\tpath",
	} {
		if _, err := ValidateBaseURL(value); err == nil {
			t.Errorf("%q should be refused", value)
		}
	}
}

func TestCredentialsInAURLAreRefusedBecauseTheyWouldBeWrittenToDisk(t *testing.T) {
	// The endpoint ends up in a config file in plain text, and in any log that
	// echoes it. A key must never arrive by this route.
	for _, value := range []string{
		"https://user:secret@example.com/v1",
		"https://user@example.com/v1",
	} {
		if _, err := ValidateBaseURL(value); err == nil {
			t.Errorf("%q should be refused", value)
		} else {
			assertCode(t, err, "INVALID_REQUEST")
		}
	}
}

func TestAValidURLLosesOnlyItsTrailingSlash(t *testing.T) {
	for input, want := range map[string]string{
		"https://example.com/v1/":    "https://example.com/v1",
		"https://example.com/v1":     "https://example.com/v1",
		"http://127.0.0.1:9000/":     "http://127.0.0.1:9000",
		"https://example.com/a/b///": "https://example.com/a/b",
		"https://example.com/v1?x=1": "https://example.com/v1?x=1",
	} {
		got, err := ValidateBaseURL(input)
		if err != nil {
			t.Errorf("%q: unexpected error %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ValidateBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAManagedProviderResolvesToItsCatalogBase(t *testing.T) {
	for id, meta := range catalog.Providers {
		got, err := Base(id, "")
		if err != nil {
			t.Errorf("%s: unexpected error %v", id, err)
			continue
		}
		if got != meta.BaseURL {
			t.Errorf("%s: base = %q, want %q", id, got, meta.BaseURL)
		}
	}
}

func TestACustomBaseOverridesAManagedProvider(t *testing.T) {
	// So a user can point a managed provider at their own gateway without
	// having to select "custom" and lose the provider's other defaults.
	got, err := Base("ppio", "http://127.0.0.1:9000/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "http://127.0.0.1:9000" {
		t.Fatalf("base = %q, want the custom endpoint", got)
	}
}

func TestCustomWithoutABaseIsReportedAsAMissingURL(t *testing.T) {
	// The provider selection is fine; the endpoint is what is absent, and the
	// message should say so rather than blaming the provider.
	_, err := Base(CustomProvider, "")
	if err == nil {
		t.Fatal("custom with no base must be refused")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("message = %q, want it to name the missing URL", err.Error())
	}
}

func TestAnUnknownProviderIsRefused(t *testing.T) {
	_, err := Base("not-a-provider", "")
	if err == nil {
		t.Fatal("an unknown provider must be refused")
	}
	assertCode(t, err, "INVALID_REQUEST")
}

func TestAnthropicAgentsGetTheSeparateRouteOnManagedProviders(t *testing.T) {
	// An Anthropic-speaking Agent pointed at the OpenAI route reports success
	// and then cannot authenticate. This is the defect the protocol-keyed
	// derivation exists to prevent.
	for id, meta := range catalog.Providers {
		got, err := ConfigBase(id, "", catalog.ProtocolAnthropic)
		if err != nil {
			t.Errorf("%s: unexpected error %v", id, err)
			continue
		}
		if got != meta.AnthropicBaseURL {
			t.Errorf("%s: anthropic base = %q, want %q", id, got, meta.AnthropicBaseURL)
		}
	}
}

func TestNonAnthropicProtocolsKeepTheOpenAICompatibleBase(t *testing.T) {
	for _, protocol := range []string{catalog.ProtocolOpenAI, catalog.ProtocolResponses} {
		got, err := ConfigBase("ppio", "", protocol)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != catalog.Providers["ppio"].BaseURL {
			t.Errorf("protocol=%s base = %q, want the OpenAI route", protocol, got)
		}
	}
}

func TestACustomBaseIsNotRewrittenForAnthropic(t *testing.T) {
	// The user gave one endpoint. Substituting a managed provider's Anthropic
	// route would silently send the request somewhere they did not choose.
	for _, testCase := range []struct{ provider, custom string }{
		{"ppio", "https://override.example.com"},
		{CustomProvider, "http://127.0.0.1:9000"},
	} {
		got, err := ConfigBase(testCase.provider, testCase.custom, catalog.ProtocolAnthropic)
		if err != nil {
			t.Errorf("%+v: unexpected error %v", testCase, err)
			continue
		}
		if got != testCase.custom {
			t.Errorf("%+v: base = %q, want the custom endpoint untouched", testCase, got)
		}
	}
}

func TestRegistrationIsOnlyOfferedForProvidersThatHaveOne(t *testing.T) {
	for id, meta := range catalog.Providers {
		got, err := Home(id)
		if err != nil {
			t.Errorf("%s: unexpected error %v", id, err)
			continue
		}
		if got != meta.Home {
			t.Errorf("%s: home = %q, want %q", id, got, meta.Home)
		}
	}
	// A custom endpoint has no registration page, and guessing one would send
	// the user somewhere OneAgent does not know.
	if _, err := Home(CustomProvider); err == nil {
		t.Error("custom must not offer a registration page")
	}
}

func TestAPastedRouteIsStrippedBeforeV1IsAppended(t *testing.T) {
	// Users paste whatever their provider's docs showed. Appending /v1 blindly
	// produced .../v1/chat/completions/v1/chat/completions.
	for input, want := range map[string]string{
		"https://example.com/v1/chat/completions": "https://example.com/v1",
		"https://example.com/v1/responses":        "https://example.com/v1",
		"https://example.com/v1/models":           "https://example.com/v1",
		"https://example.com/v1":                  "https://example.com/v1",
		"https://example.com/v1/":                 "https://example.com/v1",
		"https://example.com":                     "https://example.com/v1",
		"https://api.ppio.com/openai":             "https://api.ppio.com/openai/v1",
	} {
		if got := OpenAIBaseURL(input); got != want {
			t.Errorf("OpenAIBaseURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBothAnthropicBaseShapesNormaliseToOneMessagesRoute(t *testing.T) {
	// The managed providers expose .../anthropic with no version segment, while
	// a custom base is commonly pasted already ending in /v1.
	for input, want := range map[string]string{
		"https://api.ppio.com/anthropic":   "https://api.ppio.com/anthropic/v1/messages",
		"https://api.novita.ai/anthropic/": "https://api.novita.ai/anthropic/v1/messages",
		"https://proxy.test/v1":            "https://proxy.test/v1/messages",
		"https://proxy.test/v1/messages":   "https://proxy.test/v1/messages",
		"https://proxy.test/messages":      "https://proxy.test/v1/messages",
		"https://proxy.test/v1/messages/":  "https://proxy.test/v1/messages",
	} {
		if got := AnthropicMessagesURL(input); got != want {
			t.Errorf("AnthropicMessagesURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestStrippingMessagesStopsAtTheFirstMatchSoTheVersionSurvives(t *testing.T) {
	// Checking "/messages" after already stripping "/v1/messages" would remove
	// the version segment too and produce ".../v1/messages" from a different
	// base than the user gave.
	if got := AnthropicMessagesURL("https://x.test/v1/messages"); got != "https://x.test/v1/messages" {
		t.Fatalf("got %q, want the URL unchanged", got)
	}
}
