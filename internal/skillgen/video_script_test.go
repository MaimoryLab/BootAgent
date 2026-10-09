package skillgen

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/modality"
)

// fakeFFmpeg is a shell script standing in for ffmpeg: asked for details it
// prints a Duration line and fails, as ffmpeg does with no output; asked for a
// frame it writes a tiny JPEG. It records its environment so the test can
// prove the key never reaches it.
func fakeFFmpeg(t *testing.T, envDump string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake ffmpeg is a shell script")
	}
	dir := resolvedTempDir(t)
	script := `#!/bin/sh
env >> '` + envDump + `'
for arg in "$@"; do last="$arg"; done
case " $* " in
  *" -frames:v "*) printf '\377\330\377\340fakejpeg' > "$last"; exit 0 ;;
  *) echo "  Duration: 00:00:10.00, start: 0.000000, bitrate: 100 kb/s" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

var framesSpec = Spec{Capability: modality.VideoUnderstanding, Adapter: modality.AdapterFramesVision, ProviderID: "local", ProviderName: "Local", Model: "vision-model"}

func TestScriptDescribesAVideoFromSampledFrames(t *testing.T) {
	fake, dir, work, home := scriptSetup(t, framesSpec.Capability, framesSpec.Adapter, framesSpec.Model)
	envDump := filepath.Join(resolvedTempDir(t), "ffmpeg-env")
	tools := fakeFFmpeg(t, envDump)
	if err := os.WriteFile(filepath.Join(work, "clip.mp4"), []byte("not really a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := "PATH=" + tools + string(os.PathListSeparator) + os.Getenv("PATH")
	run := runScript(t, dir, work, home, []string{keyEnv, path}, "describe-video", "--video", "clip.mp4", "--frames", "4", "--prompt", "Where does the error appear?")
	if run.code != 0 || run.stdout != "A red error banner.\n" {
		t.Fatalf("run = %#v", run)
	}
	request, _ := fake.last("/v1/chat/completions")
	var body struct {
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(request.body, &body); err != nil {
		t.Fatal(err)
	}
	parts := body.Messages[0].Content
	if len(parts) != 5 {
		t.Fatalf("want a text part and 4 frames, got %d parts", len(parts))
	}
	text, _ := parts[0]["text"].(string)
	if !strings.Contains(text, "Where does the error appear?") || !strings.Contains(text, "10.0 s video") || !strings.Contains(text, "1.3 s, 3.8 s, 6.3 s, 8.8 s") {
		t.Fatalf("preface = %q", text)
	}
	if url := parts[1]["image_url"].(map[string]any)["url"].(string); !strings.HasPrefix(url, "data:image/jpeg;base64,") {
		t.Fatalf("frame = %q", url)
	}
	// ffmpeg runs with PATH alone: never the key, never its variable name.
	dump, err := os.ReadFile(envDump)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(dump), scriptKey) || strings.Contains(string(dump), "BOOTAGENT_") {
		t.Fatalf("ffmpeg saw the key:\n%s", dump)
	}
	// The temporary frame directory is gone; ffmpeg wrote its frames there.
	if matches, _ := filepath.Glob(filepath.Join(os.TempDir(), "bootagent-frames-*", "frame-*.jpg")); len(matches) != 0 {
		t.Fatalf("frames left behind: %v", matches)
	}
}

func TestScriptVideoNeedsFFmpeg(t *testing.T) {
	_, dir, work, home := scriptSetup(t, framesSpec.Capability, framesSpec.Adapter, framesSpec.Model)
	if err := os.WriteFile(filepath.Join(work, "clip.mp4"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	node, _ := exec.LookPath("node")
	// A PATH holding only node: no ffmpeg anywhere.
	only := resolvedTempDir(t)
	if err := os.Symlink(node, filepath.Join(only, "node")); err != nil {
		t.Fatal(err)
	}
	run := runScript(t, dir, work, home, []string{keyEnv, "PATH=" + only}, "describe-video", "--video", "clip.mp4")
	if run.code != 3 || !strings.Contains(run.stderr, "ffmpeg was not found") {
		t.Fatalf("run = %#v", run)
	}
	if run := runScript(t, dir, work, home, []string{keyEnv}, "describe-video", "--video", "missing.mp4"); run.code != 2 {
		t.Fatalf("missing file = %#v", run)
	}
	if run := runScript(t, dir, work, home, []string{keyEnv}, "describe-video", "--video", "clip.mp4", "--frames", "40"); run.code != 2 {
		t.Fatalf("too many frames = %#v", run)
	}
}
