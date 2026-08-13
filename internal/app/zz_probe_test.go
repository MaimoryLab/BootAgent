package app

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

type recordDoer struct{ hits []string }

func (d *recordDoer) Do(r *http.Request) (*http.Response, error) {
	d.hits = append(d.hits, r.URL.String())
	return nil, errors.New("blocked")
}

// Real machine, real locale probe: cancelled poll first, then a healthy one.
func TestProbeRealMachineAfterFix(t *testing.T) {
	info := platform.Current()
	home := t.TempDir()
	env := map[string]string{"HOME": home, "PATH": filepath.Join(home, "empty")}
	core := NewUseCases(StatusOptions{Home: home, Platform: info, Environment: env, Runner: process.OSRunner{Env: env}})

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	first, _ := core.Settings(cancelled)
	t.Logf("after a cancelled poll = %#v", first)

	second, _ := core.Settings(context.Background())
	t.Logf("next healthy poll      = %#v", second)

	doer := &recordDoer{}
	core.SetRuntimeDownloader(doer)
	_, _ = core.InstallRuntime(context.Background(), InstallRuntimeOptions{RuntimeID: "node"})
	for i, h := range doer.hits {
		t.Logf("download hit %d: %s", i, h)
	}
}
