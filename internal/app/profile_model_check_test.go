package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	profileStore "github.com/MaimoryLab/BootAgent/internal/profile"
	"github.com/MaimoryLab/BootAgent/internal/provider"
)

// modelGateway answers each probe by the model in its body, so a test can make
// one model work and another fail on the same endpoint.
type modelGateway struct {
	mu       sync.Mutex
	verdicts map[string]func(path string) (*http.Response, error)
	requests []string
	// onRequest runs inside each probe, for tests that look at what is held while
	// the request is in flight.
	onRequest func()
}

func (g *modelGateway) Do(request *http.Request) (*http.Response, error) {
	var body struct {
		Model string `json:"model"`
	}
	if request.Body != nil {
		data, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(data, &body)
	}
	g.mu.Lock()
	g.requests = append(g.requests, request.URL.Path+" "+body.Model)
	verdict := g.verdicts[body.Model]
	onRequest := g.onRequest
	g.mu.Unlock()
	if onRequest != nil {
		onRequest()
	}
	if verdict == nil {
		return appProviderResponse(http.StatusOK, `{}`), nil
	}
	return verdict(request.URL.Path)
}

func (g *modelGateway) count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.requests)
}

func (g *modelGateway) reset() {
	g.mu.Lock()
	g.requests = nil
	g.mu.Unlock()
}

const checkKey = "sk-check-secret"

func refusing(status int, body string) func(string) (*http.Response, error) {
	return func(string) (*http.Response, error) { return appProviderResponse(status, body), nil }
}

// gatewayCore is a Profile on a custom gateway, followed by Claude Code
// (Anthropic Messages) and Codex (Responses) -- the pair the report describes.
func gatewayCore(t *testing.T, gateway *modelGateway) *UseCases {
	t.Helper()
	core := activationCore(t, t.TempDir(), provider.NewClient(gateway), "linux")
	if _, err := core.SaveProvider(context.Background(), provider.Entry{
		ID: "gw", Name: "Gateway", BaseURL: "https://gw.example/v1", AnthropicBaseURL: "https://gw.example/v1", APIKey: checkKey,
	}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{ID: "team", Label: "Team", Provider: "gw", Model: "good-model", ConfigMode: "provider"}); err != nil {
		t.Fatal(err)
	}
	for _, agentID := range []string{"claude-code", "codex"} {
		if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{AgentID: agentID, Provider: "gw", ProfileID: "team"}); err != nil {
			t.Fatalf("activate %s: %v", agentID, err)
		}
	}
	gateway.reset()
	return core
}

func saveModel(t *testing.T, core *UseCases, model string) SaveProfileResult {
	t.Helper()
	result, err := core.SaveProfile(context.Background(), SaveProfileOptions{ID: "team", Label: "Team", Provider: "gw", Model: model, ConfigMode: "provider"})
	if err != nil {
		t.Fatalf("SaveProfile(%s) error = %v", model, err)
	}
	return result
}

// checkModel saves model on the team Profile, then runs the check the user asks
// for from the UI.
func checkModel(t *testing.T, core *UseCases, model string) *ProfileModelCheck {
	t.Helper()
	saveModel(t, core, model)
	check, err := core.CheckProfileModel(context.Background(), "team", nil)
	if err != nil {
		t.Fatalf("CheckProfileModel(%s) error = %v", model, err)
	}
	if check == nil {
		t.Fatalf("CheckProfileModel(%s) = nil, want a verdict", model)
	}
	return check
}

// Each check is a billed completion request, so it is the user's to send.
// Saving a Profile -- even one that changes the model every Agent follows --
// and applying one send nothing to the model.
func TestSaveAndApplyDoNotCheckTheModel(t *testing.T) {
	gateway := &modelGateway{verdicts: map[string]func(string) (*http.Response, error){
		"bad-model": refusing(http.StatusNotFound, `{"error":{"message":"no such model"}}`),
	}}
	core := gatewayCore(t, gateway)
	result := saveModel(t, core, "bad-model")
	if !slices.Equal(result.Reapplied, []string{"claude-code", "codex"}) {
		t.Fatalf("reapplied = %v, want both Agents", result.Reapplied)
	}
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{AgentID: "claude-code", Provider: "gw", ProfileID: "team"}); err != nil {
		t.Fatal(err)
	}
	for _, request := range gateway.requests {
		if strings.HasSuffix(request, " bad-model") {
			t.Fatalf("requests = %v; save and apply must not probe the model", gateway.requests)
		}
	}
}

