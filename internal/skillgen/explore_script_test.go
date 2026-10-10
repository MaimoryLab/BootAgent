package skillgen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/modality"
	"github.com/MaimoryLab/BootAgent/internal/skill"
)

// exploreProvider stands in for a gateway whose calling convention BootAgent
// does not know. Error bodies echo the credential headers, as some gateways
// do, so the tests prove the script scrubs them.
type exploreProvider struct {
	mu        sync.Mutex
	requests  []exploreRequest
	elsewhere string
}

type exploreRequest struct {
	method, path, auth, apiKey, contentType string
	body                                    []byte
}

func (f *exploreProvider) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, exploreRequest{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("X-Api-Key"), r.Header.Get("Content-Type"), body})
	elsewhere := f.elsewhere
	f.mu.Unlock()
	switch r.URL.Path {
	case "/v3/gpt-image-2":
		// A PNG large enough to be taken out of the JSON, and a short field
		// that stays.
		png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 2048)...)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"image": base64.StdEncoding.EncodeToString(png), "id": "img-1"}})
	case "/v3/speech":
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3 fake mp3 bytes"))
	case "/v3/redirect":
		http.Redirect(w, r, elsewhere+"/steal", http.StatusFound)
	case "/v3/refuse":
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad body for ` + r.Header.Get("Authorization") + r.Header.Get("X-Api-Key") + `"}}`))
	case "/v3/unauthorized":
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"no"}`))
	case "/files/result.png":
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\nresult"))
	default:
		http.NotFound(w, r)
	}
}

func (f *exploreProvider) all() []exploreRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]exploreRequest(nil), f.requests...)
}

// exploreSetup renders an explore Skill for a Provider at a local server, and
// a second server standing for any other host.
func exploreSetup(t *testing.T) (*exploreProvider, *exploreProvider, string, string, string) {
	t.Helper()
	fake, other := &exploreProvider{}, &exploreProvider{}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	elsewhere := httptest.NewServer(http.HandlerFunc(other.handler))
	t.Cleanup(server.Close)
	t.Cleanup(elsewhere.Close)
	fake.elsewhere = elsewhere.URL
	dir := renderTo(t, Spec{Capability: modality.ImageGeneration, Adapter: modality.AdapterExplore, ProviderID: "local", ProviderName: "Local", Model: "gpt-image-2", BaseURL: server.URL, ProviderHome: "https://docs.example"})
	return fake, other, dir, resolvedTempDir(t), resolvedTempDir(t)
}

func noKey(t *testing.T, run scriptRun) {
	t.Helper()
	if strings.Contains(run.stdout+run.stderr, scriptKey) {
		t.Fatalf("the key reached the output: %#v", run)
	}
}

func TestExploreSendsTheRequestAndSavesMediaFromJSON(t *testing.T) {
	fake, _, dir, work, home := exploreSetup(t)
	if err := os.WriteFile(filepath.Join(work, "body.json"), []byte(`{"model":"gpt-image-2","prompt":"a cat"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	run := runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", "/v3/gpt-image-2", "--json", "body.json", "--query", "async=false")
	noKey(t, run)
	if run.code != 0 || !strings.HasPrefix(run.stdout, "HTTP 200 application/json\n") || !strings.Contains(run.stdout, `"id": "img-1"`) {
		t.Fatalf("run = %#v", run)
	}
	requests := fake.all()
	if len(requests) != 1 || requests[0].method != "POST" || requests[0].path != "/v3/gpt-image-2?async=false" || requests[0].auth != "Bearer "+scriptKey || requests[0].contentType != "application/json" || string(requests[0].body) != `{"model":"gpt-image-2","prompt":"a cat"}` {
		t.Fatalf("requests = %#v", requests)
	}
	output := filepath.Join(home, ".bootagent", "output") + string(os.PathSeparator)
	start := strings.Index(run.stdout, "[saved to ")
	if start < 0 {
		t.Fatalf("the image was not taken out of the JSON: %s", run.stdout)
	}
	saved := strings.SplitN(run.stdout[start+len("[saved to "):], "]", 2)[0]
	if !strings.HasPrefix(saved, output) || !strings.HasSuffix(saved, ".png") {
		t.Fatalf("saved = %q", saved)
	}
	if data, _ := os.ReadFile(saved); !bytes.HasPrefix(data, []byte("\x89PNG")) {
		t.Fatalf("saved image = %q", data[:8])
	}
}

