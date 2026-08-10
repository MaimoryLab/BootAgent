// Package install contains the Agent installation workflow. It stays separate
// from Wails bindings so checks and command construction remain in the core.
package install

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
)

const (
	// DefaultCommandTimeout is a backstop, not the working limit. Stall detection
	// in internal/process is what actually ends a hung install, so this only has
	// to be larger than any legitimate install: at 180s it was cutting off
	// perfectly healthy `npm install -g` runs on slow links, and the user saw
	// "timed out" rather than anything pointing at the registry.
	DefaultCommandTimeout = 60 * time.Minute
	VersionCommandTimeout = 30 * time.Second
)

type Runtime struct {
	Home     string
	Platform platform.Info
	Env      map[string]string
	Runner   process.Runner
	OnOutput process.OutputListener
}

func NewRuntime(home string, info platform.Info, runner process.Runner, env map[string]string) Runtime {
	if info.OS == "" {
		info = platform.Current()
	}
	if home == "" {
		home = platform.ResolveHome(env, info.OS)
	}
	if runner == nil {
		current := process.Current()
		runner = current
		if env == nil {
			env = current.Env
		}
	}
	return Runtime{Home: home, Platform: info, Env: cloneEnv(env), Runner: runner}
}

func (r Runtime) command(ctx context.Context, argv []string, env map[string]string, timeout time.Duration) (process.Result, error) {
	if r.Runner == nil {
		return process.Result{Args: append([]string(nil), argv...), ExitCode: -1}, fmt.Errorf("process runner is not configured")
	}
	overrides := cloneEnv(r.Env)
	maps.Copy(overrides, env)
	if r.OnOutput != nil {
		r.OnOutput(process.Output{Kind: "command", Args: append([]string(nil), argv...)})
	}
	var result process.Result
	var err error
	if runner, ok := r.Runner.(process.StreamingRunner); ok {
		result, err = runner.RunWithOutput(ctx, argv, overrides, timeout, r.OnOutput)
	} else {
		result, err = r.Runner.Run(ctx, argv, overrides, timeout)
	}
	if err == nil {
		return result, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return result, err
	}
	return result, err
}

// Run keeps callers that need a package-manager-specific command on the same
// environment, output, and timeout path as installation.
func (r Runtime) Run(ctx context.Context, argv []string, env map[string]string, timeout time.Duration) (process.Result, error) {
	return r.command(ctx, argv, env, timeout)
}

func (r Runtime) timeout(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return DefaultCommandTimeout
}

func cloneEnv(source map[string]string) map[string]string {
	if source == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(source))
	maps.Copy(result, source)
	return result
}