// The report: a model the gateway lists but refuses broke Claude Code and
// Codex together. Asked for, the check names the model, both Agents, and the
// provider's own words.
func TestModelCheckNamesARefusedModelAndTheAgentsItBreaks(t *testing.T) {
	gateway := &modelGateway{verdicts: map[string]func(string) (*http.Response, error){
		"claude-opus-5-5": refusing(http.StatusNotFound, `{"type":"error","error":{"type":"not_found_error","message":"model: claude-opus-5-5 does not exist or you do not have access to it (key `+checkKey+`)"}}`),
	}}
	core := gatewayCore(t, gateway)

	check := checkModel(t, core, "claude-opus-5-5")
	if check.Status != ModelCheckUnavailable || check.Reason != "refused" {
		t.Errorf("status/reason = %s/%s, want unavailable/refused", check.Status, check.Reason)
	}
	if check.Model != "claude-opus-5-5" {
		t.Errorf("model = %q, want claude-opus-5-5", check.Model)
	}
	if !slices.Equal(check.Agents, []string{"claude-code", "codex"}) {
		t.Errorf("agents = %v, want both Agents", check.Agents)
	}
	if check.HTTPStatus != http.StatusNotFound {
		t.Errorf("http status = %d, want 404", check.HTTPStatus)
	}
	if !strings.Contains(check.Detail, "does not exist or you do not have access") {
		t.Errorf("detail = %q, want the provider's own explanation", check.Detail)
	}
	if strings.Contains(check.Detail, checkKey) {
		t.Errorf("detail = %q leaks the API key the gateway echoed back", check.Detail)
	}
}

// A model that answers is reported as such, naming the Agents it was checked
// for, so a check the user asked for always ends with an answer.
func TestModelCheckReportsAWorkingModelAsAvailable(t *testing.T) {
	gateway := &modelGateway{}
	core := gatewayCore(t, gateway)
	check := checkModel(t, core, "good-model")
	if check.Status != ModelCheckAvailable || check.Reason != "" {
		t.Fatalf("check = %#v, want available with no reason", check)
	}
	if !slices.Equal(check.Agents, []string{"claude-code", "codex"}) {
		t.Errorf("agents = %v, want the Agents checked", check.Agents)
	}
	if gateway.count() == 0 {
		t.Error("no request was sent")
	}
}

// Each Agent is checked over the protocol it actually sends. Here only
// Anthropic Messages refuses, so only Claude Code is named.
func TestModelCheckNamesOnlyTheAgentsWhoseProtocolRefuses(t *testing.T) {
	gateway := &modelGateway{verdicts: map[string]func(string) (*http.Response, error){
		"chat-only": func(path string) (*http.Response, error) {
			if strings.HasSuffix(path, "/messages") {
				return appProviderResponse(http.StatusBadRequest, `{"error":{"message":"model not available on messages"}}`), nil
			}
			return appProviderResponse(http.StatusOK, `{}`), nil
		},
	}}
	core := gatewayCore(t, gateway)
	check := checkModel(t, core, "chat-only")
	if check.Status != ModelCheckUnavailable || !slices.Equal(check.Agents, []string{"claude-code"}) {
		t.Fatalf("check = %#v, want only claude-code", check)
	}
	if check.Protocol != provider.ProtocolAnthropic {
		t.Errorf("protocol = %q, want anthropic", check.Protocol)
	}
}

// An unreachable provider is not evidence against the model. The result says
// "could not verify", not "unavailable", so the UI does not tell the user to
// abandon a model that may be fine.
func TestModelCheckReportsAnUnreachableProviderAsUnverified(t *testing.T) {
	gateway := &modelGateway{verdicts: map[string]func(string) (*http.Response, error){
		"new-model": func(string) (*http.Response, error) { return nil, errors.New("dial tcp: connection refused") },
	}}
	core := gatewayCore(t, gateway)
	check := checkModel(t, core, "new-model")
	if check.Status != ModelCheckUnverified || check.Reason != "network" {
		t.Fatalf("check = %#v, want unverified/network", check)
	}
	if check.HTTPStatus != 0 {
		t.Errorf("http status = %d, want 0 for a request that got no answer", check.HTTPStatus)
	}
}

