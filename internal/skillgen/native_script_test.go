package skillgen

import (
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/modality"
)

// nativeProvider serves the MiniMax-style speech route and the async task
// routes Novita, PPIO and JieKou.AI document, over TLS: native routes must be
// https, and Node trusts the test certificate through NODE_EXTRA_CA_CERTS.
type nativeProvider struct {
	mu       sync.Mutex
	polls    int
	outcome  string // "succeed", "fail" or "pending"
	baseResp int
	bodies   map[string]map[string]any
	auth     map[string]string
}

func (f *nativeProvider) serve(server **httptest.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		f.mu.Lock()
		f.bodies[r.URL.Path] = body
		f.auth[r.URL.Path] = r.Header.Get("Authorization")
		outcome, baseResp := f.outcome, f.baseResp
		f.mu.Unlock()
		switch {
		case r.URL.Path == "/v3/minimax-speech-2.8-turbo":
			if baseResp != 0 {
				_, _ = w.Write([]byte(`{"base_resp":{"status_code":` + itoa(baseResp) + `,"status_msg":"refused for ` + r.Header.Get("Authorization") + `"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"audio":"494433","status":2},"base_resp":{"status_code":0,"status_msg":"success"}}`))
		case r.URL.Path == "/v3/async/wan2.7-t2v":
			_, _ = w.Write([]byte(`{"task_id":"task-123"}`))
		case r.URL.Path == "/v3/async/task-result":
			f.mu.Lock()
			f.polls++
			polls := f.polls
			f.mu.Unlock()
			if r.URL.Query().Get("task_id") != "task-123" {
				http.Error(w, `{"error":"unknown task"}`, http.StatusNotFound)
				return
			}
			switch {
			case outcome == "pending" || polls < 2:
				_, _ = w.Write([]byte(`{"task":{"task_id":"task-123","status":"TASK_STATUS_PROCESSING","eta":1}}`))
			case outcome == "fail":
				_, _ = w.Write([]byte(`{"task":{"task_id":"task-123","status":"TASK_STATUS_FAILED","reason":"content policy"}}`))
			default:
				_, _ = w.Write([]byte(`{"task":{"task_id":"task-123","status":"TASK_STATUS_SUCCEED"},"videos":[{"video_url":"` + (*server).URL + `/files/clip.mp4","video_url_ttl":"3600","video_type":"mp4"}]}`))
			}
		case r.URL.Path == "/files/clip.mp4":
			_, _ = w.Write([]byte("fake mp4 bytes"))
		default:
			http.NotFound(w, r)
		}
	}
}

func (f *nativeProvider) body(path string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, ok := f.bodies[path]
	return body, ok
}

func (f *nativeProvider) authFor(path string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.auth[path]
}

func (f *nativeProvider) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func (f *nativeProvider) pollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls
}

