package provider

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
)

// failingTransport reports a transport-level failure, which is the case no HTTP
// status can express: nothing answered.
type failingTransport struct{ err error }

func (f failingTransport) Do(*http.Request) (*http.Response, error) { return nil, f.err }

func TestATimeoutIsToldApartFromAnUnreachableEndpoint(t *testing.T) {
	// These map to different error codes and therefore different advice. Telling
	// someone their key was rejected when the endpoint never answered sends them
	// to the wrong place, and the reverse sends them to check a firewall over a
	// slow provider.
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"deadline exceeded", context.DeadlineExceeded, "TIMEOUT"},
		{"net timeout", timeoutError{}, "TIMEOUT"},
		{"wrapped deadline", &net.OpError{
			Op: "dial", Err: context.DeadlineExceeded,
		}, "TIMEOUT"},
		{"reason says timed out", errors.New("read tcp: i/o timed out"), "TIMEOUT"},
		{"connection refused", errors.New("connect: connection refused"), "PROVIDER_UNREACHABLE"},
		{"no such host", errors.New("lookup nowhere.invalid: no such host"), "PROVIDER_UNREACHABLE"},
		{"tls failure", errors.New("tls: handshake failure"), "PROVIDER_UNREACHABLE"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := &Client{HTTP: failingTransport{err: testCase.err}}
			verdict, err := client.Probe(ProbeRequest{
				Protocol: catalog.ProtocolOpenAI, Provider: "ppio", APIKey: "sk-a", Model: "m",
			})
			if err != nil {
				t.Fatalf("a transport failure must be a verdict, not an error: %v", err)
			}
			if verdict.ErrorCode != testCase.want {
				t.Errorf("error_code = %q, want %q", verdict.ErrorCode, testCase.want)
			}
			if verdict.OK || verdict.Reachable {
				t.Error("nothing answered, so the endpoint is neither ok nor reachable")
			}
			if !verdict.Retryable {
				t.Error("a transport failure is worth retrying")
			}
			if !strings.HasPrefix(verdict.Message, "Cannot reach endpoint: ") {
				t.Errorf("message = %q, want it to say the endpoint was unreachable", verdict.Message)
			}
			// The same classification has to hold for discovery, which reports its
			// own shape.
			listing, err := client.ListModels("ppio", "", "sk-a")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if listing.ErrorCode != testCase.want {
				t.Errorf("discovery error_code = %q, want %q", listing.ErrorCode, testCase.want)
			}
			if listing.Models == nil {
				t.Error("discovery reports models as null rather than an empty list")
			}
		})
	}
}

// timeoutError is a net.Error that reports a timeout, which is how the standard
// library signals a deadline on some paths.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o deadline reached" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestAProbeRefusesToRunWithoutTheInputsItNeeds(t *testing.T) {
	client := &Client{HTTP: failingTransport{err: errors.New("must not be called")}}
	if _, err := client.Probe(ProbeRequest{
		Protocol: catalog.ProtocolOpenAI, Provider: "ppio", Model: "m",
	}); err == nil {
		t.Error("a probe without a key was attempted")
	}
	if _, err := client.Probe(ProbeRequest{
		Protocol: "graphql", Provider: "ppio", APIKey: "sk-a", Model: "m",
	}); err == nil {
		t.Error("an unknown protocol was probed")
	}
	if _, err := client.ListModels("ppio", "", ""); err == nil {
		t.Error("discovery without a key was attempted")
	}
	// An unusable custom endpoint is refused before a request is built, rather
	// than reported as unreachable: the URL is the problem, not the network.
	if _, err := client.Probe(ProbeRequest{
		Protocol: catalog.ProtocolOpenAI, Provider: "custom",
		CustomBase: "ftp://vendor.example", APIKey: "sk-a", Model: "m",
	}); err == nil {
		t.Error("a non-HTTP endpoint was probed")
	}
}