func TestCodexDesktopModelCheckReportsUnsupportedResponsesEndpoint(t *testing.T) {
	model := "aphrodite/venus-marble-5-5-es-cc-dst"
	detail := "model: " + model + " does not support endpoint: responses"
	gateway := &modelGateway{verdicts: map[string]func(string) (*http.Response, error){
		model: func(path string) (*http.Response, error) {
			if path != "/v1/responses" {
				return appProviderResponse(http.StatusOK, `{}`), nil
			}
			return appProviderResponse(http.StatusBadRequest, `{"code":400,"reason":"INVALID_REQUEST_BODY","message":"`+detail+`","metadata":0}`), nil
		},
	}}
	core := gatewayCore(t, gateway)
	saveModel(t, core, model)
	gateway.reset()
	check, err := core.CheckProfileModel(context.Background(), "team", []string{"codex", "chatgpt-desktop"})
	if err != nil {
		t.Fatal(err)
	}
	if check == nil || check.Status != ModelCheckUnavailable || check.Reason != "refused" || check.Protocol != provider.ProtocolResponses || check.HTTPStatus != http.StatusBadRequest || check.Detail != detail {
		t.Fatalf("check = %#v", check)
	}
	if !slices.Equal(check.Agents, []string{"chatgpt-desktop", "codex"}) {
		t.Fatalf("affected Agents = %v", check.Agents)
	}
	if !slices.Equal(gateway.requests, []string{"/v1/responses " + model}) {
		t.Fatalf("requests = %v; a Codex check must not fall back to another protocol", gateway.requests)
	}
}

// A rate limit or a server error is about the provider's state, not the model.
// It is reported as unverified so the user is not told to abandon a model that
// works. A 5xx that says the endpoint does not serve the protocol is a real
// refusal and stays one.
func TestModelCheckDoesNotBlameTheModelForABusyProvider(t *testing.T) {
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		gateway := &modelGateway{verdicts: map[string]func(string) (*http.Response, error){
			"new-model": refusing(status, `{"error":{"message":"try again later"}}`),
		}}
		core := gatewayCore(t, gateway)
		if check := checkModel(t, core, "new-model"); check.Status != ModelCheckUnverified || check.Reason != "transient" {
			t.Errorf("HTTP %d: check = %#v, want unverified/transient", status, check)
		}
	}
	gateway := &modelGateway{verdicts: map[string]func(string) (*http.Response, error){
		"new-model": refusing(http.StatusInternalServerError, `{"error":{"message":"this model does not support endpoint /v1/messages"}}`),
	}}
	core := gatewayCore(t, gateway)
	if check := checkModel(t, core, "new-model"); check.Reason != "refused" {
		t.Errorf("unsupported-endpoint 500: check = %#v, want refused", check)
	}
}

// A rejected key is named as a key problem, because changing the model would
// not fix it.
func TestModelCheckNamesARejectedKey(t *testing.T) {
	gateway := &modelGateway{verdicts: map[string]func(string) (*http.Response, error){
		"new-model": refusing(http.StatusUnauthorized, `{"error":{"message":"invalid api key"}}`),
	}}
	core := gatewayCore(t, gateway)
	if check := checkModel(t, core, "new-model"); check.Status != ModelCheckUnavailable || check.Reason != "key" {
		t.Fatalf("check = %#v, want unavailable/key", check)
	}
}

// With no key saved there is nothing to send. That is reported, not probed.
func TestModelCheckReportsAMissingKeyWithoutARequest(t *testing.T) {
	gateway := &modelGateway{}
	core := activationCore(t, t.TempDir(), provider.NewClient(gateway), "linux")
	if _, err := core.SaveProvider(context.Background(), provider.Entry{ID: "nokey", Name: "No key", BaseURL: "https://gw.example/v1"}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{ID: "p", Label: "P", Provider: "nokey", Model: "m", ConfigMode: "provider"}); err != nil {
		t.Fatal(err)
	}
	check, err := core.CheckProfileModel(context.Background(), "p", nil)
	if err != nil {
		t.Fatal(err)
	}
	if check == nil || check.Reason != "no_key" || check.Status != ModelCheckUnverified {
		t.Fatalf("check = %#v, want unverified/no_key", check)
	}
	if gateway.count() != 0 {
		t.Errorf("sent %d requests without a key", gateway.count())
	}
}

