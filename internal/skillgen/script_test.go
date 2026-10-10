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
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/modality"
)

const scriptKey = "sk-script-test-secret"

// fakeProvider records requests and answers each route the way an
// OpenAI-compatible gateway does. Error bodies echo the key, as some gateways
// do, so the tests prove the script scrubs it.
type fakeProvider struct {
	mu       sync.Mutex
	requests []recorded
	status   int
}

type recorded struct {
	path, auth, contentType string
	body                    []byte
}

func (f *fakeProvider) handler(t *testing.T, server **httptest.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, recorded{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body})
		status := f.status
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"refused for ` + r.Header.Get("Authorization") + `"}}`))
			return
		}
		switch r.URL.Path {
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"A red error banner."}}]}`))
		case "/v1/images/generations":
			png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA6fptVAAAACklEQVR4nGNgAAAAAgABSK+kcQAAAABJRU5ErkJggg==")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{
				{"b64_json": base64.StdEncoding.EncodeToString(png)},
				{"url": (*server).URL + "/files/result.png"},
			}})
		case "/files/result.png":
			_, _ = w.Write([]byte("\xff\xd8\xff downloaded jpeg"))
		case "/v1/audio/transcriptions":
			_, _ = w.Write([]byte(`{"text":"hello there"}`))
		case "/v1/audio/speech":
			_, _ = w.Write([]byte("ID3 fake mp3 bytes"))
		default:
			http.NotFound(w, r)
		}
	}
}

func (f *fakeProvider) last(path string) (recorded, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := len(f.requests) - 1; index >= 0; index-- {
		if f.requests[index].path == path {
			return f.requests[index], true
		}
	}
	return recorded{}, false
}

type scriptRun struct {
	stdout, stderr string
	code           int
}

// runScript runs the generated script with argv and a controlled environment:
// only PATH, HOME and what the test passes.
func runScript(t *testing.T, dir, workdir, home string, extraEnv []string, args ...string) scriptRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, node, append([]string{filepath.Join(dir, "scripts", "run.mjs")}, args...)...)
	command.Dir = workdir
	command.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "USERPROFILE=" + home}, extraEnv...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err = command.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return scriptRun{stdout.String(), stderr.String(), code}
}

func scriptSetup(t *testing.T, capability, adapter, model string) (*fakeProvider, string, string, string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	fake := &fakeProvider{}
	var server *httptest.Server
	server = httptest.NewServer(fake.handler(t, &server))
	t.Cleanup(server.Close)
	dir := renderTo(t, Spec{Capability: capability, Adapter: adapter, ProviderID: "local", ProviderName: "Local", Model: model, BaseURL: server.URL})
	return fake, dir, resolvedTempDir(t), resolvedTempDir(t)
}

// resolvedTempDir resolves symlinks in the temp path (macOS puts /var under
// /private/var), because Node reports the resolved working directory.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

var keyEnv = "BOOTAGENT_LOCAL_API_KEY=" + scriptKey