func TestResolveModelPrefersALiveListingAndFallsBackQuietly(t *testing.T) {
	// A hardcoded id goes stale when a provider retires a model, so the listing
	// wins when there is one. Discovery failing is not fatal.
	listed := &fakeTransport{status: 200, body: `{"data":[{"id":"text-embedding-3"},{"id":"deepseek/v3-chat"}]}`}
	client := &Client{HTTP: listed}
	if got := client.ResolveModel("ppio", "sk-a", "", ""); got != "deepseek/v3-chat" {
		// The embedding model is skipped: probing a protocol endpoint with one
		// fails for a reason unrelated to connectivity or the key.
		t.Errorf("ResolveModel = %q, want the first chat-capable id", got)
	}

	// A model the user chose short-circuits discovery, so no round trip happens.
	refusing := failingTransport{err: errors.New("must not be called")}
	if got := (&Client{HTTP: refusing}).ResolveModel("ppio", "sk-a", "chosen", ""); got != "chosen" {
		t.Errorf("ResolveModel = %q, want the chosen model", got)
	}
	// No key means discovery would be refused, so the fallback is used directly.
	if got := (&Client{HTTP: refusing}).ResolveModel("ppio", "", "", ""); got != catalog.FallbackProbeModel("ppio") {
		t.Errorf("ResolveModel = %q, want the catalog fallback", got)
	}
	// And a failed listing falls back rather than propagating.
	if got := (&Client{HTTP: refusing}).ResolveModel("novita", "sk-a", "", ""); got != catalog.FallbackProbeModel("novita") {
		t.Errorf("ResolveModel = %q, want the provider's own fallback", got)
	}
}

func TestABodyLargerThanTheLimitDoesNotExhaustMemory(t *testing.T) {
	// A provider that streams an unbounded error page must not be able to make
	// this allocate without limit.
	huge := strings.Repeat("x", 256*1024)
	client := &Client{HTTP: &fakeTransport{status: 500, body: huge}}
	verdict, err := client.Probe(ProbeRequest{
		Protocol: catalog.ProtocolOpenAI, Provider: "ppio", APIKey: "sk-a", Model: "m",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict.OK {
		t.Error("a 500 was reported as a pass")
	}
	// The message names the status rather than quoting the body, so its size
	// never reaches the user either.
	if strings.Contains(verdict.Message, "xxxx") {
		t.Errorf("the body was echoed into the message: %q", verdict.Message)
	}
}

func TestTheDefaultClientAppliesATimeoutAndDropsCredentialsOnACrossHostRedirect(t *testing.T) {
	// NewClient is what production uses, so its redirect policy is the one that
	// matters: replaying Authorization to whatever host a redirect names is a
	// credential leak an endpoint could trigger.
	var elsewhere *http.Request
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		elsewhere = request.Clone(request.Context())
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "{}")
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL+"/v1/models", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	client := NewClient(5 * time.Second)
	request, err := http.NewRequest(http.MethodGet, origin.URL+"/v1/models", nil)
	if err != nil {
		t.Fatalf("cannot build a request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer sk-must-not-travel")
	request.Header.Set("X-Api-Key", "sk-must-not-travel")
	if _, _, err := client.send(request); err != nil {
		t.Fatalf("the request failed: %v", err)
	}

	if elsewhere == nil {
		t.Fatal("the redirect was not followed, so this proves nothing")
	}
	if got := elsewhere.Header.Get("Authorization"); got != "" {
		t.Errorf("the credential was replayed to another host: %q", got)
	}
	if got := elsewhere.Header.Get("X-Api-Key"); got != "" {
		t.Errorf("the API key header was replayed to another host: %q", got)
	}
}

func TestASameHostRedirectKeepsTheCredential(t *testing.T) {
	// Dropping it unconditionally would break a provider that redirects /v1/models
	// to a canonical path on its own host.
	var final *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/models" {
			http.Redirect(writer, request, "/v1/models/", http.StatusTemporaryRedirect)
			return
		}
		final = request.Clone(request.Context())
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "{}")
	}))
	defer server.Close()

	client := NewClient(5 * time.Second)
	request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/models", nil)
	if err != nil {
		t.Fatalf("cannot build a request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer sk-keep")
	if _, _, err := client.send(request); err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	if final == nil {
		t.Fatal("the redirect was not followed")
	}
	if got := final.Header.Get("Authorization"); got != "Bearer sk-keep" {
		t.Errorf("the credential was dropped on a same-host redirect: %q", got)
	}
}