// A Profile nothing follows yet is checked over the protocol it is set up
// for, and names no Agents.
func TestModelCheckCoversAProfileNoAgentFollowsYet(t *testing.T) {
	gateway := &modelGateway{verdicts: map[string]func(string) (*http.Response, error){
		"bad": refusing(http.StatusNotFound, `{"error":{"message":"no such model"}}`),
	}}
	core := activationCore(t, t.TempDir(), provider.NewClient(gateway), "linux")
	if _, err := core.SaveProvider(context.Background(), provider.Entry{ID: "gw", Name: "Gateway", BaseURL: "https://gw.example/v1", AnthropicBaseURL: "https://gw.example/v1", APIKey: checkKey}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{ID: "fresh", Label: "Fresh", Provider: "gw", Model: "bad", ConfigMode: "provider", Protocol: provider.ProtocolAnthropic}); err != nil {
		t.Fatal(err)
	}
	check, err := core.CheckProfileModel(context.Background(), "fresh", nil)
	if err != nil {
		t.Fatal(err)
	}
	if check == nil || check.Status != ModelCheckUnavailable || len(check.Agents) != 0 || check.Protocol != provider.ProtocolAnthropic {
		t.Fatalf("check = %#v, want unavailable on anthropic with no Agents", check)
	}
}

// The check is a network request, so it must run with the configuration write
// lock released: a slow provider would otherwise stall every other write.
func TestModelCheckRunsWithTheWriteLockReleased(t *testing.T) {
	gateway := &modelGateway{}
	core := gatewayCore(t, gateway)
	var heldDuringProbe atomic.Bool
	var probed atomic.Bool
	gateway.mu.Lock()
	gateway.onRequest = func() {
		probed.Store(true)
		if !core.writeMu.TryLock() {
			heldDuringProbe.Store(true)
			return
		}
		core.writeMu.Unlock()
	}
	gateway.mu.Unlock()
	checkModel(t, core, "another-model")
	if !probed.Load() {
		t.Fatal("the model was not checked")
	}
	if heldDuringProbe.Load() {
		t.Error("writeMu was held while the provider was being asked")
	}
}

// Closing the window mid-check is not a network failure, and must not be
// reported as one: the check ends with a cancellation, not a verdict.
func TestModelCheckIsCancelledNotUnverified(t *testing.T) {
	gateway := &modelGateway{}
	core := gatewayCore(t, gateway)
	saveModel(t, core, "slow")
	ctx, cancel := context.WithCancel(context.Background())
	gateway.mu.Lock()
	gateway.verdicts = map[string]func(string) (*http.Response, error){
		"slow": func(string) (*http.Response, error) {
			cancel()
			return nil, context.Canceled
		},
	}
	gateway.mu.Unlock()
	check, err := core.CheckProfileModel(ctx, "team", nil)
	if check != nil || oneerrors.As(err).Code != oneerrors.Timeout {
		t.Fatalf("check = %#v, err = %v; want a cancellation error and no verdict", check, err)
	}
}

