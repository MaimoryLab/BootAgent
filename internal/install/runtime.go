// Package install contains the transport-independent Agent installation
// workflow. It is intentionally separate from Wails bindings so the CLI can
// use the same checks and command construction.
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
	DefaultCommandTimeout = 180 * time.Second
	VersionCommandTimeout = 30 * time.Second
)

type Runtime struct {
	Home     string
	Platform platform.Info
	Env      map[string]string
	Runner   process.Runner
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

func CurrentRuntime() Runtime {
	info := platform.Current()
	current := process.Current()
	return NewRuntime(platform.ResolveHome(current.Env, info.OS), info, current, current.Env)
}

func (r Runtime) command(ctx context.Context, argv []string, env map[string]string, timeout time.Duration) (process.Result, error) {
	if r.Runner == nil {
		return process.Result{Args: append([]string(nil), argv...), ExitCode: -1}, fmt.Errorf("process runner is not configured")
	}
	overrides := cloneEnv(r.Env)
	maps.Copy(overrides, env)
	result, err := r.Runner.Run(ctx, argv, overrides, timeout)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return result, err
	}
	return result, err
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
