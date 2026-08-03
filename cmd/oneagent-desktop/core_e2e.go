//go:build wails && e2e

package main

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/app"
	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

func newDesktopUseCases() *app.UseCases {
	info := platform.Current()
	home := platform.ResolveHome(nil, info.OS)
	return app.NewUseCasesWithProviderClient(app.StatusOptions{
		Home:        home,
		Platform:    info,
		Runner:      newE2ERunner(),
		Environment: map[string]string{"HOME": home},
	}, provider.NewClient(e2eProviderDoer{}))
}

type e2eRunner struct {
	mu        sync.RWMutex
	agents    map[string]catalog.Agent
	byPackage map[string]string
	installed map[string]string
}

func newE2ERunner() *e2eRunner {
	runner := &e2eRunner{
		agents:    map[string]catalog.Agent{},
		byPackage: map[string]string{},
		installed: map[string]string{},
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return runner
	}
	runner.agents = manifest.Agents
	for id, agent := range manifest.Agents {
		if agent.Package != nil {
			runner.byPackage[agent.Package.Name] = id
		}
	}
	return runner
}

func (r *e2eRunner) LookPath(command string) (string, bool) {
	// The browser build pretends Node is present and uv is not, so the runtime
	// section renders one installed row and one installable row without any
	// download happening.
	if command == "npm" || command == "node" {
		return "/oneagent-e2e/" + command, true
	}
	r.mu.RLock()
	_, ok := r.installed[command]
	r.mu.RUnlock()
	if !ok {
		return "", false
	}
	return "/oneagent-e2e/" + command, true
}

func (r *e2eRunner) Run(_ context.Context, argv []string, _ map[string]string, _ time.Duration) (process.Result, error) {
	result := process.Result{Args: append([]string(nil), argv...), ExitCode: 0}
	// The region probe is answered explicitly so the browser build renders the
	// same download defaults on every machine. Without this it would depend on
	// whichever locale the developer's own system reports.
	if len(argv) >= 2 && argv[0] == "defaults" && argv[1] == "read" {
		result.Stdout = "zh_CN\n"
		return result, nil
	}
	if len(argv) >= 4 && argv[1] == "install" && argv[2] == "-g" {
		if id, ok := r.byPackage[argv[3]]; ok {
			agent := r.agents[id]
			r.mu.Lock()
			r.installed[agent.Command] = "1.0.0"
			r.mu.Unlock()
		}
		return result, nil
	}
	if len(argv) >= 2 && argv[len(argv)-1] == "--version" {
		command := filepath.Base(argv[0])
		r.mu.RLock()
		version := r.installed[command]
		r.mu.RUnlock()
		if version != "" {
			result.Stdout = command + " " + version + "\n"
		}
	}
	return result, nil
}

type e2eProviderDoer struct{}

func (e2eProviderDoer) Do(request *http.Request) (*http.Response, error) {
	body := ""
	if request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/models") {
		body = `{"data":[{"id":"oneagent-e2e-model"}]}`
	}
	status := http.StatusNoContent
	if body != "" {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}
