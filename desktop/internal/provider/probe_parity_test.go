package provider

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// A probe's verdict decides what the wizard tells the user and which error code
// the CLI exits with, so a difference here is a difference in advice: reporting a
// rejected key as a network problem sends someone to check their firewall.
//
// Comparing the classification means driving both implementations over the same
// responses, which is what the fake transport below is for. The regular CI run
// reaches no provider, and that is deliberate.

// fakeTransport answers every request from a canned response.
type fakeTransport struct {
	status  int
	body    string
	err     error
	request *http.Request
}

func (f *fakeTransport) Do(request *http.Request) (*http.Response, error) {
	f.request = request
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     http.Header{},
	}, nil
}

// pythonVerdict is the shape both sides produce, decoded from Python's dict.
type pythonVerdict struct {
	OK        bool     `json:"ok"`
	Reachable bool     `json:"reachable"`
	Protocol  string   `json:"protocol"`
	Status    int      `json:"status"`
	Message   string   `json:"message"`
	ErrorCode *string  `json:"error_code"`
	Retryable bool     `json:"retryable"`
	Models    []string `json:"models"`
}

// probeResponses are what a provider actually answers with, including the cases
// that decide between "wrong key", "wrong protocol" and "unreachable".
var probeResponses = []struct {
	name   string
	status int
	body   string
}{
	{"ok", 200, `{"choices":[]}`},
	{"no content", 204, ``},
	{"unauthorized", 401, `{"error":"bad key"}`},
	{"forbidden", 403, `{"error":"forbidden"}`},
	{"not found", 404, `{"error":"no such route"}`},
	{"method not allowed", 405, ``},
	{"not implemented", 501, ``},
	{"bad request unsupported", 400, `{"error":{"message":"model does not support responses"}}`},
	{"bad request other", 400, `{"error":{"message":"max_tokens is too small"}}`},
	{"unprocessable unsupported", 422, `{"error":"unsupported endpoint for this model"}`},
	{"server error", 500, `internal`},
	{"server error unsupported", 500, `{"error":"not supported"}`},
	{"bad gateway", 502, `gateway`},
	{"rate limited", 429, `{"error":"slow down"}`},
	{"teapot", 418, ``},
}

func TestParityAProbeVerdictMatchesPython(t *testing.T) {
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from unittest.mock import patch
from urllib.error import HTTPError
import io

from oneagent import providers

payload = json.loads(sys.argv[2])
results = []
for case in payload:
    status, body = case["status"], case["body"]

    class FakeResponse:
        def __init__(self):
            self.status = status
        def read(self):
            return body.encode()
        def __enter__(self):
            return self
        def __exit__(self, *args):
            return False

    def fake_urlopen(request, timeout=None):
        if status in {200, 204}:
            return FakeResponse()
        raise HTTPError(request.full_url, status, "err", {}, io.BytesIO(body.encode()))

    with patch.object(providers, "urlopen", fake_urlopen):
        verdict = providers.protocol_probe(
            protocol=case["protocol"], provider="ppio", api_key="sk-test",
            model=case["model"], custom_base="", timeout=5,
        )
    results.append(verdict)
print(json.dumps(results))
`
	type probeCase struct {
		Protocol string `json:"protocol"`
		Model    string `json:"model"`
		Status   int    `json:"status"`
		Body     string `json:"body"`
	}
	cases := []probeCase{}
	for _, protocol := range []string{"openai", "responses", "anthropic"} {
		for _, response := range probeResponses {
			cases = append(cases, probeCase{
				Protocol: protocol, Model: "gpt-5-mini",
				Status: response.status, Body: response.body,
			})
		}
	}
	// Model names that reach the "does not support" message, where Python
	// interpolates with !r.
	for _, model := range []string{"", "it's", `has"dq`, "通义", `back\slash`} {
		cases = append(cases, probeCase{
			Protocol: "responses", Model: model, Status: 404, Body: "",
		})
	}

	encoded, err := json.Marshal(cases)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	command := exec.Command(pythonBin(t), "-c", script, repoRoot(t), string(encoded))
	command.Dir = repoRoot(t)
	output, err := command.Output()
	if err != nil {
		stderr := ""
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}
		t.Fatalf("python failed: %v\n%s", err, stderr)
	}
	var want []pythonVerdict
	if err := json.Unmarshal(output, &want); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}
	if len(want) != len(cases) {
		t.Fatalf("python returned %d verdicts for %d cases", len(want), len(cases))
	}

	for index, testCase := range cases {
		transport := &fakeTransport{status: testCase.Status, body: testCase.Body}
		client := &Client{HTTP: transport}
		got, err := client.Probe(ProbeRequest{
			Protocol: testCase.Protocol, Provider: "ppio", APIKey: "sk-test", Model: testCase.Model,
		})
		if err != nil {
			t.Errorf("%s/%d: Go refused what Python answered: %v", testCase.Protocol, testCase.Status, err)
			continue
		}
		compareVerdict(t,
			testCase.Protocol+"/"+strconv.Itoa(testCase.Status)+"/"+testCase.Model,
			got, want[index])
	}
}

