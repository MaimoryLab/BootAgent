package app

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/platform"
)

// registryDoer answers any package with one version and records what was asked.
type registryDoer struct {
	mu       sync.Mutex
	asked    []string
	calls    atomic.Int32
	inFlight atomic.Int32
	peak     atomic.Int32
	version  string
	status   int
}

func (d *registryDoer) Do(request *http.Request) (*http.Response, error) {
	d.calls.Add(1)
	if current := d.inFlight.Add(1); current > d.peak.Load() {
		d.peak.Store(current)
	}
	defer d.inFlight.Add(-1)
	d.mu.Lock()
	d.asked = append(d.asked, request.URL.Path)
	d.mu.Unlock()
	status := d.status
	if status == 0 {
		status = http.StatusOK
	}
	version := d.version
	if version == "" {
		version = "9.9.9"
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(`{"dist-tags":{"latest":"` + version + `"}}`)),
	}, nil
}

func (d *registryDoer) paths() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.asked...)
}

// statusWithRegistry builds a use case whose codex and npm commands resolve, so
// exactly one npm Agent counts as installed.
func statusWithRegistry(t *testing.T, doer *registryDoer, installed ...string) *UseCases {
	t.Helper()
	present := map[string]bool{"npm": true}
	for _, command := range installed {
		present[command] = true
	}
	core := NewUseCases(StatusOptions{
		Home:     t.TempDir(),
		Platform: platform.For("linux", "amd64"),
		Lookup: func(command string) (string, bool) {
			if present[command] {
				return "/fake/" + command, true
			}
			return "", false
		},
	})
	core.SetRuntimeDownloader(doer)
	return core
}

func TestStatusReportsTheRegistryVersionForInstalledNPMAgents(t *testing.T) {
	doer := &registryDoer{version: "1.4.0"}
	status, err := statusWithRegistry(t, doer, "codex").GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	latest := status.Agents["codex"].LatestVersion
	if latest == nil || *latest != "1.4.0" {
		t.Fatalf("codex latestVersion = %v", latest)
	}
	// Only what is installed is asked about: there is nothing to update on a
	// machine that does not have the Agent, and asking anyway would spend a
	// request per catalog entry on every poll.
	if paths := doer.paths(); len(paths) != 1 || !strings.Contains(paths[0], "@openai/codex") {
		t.Fatalf("registry was asked for %v, want only @openai/codex", paths)
	}
	if opencode := status.Agents["opencode"].LatestVersion; opencode != nil {
		t.Fatalf("uninstalled Agent carried a latestVersion: %v", opencode)
	}
}

func TestStatusLeavesLatestVersionUnsetWhenTheRegistryCannotAnswer(t *testing.T) {
	// Offline, rate limited, or behind a captive portal: the dot is a decoration,
	// so none of these may turn a status poll into an error or a false "current".
	doer := &registryDoer{status: http.StatusTooManyRequests}
	status, err := statusWithRegistry(t, doer, "codex").GetStatus(context.Background())
	if err != nil {
		t.Fatalf("a failing registry broke the status call: %v", err)
	}
	if latest := status.Agents["codex"].LatestVersion; latest != nil {
		t.Fatalf("latestVersion = %v, want nil", latest)
	}
	if !status.Agents["codex"].Installed {
		t.Fatal("the failing lookup also lost the installed state")
	}
}

func TestStatusDoesNotAskThePyPIAgentTheNPMQuestion(t *testing.T) {
	// Aider is installed with uv from PyPI, whose metadata shape is different.
	doer := &registryDoer{}
	status, err := statusWithRegistry(t, doer, "aider", "uv").GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest := status.Agents["aider"].LatestVersion; latest != nil {
		t.Fatalf("aider borrowed an npm answer: %v", latest)
	}
	if doer.calls.Load() != 0 {
		t.Fatalf("registry was called %d times for a uv Agent", doer.calls.Load())
	}
}

func TestStatusAsksTheRegistryOnceAcrossPolls(t *testing.T) {
	doer := &registryDoer{version: "2.0.0"}
	core := statusWithRegistry(t, doer, "codex")
	for range 4 {
		if _, err := core.GetStatus(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	// The UI polls status; without a cache that is one request per Agent per
	// poll, which is both wasteful and the fastest way to get rate-limited.
	if got := doer.calls.Load(); got != 1 {
		t.Fatalf("registry was asked %d times across four polls, want 1", got)
	}
}

func TestStatusBoundsHowManyRegistryRequestsRunAtOnce(t *testing.T) {
	doer := &registryDoer{}
	// Every npm Agent installed at once is the worst case for a rate limiter.
	core := statusWithRegistry(t, doer, "codex", "claude", "opencode", "kilo", "openclaw")
	if _, err := core.GetStatus(context.Background()); err != nil {
		t.Fatal(err)
	}
	if peak := doer.peak.Load(); peak > latestVersionConcurrency {
		t.Fatalf("%d requests were in flight at once, limit is %d", peak, latestVersionConcurrency)
	}
}

func TestNothingInstalledAsksTheRegistryNothing(t *testing.T) {
	// A first run has no Agents, so there is no version to compare and no reason
	// to spend a request. The answer is cached so this is decided once.
	doer := &registryDoer{}
	core := NewUseCases(StatusOptions{
		Home:     t.TempDir(),
		Platform: platform.For("linux", "amd64"),
		Lookup:   func(string) (string, bool) { return "", false },
	})
	core.SetRuntimeDownloader(doer)
	status, err := core.GetStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if latest := status.Agents["codex"].LatestVersion; latest != nil {
		t.Fatalf("latestVersion = %v for an Agent that is not installed", latest)
	}
	if got := doer.calls.Load(); got != 0 {
		t.Fatalf("registry was asked %d times with nothing installed", got)
	}
}