func (f *nativeProvider) set(change func(*nativeProvider)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func itoa(value int) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func nativeSetup(t *testing.T, spec Spec) (*nativeProvider, string, string, string, []string) {
	t.Helper()
	fake := &nativeProvider{outcome: "succeed", bodies: map[string]map[string]any{}, auth: map[string]string{}}
	var server *httptest.Server
	server = httptest.NewTLSServer(fake.serve(&server))
	t.Cleanup(server.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	spec.BaseURL, spec.NativeBaseURL = server.URL, server.URL
	dir := renderTo(t, spec)
	return fake, dir, resolvedTempDir(t), resolvedTempDir(t), []string{keyEnv, "NODE_EXTRA_CA_CERTS=" + ca}
}

var nativeSpeechSpec = Spec{Capability: modality.TextToSpeech, Adapter: modality.AdapterNovitaAudio, ProviderID: "local", ProviderName: "Local", Model: "minimax-speech-2.8-turbo"}

var videoSpec = Spec{Capability: modality.VideoGeneration, Adapter: modality.AdapterNovitaAsync, ProviderID: "local", ProviderName: "Local", Model: "wan2.7-t2v", Options: []string{"duration", "size", "negative_prompt"}}

func TestScriptSpeaksOnTheNativeRoute(t *testing.T) {
	fake, dir, work, home, env := nativeSetup(t, nativeSpeechSpec)
	run := runScript(t, dir, work, home, env, "speak", "--text", "Build passed", "--voice", "Chinese (Mandarin)_Lyrical_Voice", "--speed", "1.2", "--out", "done.mp3")
	if run.code != 0 || strings.TrimSpace(run.stdout) != filepath.Join(work, "done.mp3") {
		t.Fatalf("run = %#v", run)
	}
	if data, _ := os.ReadFile(filepath.Join(work, "done.mp3")); string(data) != "ID3" {
		t.Fatalf("hex audio was not decoded: %q", data)
	}
	body, _ := fake.body("/v3/minimax-speech-2.8-turbo")
	voice := body["voice_setting"].(map[string]any)
	if body["text"] != "Build passed" || voice["voice_id"] != "Chinese (Mandarin)_Lyrical_Voice" || voice["speed"] != 1.2 || body["output_format"] != "hex" {
		t.Fatalf("body = %#v", body)
	}
	if fake.authFor("/v3/minimax-speech-2.8-turbo") != "Bearer "+scriptKey {
		t.Fatal("native route did not get the key")
	}
	// A refusal in base_resp under HTTP 200 is still a refusal, and the
	// echoed key is scrubbed.
	for code, exit := range map[int]int{1004: 5, 1002: 6, 2013: 4} {
		fake.set(func(f *nativeProvider) { f.baseResp = code })
		run = runScript(t, dir, work, home, env, "speak", "--text", "hi")
		if run.code != exit || strings.Contains(run.stderr, scriptKey) {
			t.Errorf("base_resp %d: exit %d, stderr %q", code, run.code, run.stderr)
		}
	}
	if run := runScript(t, dir, work, home, env, "speak", "--text", "hi", "--format", "opus"); run.code != 2 {
		t.Fatalf("an OpenAI-only format was accepted: %#v", run)
	}
}

func TestScriptGeneratesVideoAndDownloadsWithoutTheKey(t *testing.T) {
	fake, dir, work, home, env := nativeSetup(t, videoSpec)
	run := runScript(t, dir, work, home, env, "video", "--prompt", "a paper plane over a city", "--duration", "5", "--size", "1280*720")
	if run.code != 0 || !strings.Contains(run.stderr, "task: task-123") {
		t.Fatalf("run = %#v", run)
	}
	saved := strings.TrimSpace(run.stdout)
	if !strings.HasPrefix(saved, filepath.Join(work, "bootagent-output")) || !strings.HasSuffix(saved, ".mp4") {
		t.Fatalf("saved = %q", saved)
	}
	if data, _ := os.ReadFile(saved); string(data) != "fake mp4 bytes" {
		t.Fatalf("video = %q", data)
	}
	if matches, _ := filepath.Glob(filepath.Join(work, "bootagent-output", "*.part")); len(matches) != 0 {
		t.Fatalf("partial files left: %v", matches)
	}
	body, _ := fake.body("/v3/async/wan2.7-t2v")
	if body["prompt"] != "a paper plane over a city" || body["duration"] != float64(5) || body["size"] != "1280*720" {
		t.Fatalf("submit body = %#v", body)
	}
	if polls := fake.pollCount(); polls < 2 {
		t.Fatalf("the task was not polled until done: %d polls", polls)
	}
	if fake.authFor("/files/clip.mp4") != "" {
		t.Fatal("the result URL was fetched with the key")
	}
	// An option the model does not document is refused before anything is
	// submitted.
	before := fake.requests()
	if run := runScript(t, dir, work, home, env, "video", "--prompt", "x", "--aspect-ratio", "16:9"); run.code != 2 {
		t.Fatalf("an undocumented option was accepted: %#v", run)
	}
	if fake.requests() != before {
		t.Fatal("a refused command still sent a request")
	}
}

func TestScriptReportsFailedAndPendingVideoTasks(t *testing.T) {
	fake, dir, work, home, env := nativeSetup(t, videoSpec)
	fake.set(func(f *nativeProvider) { f.outcome, f.polls = "fail", 1 })
	run := runScript(t, dir, work, home, env, "video", "--prompt", "x")
	if run.code != 4 || !strings.Contains(run.stderr, "content policy") {
		t.Fatalf("failed task = %#v", run)
	}
	fake.set(func(f *nativeProvider) { f.outcome = "pending" })
	run = runScript(t, dir, work, home, env, "video", "--prompt", "x", "--timeout", "2")
	if run.code != 7 || !strings.Contains(run.stderr, "video-result --task task-123") {
		t.Fatalf("pending task = %#v", run)
	}
	// Fetching later does not submit again.
	fake.set(func(f *nativeProvider) {
		f.outcome = "succeed"
		delete(f.bodies, "/v3/async/wan2.7-t2v")
	})
	run = runScript(t, dir, work, home, env, "video-result", "--task", "task-123")
	if run.code != 0 || !strings.HasSuffix(strings.TrimSpace(run.stdout), ".mp4") {
		t.Fatalf("video-result = %#v", run)
	}
	if _, resubmitted := fake.body("/v3/async/wan2.7-t2v"); resubmitted {
		t.Fatal("video-result submitted a new task")
	}
	if run := runScript(t, dir, work, home, env, "video-result", "--task", "../etc"); run.code != 2 {
		t.Fatalf("a malformed task id was accepted: %#v", run)
	}
}