func TestParityModelDiscoveryMatchesPython(t *testing.T) {
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from unittest.mock import patch
from urllib.error import HTTPError
import io

from oneagent import providers

payload = json.loads(sys.argv[2])
results = []
for case in payload:
    status, body = case["status"], case["body"]

    class FakeResponse:
        def __init__(self):
            self.status = status
        def read(self):
            return body.encode()
        def __enter__(self):
            return self
        def __exit__(self, *args):
            return False

    def fake_urlopen(request, timeout=None):
        if status == 200:
            return FakeResponse()
        raise HTTPError(request.full_url, status, "err", {}, io.BytesIO(body.encode()))

    with patch.object(providers, "urlopen", fake_urlopen):
        results.append(providers.list_models(
            provider="ppio", api_key="sk-test", custom_base="", timeout=5,
        ))
print(json.dumps(results))
`
	cases := []struct {
		Name   string `json:"name"`
		Status int    `json:"status"`
		Body   string `json:"body"`
	}{
		{"openai envelope", 200, `{"data":[{"id":"a"},{"id":"b"}]}`},
		{"bare list", 200, `[{"id":"a"}]`},
		{"string entries", 200, `["a","b"]`},
		{"mixed entries", 200, `{"data":[{"id":"a"},"b",{"name":"c"},null,42]}`},
		{"empty data", 200, `{"data":[]}`},
		{"no data key", 200, `{"object":"list"}`},
		{"data not a list", 200, `{"data":"nope"}`},
		{"entry without id", 200, `{"data":[{"object":"model"}]}`},
		{"empty id", 200, `{"data":[{"id":""}]}`},
		{"not json", 200, `<html>nope</html>`},
		{"truncated json", 200, `{"data":[`},
		{"unauthorized", 401, `{"error":"bad key"}`},
		{"forbidden", 403, ``},
		{"not found", 404, ``},
		{"method not allowed", 405, ``},
		{"server error", 500, `boom`},
		{"non-ascii ids", 200, `{"data":[{"id":"通义/qwen"}]}`},
	}

	encoded, err := json.Marshal(cases)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	command := exec.Command(pythonBin(t), "-c", script, repoRoot(t), string(encoded))
	command.Dir = repoRoot(t)
	output, err := command.Output()
	if err != nil {
		stderr := ""
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}
		t.Fatalf("python failed: %v\n%s", err, stderr)
	}
	var want []pythonVerdict
	if err := json.Unmarshal(output, &want); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}

	for index, testCase := range cases {
		transport := &fakeTransport{status: testCase.Status, body: testCase.Body}
		client := &Client{HTTP: transport}
		got, err := client.ListModels("ppio", "", "sk-test")
		if err != nil {
			t.Errorf("%s: Go refused what Python answered: %v", testCase.Name, err)
			continue
		}
		compareVerdict(t, testCase.Name, got, want[index])
		if len(got.Models) != len(want[index].Models) {
			t.Errorf("%s: models Go=%q Python=%q", testCase.Name, got.Models, want[index].Models)
			continue
		}
		for position := range got.Models {
			if got.Models[position] != want[index].Models[position] {
				t.Errorf("%s: models Go=%q Python=%q", testCase.Name, got.Models, want[index].Models)
				break
			}
		}
	}
}

func TestParityTheProbeRequestItselfMatchesPython(t *testing.T) {
	// The URL, method and headers decide whether the probe reaches the right route
	// at all. A probe that silently targets the wrong endpoint reports a protocol
	// as unsupported when the endpoint was never asked.
	script := `
import json, sys
sys.path.insert(0, sys.argv[1])
from oneagent import providers

results = []
for case in json.loads(sys.argv[2]):
    request = providers._protocol_request(
        case["protocol"], provider=case["provider"], custom_base=case["custom_base"],
        api_key="sk-test", model=case["model"],
    )
    results.append({
        "url": request.full_url,
        "method": request.method,
        "body": request.data.decode(),
        "headers": {key.lower(): value for key, value in request.headers.items()},
    })
print(json.dumps(results))
`
	type shape struct {
		Protocol   string `json:"protocol"`
		Provider   string `json:"provider"`
		CustomBase string `json:"custom_base"`
		Model      string `json:"model"`
	}
	cases := []shape{
		{"openai", "ppio", "", "gpt-5-mini"},
		{"responses", "ppio", "", "gpt-5-mini"},
		{"anthropic", "ppio", "", "claude-sonnet-4"},
		{"openai", "novita", "", "m"},
		{"anthropic", "novita", "", "m"},
		{"openai", "custom", "https://vendor.example/v1", "m"},
		{"responses", "custom", "https://vendor.example/v1/", "m"},
		{"anthropic", "custom", "https://vendor.example", "m"},
		{"openai", "custom", "https://vendor.example/openai/v1", "m"},
	}

	encoded, err := json.Marshal(cases)
	if err != nil {
		t.Fatalf("cannot encode: %v", err)
	}
	command := exec.Command(pythonBin(t), "-c", script, repoRoot(t), string(encoded))
	command.Dir = repoRoot(t)
	output, err := command.Output()
	if err != nil {
		stderr := ""
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = string(exitErr.Stderr)
		}
		t.Fatalf("python failed: %v\n%s", err, stderr)
	}
	var want []struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Body    string            `json:"body"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(output, &want); err != nil {
		t.Fatalf("cannot read python output: %v", err)
	}

	for index, testCase := range cases {
		transport := &fakeTransport{status: 200, body: "{}"}
		client := &Client{HTTP: transport}
		if _, err := client.Probe(ProbeRequest{
			Protocol: testCase.Protocol, Provider: testCase.Provider,
			CustomBase: testCase.CustomBase, APIKey: "sk-test", Model: testCase.Model,
		}); err != nil {
			t.Errorf("%v: %v", testCase, err)
			continue
		}
		if transport.request == nil {
			t.Errorf("%v: no request was sent", testCase)
			continue
		}
		if got := transport.request.URL.String(); got != want[index].URL {
			t.Errorf("%v url:\n  Go:     %s\n  Python: %s", testCase, got, want[index].URL)
		}
		if got := transport.request.Method; got != want[index].Method {
			t.Errorf("%v method: Go=%s Python=%s", testCase, got, want[index].Method)
		}
		// Compared as decoded values rather than bytes: Python's json.dumps puts a
		// space after the separator and Go's does not, which is not a difference
		// the endpoint can observe.
		if !sameJSON(t, bodyOf(t, transport.request), want[index].Body) {
			t.Errorf("%v body:\n  Go:     %s\n  Python: %s",
				testCase, bodyOf(t, transport.request), want[index].Body)
		}
		for name, value := range want[index].Headers {
			if got := transport.request.Header.Get(name); got != value {
				t.Errorf("%v header %s: Go=%q Python=%q", testCase, name, got, value)
			}
		}
	}
}

func TestParityTheModelReprInTheMessageMatchesPython(t *testing.T) {
	// The "does not support" message interpolates the model with !r, and I wrote
	// that renderer by hand. Backslashes and control characters are the shapes a
	// two-line version gets wrong.
	models := []string{
		"gpt-5-mini", "", "it's", `has"dq`, `both'and"dq`, "通义",
		`back\slash`, "new\nline", "tab\there", "carriage\rreturn",
		"null\x00byte", "bell\x07", "del\x7f", "'", `"`, `'"`,
	}
	want := runPython(t, "repr(value)", models)
	for index, model := range models {
		if got := pythonRepr(model); got != want[index] {
			t.Errorf("pythonRepr(%q):\n  Go:     %s\n  Python: %s", model, got, want[index])
		}
	}
}

// compareVerdict checks every field both implementations report.
func compareVerdict(t *testing.T, label string, got Verdict, want pythonVerdict) {
	t.Helper()
	if got.OK != want.OK {
		t.Errorf("%s: ok Go=%v Python=%v", label, got.OK, want.OK)
	}
	if got.Reachable != want.Reachable {
		t.Errorf("%s: reachable Go=%v Python=%v", label, got.Reachable, want.Reachable)
	}
	if got.Status != want.Status {
		t.Errorf("%s: status Go=%d Python=%d", label, got.Status, want.Status)
	}
	if got.Message != want.Message {
		t.Errorf("%s: message\n  Go:     %q\n  Python: %q", label, got.Message, want.Message)
	}
	wantCode := ""
	if want.ErrorCode != nil {
		wantCode = *want.ErrorCode
	}
	if got.ErrorCode != wantCode {
		t.Errorf("%s: error_code Go=%q Python=%q", label, got.ErrorCode, wantCode)
	}
	if got.Retryable != want.Retryable {
		t.Errorf("%s: retryable Go=%v Python=%v", label, got.Retryable, want.Retryable)
	}
	if want.Protocol != "" && got.Protocol != want.Protocol {
		t.Errorf("%s: protocol Go=%q Python=%q", label, got.Protocol, want.Protocol)
	}
}

func bodyOf(t *testing.T, request *http.Request) string {
	t.Helper()
	if request.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("cannot read the request body: %v", err)
	}
	return string(raw)
}

func sameJSON(t *testing.T, left, right string) bool {
	t.Helper()
	var first, second any
	if err := json.Unmarshal([]byte(left), &first); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(right), &second); err != nil {
		return false
	}
	leftEncoded, _ := json.Marshal(first)
	rightEncoded, _ := json.Marshal(second)
	return string(leftEncoded) == string(rightEncoded)
}