func TestScriptDescribesAnImage(t *testing.T) {
	fake, dir, work, home := scriptSetup(t, modality.Vision, modality.AdapterChatVision, "vision-model")
	image := filepath.Join(work, "shot.png")
	if err := os.WriteFile(image, []byte("\x89PNG\r\n\x1a\nfake"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := runScript(t, dir, work, home, []string{keyEnv}, "describe", "--image", "shot.png", "--prompt", "What error is shown?")
	if run.code != 0 || run.stdout != "A red error banner.\n" {
		t.Fatalf("run = %#v", run)
	}
	request, _ := fake.last("/v1/chat/completions")
	if request.auth != "Bearer "+scriptKey {
		t.Fatalf("auth = %q", request.auth)
	}
	var body struct {
		Model    string `json:"model"`
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(request.body, &body); err != nil {
		t.Fatal(err)
	}
	parts := body.Messages[0].Content
	if body.Model != "vision-model" || parts[0]["text"] != "What error is shown?" || !strings.HasPrefix(parts[1]["image_url"].(map[string]any)["url"].(string), "data:image/png;base64,") {
		t.Fatalf("body = %s", request.body)
	}
}

func TestScriptGeneratesImagesAndDownloadsWithoutTheKey(t *testing.T) {
	fake, dir, work, home := scriptSetup(t, modality.ImageGeneration, modality.AdapterImages, "gpt-image-1")
	run := runScript(t, dir, work, home, []string{keyEnv}, "generate", "--prompt", "a lighthouse", "--n", "2")
	if run.code != 0 {
		t.Fatalf("run = %#v", run)
	}
	paths := strings.Split(strings.TrimSpace(run.stdout), "\n")
	if len(paths) != 2 || !strings.HasSuffix(paths[0], ".png") || !strings.HasSuffix(paths[1], ".jpg") {
		t.Fatalf("saved = %q", paths)
	}
	for _, path := range paths {
		if !strings.HasPrefix(path, filepath.Join(home, ".bootagent", "output")+string(os.PathSeparator)) {
			t.Fatalf("%s is outside the user's output folder", path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	// The result URL may be on another host: the key must not go with it.
	download, ok := fake.last("/files/result.png")
	if !ok || download.auth != "" {
		t.Fatalf("download request = %#v", download)
	}
	// A second run never overwrites the first.
	again := runScript(t, dir, work, home, []string{keyEnv}, "generate", "--prompt", "a lighthouse")
	if again.code != 0 || again.stdout == run.stdout {
		t.Fatalf("second run = %#v", again)
	}
}

func TestScriptTranscribesAndSpeaks(t *testing.T) {
	fake, dir, work, home := scriptSetup(t, modality.SpeechToText, modality.AdapterAudioTranscriptions, "whisper-1")
	if err := os.WriteFile(filepath.Join(work, "note.wav"), []byte("RIFF....WAVE"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := runScript(t, dir, work, home, []string{keyEnv}, "transcribe", "--audio", "note.wav", "--language", "en")
	if run.code != 0 || run.stdout != "hello there\n" {
		t.Fatalf("run = %#v", run)
	}
	request, _ := fake.last("/v1/audio/transcriptions")
	if !strings.HasPrefix(request.contentType, "multipart/form-data") || !bytes.Contains(request.body, []byte("whisper-1")) || !bytes.Contains(request.body, []byte("note.wav")) {
		t.Fatalf("transcription request = %q", request.body)
	}

	_, speechDir, speechWork, speechHome := scriptSetup(t, modality.TextToSpeech, modality.AdapterAudioSpeech, "tts-1")
	spoken := runScript(t, speechDir, speechWork, speechHome, []string{keyEnv}, "speak", "--text", "Build passed", "--out", "done.mp3")
	if spoken.code != 0 || strings.TrimSpace(spoken.stdout) != filepath.Join(speechWork, "done.mp3") {
		t.Fatalf("speak = %#v", spoken)
	}
	if data, _ := os.ReadFile(filepath.Join(speechWork, "done.mp3")); string(data) != "ID3 fake mp3 bytes" {
		t.Fatalf("audio = %q", data)
	}
}

func TestScriptReadsTheFallbackFileOnlyWhenTheVariableIsUnset(t *testing.T) {
	fake, dir, work, home := scriptSetup(t, modality.TextToSpeech, modality.AdapterAudioSpeech, "tts-1")
	if err := os.MkdirAll(filepath.Join(home, ".bootagent", "skill-env"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".bootagent", "skill-env", "local.env"), []byte("BOOTAGENT_LOCAL_API_KEY='sk-from-\\'file'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if run := runScript(t, dir, work, home, nil, "speak", "--text", "hi"); run.code != 0 {
		t.Fatalf("fallback run = %#v", run)
	}
	if request, _ := fake.last("/v1/audio/speech"); request.auth != "Bearer sk-from-'file" {
		t.Fatalf("fallback auth = %q", request.auth)
	}
	if run := runScript(t, dir, work, home, []string{keyEnv}, "speak", "--text", "hi"); run.code != 0 {
		t.Fatalf("env run = %#v", run)
	}
	if request, _ := fake.last("/v1/audio/speech"); request.auth != "Bearer "+scriptKey {
		t.Fatalf("the variable did not win over the file: %q", request.auth)
	}
}

func TestScriptExitCodesAndRedaction(t *testing.T) {
	fake, dir, work, home := scriptSetup(t, modality.Vision, modality.AdapterChatVision, "vision-model")
	if err := os.WriteFile(filepath.Join(work, "a.png"), []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		status int
		env    []string
		args   []string
		code   int
	}{
		{"no key", 0, nil, []string{"describe", "--image", "a.png"}, 3},
		{"missing image", 0, []string{keyEnv}, []string{"describe", "--image", "nope.png"}, 2},
		{"wrong subcommand", 0, []string{keyEnv}, []string{"generate", "--prompt", "x"}, 2},
		{"refused", http.StatusBadRequest, []string{keyEnv}, []string{"describe", "--image", "a.png"}, 4},
		{"key rejected", http.StatusUnauthorized, []string{keyEnv}, []string{"describe", "--image", "a.png"}, 5},
		{"busy", http.StatusServiceUnavailable, []string{keyEnv}, []string{"describe", "--image", "a.png"}, 6},
	}
	for _, test := range cases {
		fake.mu.Lock()
		fake.status = test.status
		fake.mu.Unlock()
		run := runScript(t, dir, work, home, test.env, test.args...)
		if run.code != test.code {
			t.Errorf("%s: exit %d, want %d (%s)", test.name, run.code, test.code, run.stderr)
		}
		if strings.Contains(run.stdout+run.stderr, scriptKey) {
			t.Errorf("%s: output carries the key: %s", test.name, run.stdout+run.stderr)
		}
		if test.status != 0 && !strings.Contains(run.stderr, "[redacted]") {
			t.Errorf("%s: the provider's echo was not shown redacted: %s", test.name, run.stderr)
		}
	}
	// check confirms setup without sending anything.
	fake.mu.Lock()
	before := len(fake.requests)
	fake.mu.Unlock()
	if run := runScript(t, dir, work, home, []string{keyEnv}, "check"); run.code != 0 || !strings.Contains(run.stdout, "ready") || strings.Contains(run.stdout, scriptKey) {
		t.Fatalf("check = %#v", run)
	}
	fake.mu.Lock()
	after := len(fake.requests)
	fake.mu.Unlock()
	if after != before {
		t.Fatal("check sent a request")
	}
}
