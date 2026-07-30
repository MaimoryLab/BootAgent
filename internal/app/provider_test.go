package app

import (
	"context"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

type appProviderDoer func(*http.Request) (*http.Response, error)

func (doer appProviderDoer) Do(request *http.Request) (*http.Response, error) {
	return doer(request)
}

func appProviderResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func providerUseCases(t *testing.T, doer provider.HTTPDoer) *UseCases {
	t.Helper()
	return NewUseCasesWithProviderClient(StatusOptions{
		Home:     t.TempDir(),
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	}, provider.NewClient(doer))
}

func TestProbeProviderAggregatesAgentProtocols(t *testing.T) {
	seen := make([]string, 0)
	core := providerUseCases(t, appProviderDoer(func(request *http.Request) (*http.Response, error) {
		seen = append(seen, request.URL.Path)
		return appProviderResponse(http.StatusNoContent, ""), nil
	}))
	result, err := core.ProbeProvider(context.Background(), ProviderProbeOptions{
		Provider:   "custom",
		APIBaseURL: "https://proxy.test/v1",
		APIKey:     "key",
		Model:      "model",
		AgentIDs:   []string{"codex", "claude-code", "opencode"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Primary.OK || len(result.Protocols) != 3 || result.Primary.Protocol == nil || *result.Primary.Protocol != provider.ProtocolAnthropic {
		t.Fatalf("aggregated probe = %#v", result)
	}
	sort.Strings(seen)
	if !reflect.DeepEqual(seen, []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"}) {
		t.Fatalf("probe paths = %v", seen)
	}
}

func TestProbeProviderSelectsFirstFailure(t *testing.T) {
	core := providerUseCases(t, appProviderDoer(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v1/responses" {
			return appProviderResponse(http.StatusBadRequest, `{"message":"does not support endpoint"}`), nil
		}
		return appProviderResponse(http.StatusNoContent, ""), nil
	}))
	result, err := core.ProbeProvider(context.Background(), ProviderProbeOptions{
		Provider: "custom", APIBaseURL: "https://proxy.test/v1", APIKey: "key", Model: "model", AgentIDs: []string{"codex", "opencode"},
	})
	if err != nil || result.Primary.OK || result.Primary.ErrorCode == nil || *result.Primary.ErrorCode != oneerrors.ProtocolUnsupported {
		t.Fatalf("failure result = %#v, err=%v", result, err)
	}
}

func TestProtocolsForAgentsRejectsUnknownAndIgnoresGuideOnly(t *testing.T) {
	protocols, err := protocolsForAgents([]string{"openclaw"})
	if err != nil || !reflect.DeepEqual(protocols, []string{provider.ProtocolOpenAI}) {
		t.Fatalf("guide protocols = %v, err=%v", protocols, err)
	}
	if _, err := protocolsForAgents([]string{"missing-agent"}); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("unknown Agent error = %v", err)
	}
	if _, err := protocolsForAgents([]string{""}); err == nil || oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("empty Agent error = %v", err)
	}
}

func TestProviderUseCasesHonorCancellationBeforeNetwork(t *testing.T) {
	called := false
	core := providerUseCases(t, appProviderDoer(func(*http.Request) (*http.Response, error) {
		called = true
		return appProviderResponse(http.StatusOK, `{}`), nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := core.ListProviderModels(ctx, "ppio", "key", "")
	if err == nil || oneerrors.As(err).Code != oneerrors.Timeout || called {
		t.Fatalf("cancelled call = %v, called=%v", err, called)
	}
}