// The review's reproduction: a model that answers only over Responses, on a
// Profile dsh follows. dsh is written with the Chat Completions route, so dsh
// cannot call that model, and the check has to say so.
func TestModelCheckWarnsWhenDSHIsWrittenWithAProtocolTheModelRefuses(t *testing.T) {
	gateway := &modelGateway{verdicts: map[string]func(string) (*http.Response, error){
		"responses-only": func(path string) (*http.Response, error) {
			if strings.HasSuffix(path, "/chat/completions") {
				return appProviderResponse(http.StatusBadRequest, `{"error":{"message":"use the responses API for this model"}}`), nil
			}
			return appProviderResponse(http.StatusOK, `{}`), nil
		},
	}}
	core := activationCore(t, t.TempDir(), provider.NewClient(gateway), "linux")
	if _, err := core.SaveProvider(context.Background(), provider.Entry{ID: "gw", Name: "Gateway", BaseURL: "https://gw.example/v1", APIKey: checkKey}, true, false); err != nil {
		t.Fatal(err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{ID: "team", Label: "Team", Provider: "gw", Model: "good-model", ConfigMode: "provider"}); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ActivateAgent(context.Background(), ActivateAgentOptions{AgentID: "dsh", Provider: "gw", ProfileID: "team"}); err != nil {
		t.Fatal(err)
	}
	check := checkModel(t, core, "responses-only")
	if check.Status != ModelCheckUnavailable || !slices.Equal(check.Agents, []string{"dsh"}) || check.Protocol != provider.ProtocolOpenAI {
		t.Fatalf("check = %#v, want dsh named on openai", check)
	}
}

// A page configuring one Agent checks for that Agent alone, and an unknown
// Profile, the adapter's own Profiles, and a Profile without a model are
// refused rather than answered with an empty verdict.
func TestCheckProfileModelNarrowsToTheNamedAgentAndRefusesWhatItCannotCheck(t *testing.T) {
	gateway := &modelGateway{verdicts: map[string]func(string) (*http.Response, error){
		"good-model": func(path string) (*http.Response, error) {
			if strings.HasSuffix(path, "/responses") {
				return appProviderResponse(http.StatusBadRequest, `{"error":{"message":"responses not served for this model"}}`), nil
			}
			return appProviderResponse(http.StatusOK, `{}`), nil
		},
	}}
	core := gatewayCore(t, gateway)
	check, err := core.CheckProfileModel(context.Background(), "team", []string{"claude-code"})
	if err != nil || check == nil || check.Status != ModelCheckAvailable || !slices.Equal(check.Agents, []string{"claude-code"}) {
		t.Fatalf("claude-code check = %#v, %v; want available for claude-code", check, err)
	}
	check, err = core.CheckProfileModel(context.Background(), "team", nil)
	if err != nil || check == nil || check.Status != ModelCheckUnavailable || !slices.Equal(check.Agents, []string{"codex"}) {
		t.Fatalf("bound-Agents check = %#v, %v; want codex named", check, err)
	}
	if _, err := core.CheckProfileModel(context.Background(), "missing", nil); oneerrors.As(err).Code != oneerrors.InvalidRequest {
		t.Fatalf("unknown Profile error = %v, want InvalidRequest", err)
	}
	if _, err := core.SaveProfile(context.Background(), SaveProfileOptions{ID: "empty", Label: "Empty", Provider: "gw", ConfigMode: "provider"}); err == nil {
		if _, err := core.CheckProfileModel(context.Background(), "empty", nil); oneerrors.As(err).Code != oneerrors.InvalidRequest {
			t.Fatalf("model-less Profile error = %v, want InvalidRequest", err)
		}
	}
}

// Which protocol stands for an Agent: the one a Profile save or Apply writes.
// dsh can speak either OpenAI wire protocol, but those writes give it Chat
// Completions, so that is what is checked. A Chat Completions Profile reaches
// Claude Desktop through local adaptation, so its upstream is Chat Completions,
// not Anthropic Messages -- checking the latter would warn about a working setup.
func TestAgentProtocolOptionsFollowWhatEachAgentSends(t *testing.T) {
	core := &UseCases{}
	for _, test := range []struct {
		agentID, profileProtocol string
		want                     []string
	}{
		{"dsh", "", []string{provider.ProtocolOpenAI}},
		{"dsh-desktop", "", []string{provider.ProtocolOpenAI}},
		{"claude-code", "", []string{provider.ProtocolAnthropic}},
		{"codex", "", []string{provider.ProtocolResponses}},
		{"claude-desktop", provider.ProtocolOpenAI, []string{provider.ProtocolOpenAI}},
		{"claude-desktop", provider.ProtocolAnthropic, []string{provider.ProtocolAnthropic}},
		{"not-an-agent", "", nil},
	} {
		got := core.agentProtocolOptions(test.agentID, profileStore.Profile{Protocol: test.profileProtocol})
		if !slices.Equal(got, test.want) {
			t.Errorf("agentProtocolOptions(%s, %q) = %v, want %v", test.agentID, test.profileProtocol, got, test.want)
		}
	}
}