func TestExploreSendsTheKeyAsTheHeaderAuthNames(t *testing.T) {
	fake, _, dir, work, home := exploreSetup(t)
	run := runScript(t, dir, work, home, []string{keyEnv}, "request", "--method", "GET", "--path", "/v3/gpt-image-2", "--auth", "header:X-Api-Key")
	noKey(t, run)
	requests := fake.all()
	if run.code != 0 || len(requests) != 1 || requests[0].apiKey != scriptKey || requests[0].auth != "" {
		t.Fatalf("run = %#v, requests = %#v", run, requests)
	}
	run = runScript(t, dir, work, home, []string{keyEnv}, "request", "--method", "GET", "--path", "/v3/gpt-image-2", "--auth", "none")
	if requests = fake.all(); run.code != 0 || requests[1].apiKey != "" || requests[1].auth != "" {
		t.Fatalf("auth none sent a key: %#v", requests[1])
	}
}

func TestExploreNeverLeavesTheProviderOrigin(t *testing.T) {
	fake, other, dir, work, home := exploreSetup(t)
	host := strings.TrimPrefix(fake.elsewhere, "http://")
	for _, path := range []string{"//" + host + "/steal", `/\` + host + "/steal", fake.elsewhere + "/steal", "steal", "/v3/a b"} {
		run := runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", path)
		if run.code != 2 {
			t.Fatalf("--path %q: %#v", path, run)
		}
	}
	for _, header := range []string{"Authorization=Bearer x", "cookie=a=b", "X-Api-Key=x", "Host=evil"} {
		if run := runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", "/v3/gpt-image-2", "--header", header); run.code != 2 {
			t.Fatalf("--header %q: %#v", header, run)
		}
	}
	if run := runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", "/v3/gpt-image-2", "--auth", "header:Cookie"); run.code != 2 {
		t.Fatalf("--auth header:Cookie: %#v", run)
	}
	// A redirect is reported, not followed, so the key never goes with it.
	run := runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", "/v3/redirect")
	if run.code != 4 || !strings.Contains(run.stdout, "HTTP 302") || !strings.Contains(run.stdout, "Location: "+fake.elsewhere+"/steal") {
		t.Fatalf("redirect: %#v", run)
	}
	if len(fake.all()) != 1 || len(other.all()) != 0 {
		t.Fatalf("requests: provider %#v, elsewhere %#v", fake.all(), other.all())
	}
}

func TestExploreReportsRefusalsWithoutTheKey(t *testing.T) {
	_, _, dir, work, home := exploreSetup(t)
	run := runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", "/v3/refuse")
	noKey(t, run)
	if run.code != 4 || !strings.Contains(run.stdout, "HTTP 400") || !strings.Contains(run.stdout, "[redacted]") {
		t.Fatalf("refusal: %#v", run)
	}
	run = runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", "/v3/refuse", "--auth", "header:X-Api-Key")
	noKey(t, run)
	if run.code != 4 {
		t.Fatalf("refusal with a header key: %#v", run)
	}
	if run := runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", "/v3/unauthorized"); run.code != 5 || !strings.Contains(run.stderr, "--auth") {
		t.Fatalf("401: %#v", run)
	}
}

func TestExploreSavesBinaryResponsesAndSendsMultipart(t *testing.T) {
	fake, _, dir, work, home := exploreSetup(t)
	run := runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", "/v3/speech", "--json", writeFile(t, work, "speech.json", `{"text":"hi"}`))
	if run.code != 0 || !strings.Contains(run.stdout, "HTTP 200 audio/mpeg") || !strings.Contains(run.stdout, ".mp3]") {
		t.Fatalf("binary: %#v", run)
	}
	audio := writeFile(t, work, "clip.wav", "RIFF....WAVEfmt ")
	run = runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", "/v3/gpt-image-2", "--form", "model=whisper-1", "--file", "file="+audio)
	requests := fake.all()
	last := requests[len(requests)-1]
	if run.code != 0 || !strings.HasPrefix(last.contentType, "multipart/form-data") || !bytes.Contains(last.body, []byte("whisper-1")) || !bytes.Contains(last.body, []byte("WAVEfmt")) {
		t.Fatalf("multipart: %#v, %#v", run, last)
	}
	if run := runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", "/v3/speech", "--json", "speech.json", "--form", "a=b"); run.code != 2 {
		t.Fatalf("--json with --form: %#v", run)
	}
}

func TestExploreFetchesResultsWithoutTheKey(t *testing.T) {
	fake, _, dir, work, home := exploreSetup(t)
	origin := providerOriginOf(t, dir)
	run := runScript(t, dir, work, home, []string{keyEnv}, "fetch-result", "--url", origin+"/files/result.png")
	requests := fake.all()
	if run.code != 0 || !strings.HasSuffix(strings.TrimSpace(run.stdout), ".png") || len(requests) != 1 || requests[0].auth != "" {
		t.Fatalf("fetch-result: %#v, %#v", run, requests)
	}
	// Plain http is allowed only on the Provider's own origin.
	if run := runScript(t, dir, work, home, nil, "fetch-result", "--url", fake.elsewhere+"/files/result.png"); run.code != 2 {
		t.Fatalf("http elsewhere: %#v", run)
	}
}

func TestExploreNotesLiveOutsideTheSkillAndDropTheKey(t *testing.T) {
	_, _, dir, work, home := exploreSetup(t)
	before, err := skill.HashTree(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	// The helpers run without a key.
	if run := runScript(t, dir, work, home, nil, "notes"); run.code != 0 || !strings.Contains(run.stdout, "No notes yet") {
		t.Fatalf("notes: %#v", run)
	}
	notes := writeFile(t, work, "notes.md", "POST /v3/gpt-image-2 with Bearer "+scriptKey+"\nother key sk-abcdefghijklmnopqrstuvwx\n")
	run := runScript(t, dir, work, home, []string{keyEnv}, "save-notes", "--from", notes)
	stored := filepath.Join(home, ".bootagent", "skill-notes", "local-image-generation.md")
	data, err := os.ReadFile(stored)
	if run.code != 0 || err != nil || strings.Contains(string(data), scriptKey) || strings.Contains(string(data), "sk-abcdefghij") || !strings.Contains(string(data), "POST /v3/gpt-image-2") {
		t.Fatalf("save-notes: %#v, %q, %v", run, data, err)
	}
	if run := runScript(t, dir, work, home, nil, "notes"); run.code != 0 || !strings.Contains(run.stdout, "POST /v3/gpt-image-2") {
		t.Fatalf("notes after saving: %#v", run)
	}
	after, err := skill.HashTree(context.Background(), dir)
	if err != nil || after.Hash != before.Hash {
		t.Fatalf("the notes changed the Skill tree: %v", err)
	}
}

func TestExploreRefusesOtherSubcommandsAndUnknownFlags(t *testing.T) {
	_, _, dir, work, home := exploreSetup(t)
	if run := runScript(t, dir, work, home, []string{keyEnv}, "generate", "--prompt", "x"); run.code != 2 {
		t.Fatalf("generate on an explore Skill: %#v", run)
	}
	if run := runScript(t, dir, work, home, []string{keyEnv}, "request", "--path", "/v3/gpt-image-2", "--url", "x"); run.code != 2 {
		t.Fatalf("unknown flag: %#v", run)
	}
	if run := runScript(t, dir, work, home, nil, "request", "--path", "/v3/gpt-image-2"); run.code != 3 {
		t.Fatalf("request without a key: %#v", run)
	}
	if run := runScript(t, dir, work, home, nil, "help"); run.code != 0 || !strings.Contains(run.stdout, "run.mjs request") || !strings.Contains(run.stdout, "run.mjs save-notes") {
		t.Fatalf("help: %#v", run)
	}
}

// SKILL.md documents the commands as the script parses them.
func TestExploreSkillDocumentsTheScriptsFlags(t *testing.T) {
	_, _, dir, _, _ := exploreSetup(t)
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"run.mjs notes", "run.mjs request --method POST --path", "run.mjs fetch-result --url", "run.mjs save-notes --from", "--auth", "~/.bootagent/skill-notes/local-image-generation.md", "https://docs.example"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("SKILL.md does not mention %q", want)
		}
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func providerOriginOf(t *testing.T, dir string) string {
	t.Helper()
	cfg, err := ReadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	return originOf(cfg.BaseURL)
}
