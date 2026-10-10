package skillgen

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/modality"
	"github.com/MaimoryLab/BootAgent/internal/skill"
)

func TestScriptRedactsSuccessfulTextResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		text := "Echo: " + r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text": text, "choices": []any{map[string]any{"message": map[string]any{"content": text}}},
		})
	}))
	defer server.Close()
	for _, capability := range []string{modality.Vision, modality.SpeechToText, modality.VideoUnderstanding} {
		t.Run(capability, func(t *testing.T) {
			adapter := map[string]string{modality.Vision: modality.AdapterChatVision, modality.SpeechToText: modality.AdapterAudioTranscriptions, modality.VideoUnderstanding: modality.AdapterFramesVision}[capability]
			dir := renderTo(t, Spec{Capability: capability, Adapter: adapter, ProviderID: "local", Model: "m", BaseURL: server.URL})
			work, home := resolvedTempDir(t), resolvedTempDir(t)
			env := []string{keyEnv}
			args := []string{"describe", "--image", "input.png"}
			input := "input.png"
			if capability == modality.SpeechToText {
				input, args = "input.wav", []string{"transcribe", "--audio", "input.wav"}
			} else if capability == modality.VideoUnderstanding {
				input, args = "input.mp4", []string{"describe-video", "--video", "input.mp4", "--frames", "1"}
				tools := fakeFFmpeg(t, filepath.Join(resolvedTempDir(t), "env"))
				env = append(env, "PATH="+tools+string(os.PathListSeparator)+os.Getenv("PATH"))
			}
			if err := os.WriteFile(filepath.Join(work, input), []byte("input"), 0o600); err != nil {
				t.Fatal(err)
			}
			run := runScript(t, dir, work, home, env, args...)
			if run.code != 0 || !strings.Contains(run.stdout, "[redacted]") || strings.Contains(run.stdout+run.stderr, scriptKey) {
				t.Fatalf("successful response was not redacted: exit %d, redaction present %t, key present %t", run.code, strings.Contains(run.stdout, "[redacted]"), strings.Contains(run.stdout+run.stderr, scriptKey))
			}
		})
	}
}

func TestScriptFlushesLongTranscriptsToAPipe(t *testing.T) {
	transcript := strings.Repeat("long transcript ", 20000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = json.NewEncoder(w).Encode(map[string]string{"text": transcript})
	}))
	defer server.Close()
	dir := renderTo(t, Spec{Capability: modality.SpeechToText, Adapter: modality.AdapterAudioTranscriptions, ProviderID: "local", Model: "m", BaseURL: server.URL})
	work, home := resolvedTempDir(t), resolvedTempDir(t)
	if err := os.WriteFile(filepath.Join(work, "input.wav"), []byte("RIFF WAVE"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := runScript(t, dir, work, home, []string{keyEnv}, "transcribe", "--audio", "input.wav")
	want := strings.TrimSpace(transcript) + "\n"
	if run.code != 0 || run.stdout != want {
		t.Fatalf("transcript: exit %d, received %d bytes, want %d bytes", run.code, len(run.stdout), len(want))
	}
}

func TestScriptDefaultOutputPreservesTheSkillTree(t *testing.T) {
	for _, capability := range []string{modality.ImageGeneration, modality.TextToSpeech} {
		t.Run(capability, func(t *testing.T) {
			adapter := modality.AdapterImages
			args := []string{"generate", "--prompt", "a lighthouse"}
			if capability == modality.TextToSpeech {
				adapter, args = modality.AdapterAudioSpeech, []string{"speak", "--text", "hello"}
			}
			_, dir, _, home := scriptSetup(t, capability, adapter, "m")
			before, err := skill.HashTree(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			// The Skill may be invoked from its own directory by an Agent.
			run := runScript(t, dir, dir, home, []string{keyEnv}, args...)
			if run.code != 0 {
				t.Fatalf("run = %#v", run)
			}
			for _, path := range strings.Split(strings.TrimSpace(run.stdout), "\n") {
				if !strings.HasPrefix(path, filepath.Join(home, ".bootagent", "output")+string(os.PathSeparator)) {
					t.Errorf("default output is outside the user's output folder: %s", path)
				}
			}
			after, err := skill.HashTree(context.Background(), dir)
			if err != nil || before != after {
				t.Fatalf("generation changed the managed Skill tree: before %#v, after %#v, error %v", before, after, err)
			}
		})
	}
}

func TestScriptConcurrentVideosKeepBothResults(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	var mu sync.Mutex
	downloads := 0
	ready := make(chan struct{})
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/async/task-result" {
			task := r.URL.Query().Get("task_id")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task":   map[string]string{"status": "TASK_STATUS_SUCCEED"},
				"videos": []any{map[string]string{"video_url": server.URL + "/files/" + task, "video_type": "mp4"}},
			})
			return
		}
		mu.Lock()
		downloads++
		if downloads == 2 {
			close(ready)
		}
		mu.Unlock()
		select {
		case <-ready:
		case <-time.After(3 * time.Second):
			http.Error(w, "the other download did not start", http.StatusGatewayTimeout)
			return
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(r.URL.Path + ":first"))
		w.(http.Flusher).Flush()
		// Keep both downloads open so each writer has begun before either finishes.
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(":last"))
	}))
	defer server.Close()
	ca := filepath.Join(resolvedTempDir(t), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := videoSpec
	spec.BaseURL, spec.NativeBaseURL = server.URL, server.URL
	dir := renderTo(t, spec)
	work, home := resolvedTempDir(t), resolvedTempDir(t)
	outputDir := filepath.Join(home, ".bootagent", "output")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(outputDir, "video-20261010-000000.mp4")
	if err := os.WriteFile(existing, []byte("existing video"), 0o600); err != nil {
		t.Fatal(err)
	}
	clock := filepath.Join(resolvedTempDir(t), "clock.cjs")
	if err := os.WriteFile(clock, []byte(`Date.prototype.toISOString = () => "2026-10-10T00:00:00.000Z";`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{keyEnv, "NODE_EXTRA_CA_CERTS=" + ca, "NODE_OPTIONS=--require " + strconv.Quote(clock)}
	results := make(chan scriptRun, 2)
	for _, task := range []string{"task-A", "task-B"} {
		go func(task string) {
			results <- runScript(t, dir, work, home, env, "video-result", "--task", task)
		}(task)
	}
	paths := map[string]bool{}
	contents := map[string]bool{}
	for range 2 {
		run := <-results
		if run.code != 0 {
			t.Errorf("concurrent download: exit %d, stderr %s", run.code, run.stderr)
			continue
		}
		path := strings.TrimSpace(run.stdout)
		paths[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		contents[string(data)] = true
	}
	if len(paths) != 2 || !contents["/files/task-A:first:last"] || !contents["/files/task-B:first:last"] {
		t.Errorf("both distinct videos were not preserved: %d paths, content keys %v", len(paths), contents)
	}
	if data, err := os.ReadFile(existing); err != nil || string(data) != "existing video" {
		t.Errorf("existing video was replaced: %q, %v", data, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(home, ".bootagent", "output", ".bootagent-output-*")); len(matches) != 0 {
		t.Errorf("temporary download directories left behind: %v", matches)
	}
}

func TestScriptExplicitSpeechOutputKeepsAnExistingFile(t *testing.T) {
	_, dir, work, home := scriptSetup(t, modality.TextToSpeech, modality.AdapterAudioSpeech, "tts-1")
	existing := filepath.Join(work, "done.mp3")
	if err := os.WriteFile(existing, []byte("existing audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := runScript(t, dir, work, home, []string{keyEnv}, "speak", "--text", "hello", "--out", "done.mp3")
	if run.code != 0 || strings.TrimSpace(run.stdout) != filepath.Join(work, "done-2.mp3") {
		t.Fatalf("explicit output = %#v", run)
	}
	if data, err := os.ReadFile(existing); err != nil || string(data) != "existing audio" {
		t.Fatalf("existing audio was replaced: %q, %v", data, err)
	}
}
