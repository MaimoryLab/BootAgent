package install

import (
	"context"
	"fmt"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// Result reports what happened to one Agent.
type Result struct {
	// Installed is false when the Agent was already present at an acceptable
	// version, which is not a failure.
	Installed bool `json:"installed"`
	// Version is what is now installed. Empty after a --latest install, because
	// what floats at the tag is not known without asking again.
	Version string `json:"version"`
	// LockedVersion is what the manifest pins, shown so a user can see the
	// difference when they installed something else.
	LockedVersion string `json:"lockedVersion"`
	// Registry is where the package came from. Reported so the origin of what is
	// on the machine is visible rather than implied.
	Registry string `json:"registry,omitempty"`
}

// Options carries the per-call decisions for one install.
type Options struct {
	// EnforceLocked reinstalls when the present version differs from the pin.
	// Without it an Agent that is merely present is left alone.
	EnforceLocked bool
	// Latest installs the floating tag instead of the pinned version. Only ever
	// set from an explicit user request: the manifest forbids `latest`, and the
	// integrity check cannot apply to it.
	Latest  bool
	Timeout time.Duration
	// Registry is a mirror id or an HTTPS URL. Empty means the official source.
	// A mirror is always an explicit choice -- switching automatically on a
	// network error would leave the user unable to tell where a package came
	// from.
	Registry string
}

// LockedAgent installs one Agent at the version the manifest pins.
func LockedAgent(rt *runtime.Runtime, agentID string, agent catalog.Agent, options Options) (Result, error) {
	if agent.Package == nil {
		return Result{}, oerr.Newf("PREREQUISITE_MISSING", "No allowlisted package manager for %s", agent.Name)
	}
	locked := agent.Package.Version
	_, present := rt.Which(agent.Command)

	current := ""
	if present {
		current = InstalledVersion(rt, agent)
	}
	// Present and we are not enforcing the pin: leave it alone. Reinstalling
	// would replace a version the user may have chosen.
	if present && !options.EnforceLocked {
		return Result{Installed: false, Version: current, LockedVersion: locked}, nil
	}
	if present && options.EnforceLocked && current == locked {
		return Result{Installed: false, Version: current, LockedVersion: locked}, nil
	}

	if err := RequirePrerequisites(rt, agent); err != nil {
		return Result{}, err
	}

	resolvedRegistry, err := ResolveRegistry(options.Registry)
	if err != nil {
		return Result{}, err
	}

	env := rt.Env
	var argv []string

	switch agent.Package.Manager {
	case "npm":
		npm, found := rt.Which("npm")
		if !found {
			return Result{}, oerr.Newf("PREREQUISITE_MISSING", "npm is required to install %s", agent.Name)
		}
		spec := agent.Package.Name
		if !options.Latest {
			spec = agent.Package.Name + "@" + locked
		}
		if resolvedRegistry != catalog.OfficialNpmRegistry {
			// npm reads the registry from its environment, so no argument has to
			// be threaded through the install command itself. Copied rather than
			// mutated so the Runtime's own environment is unchanged.
			env = withRegistry(rt.Env, resolvedRegistry)
		}
		if !options.Latest {
			// Only meaningful for a pinned spec: the manifest's checksum
			// describes the locked version, not whatever floats at the tag.
			if err := VerifyNpmIntegrity(rt, npm, spec, agent.Package.Integrity, resolvedRegistry, options.Timeout); err != nil {
				return Result{}, err
			}
		}
		argv = []string{npm, "install", "-g", spec}

	case "uv":
		uv, found := rt.Which("uv")
		if !found {
			return Result{}, oerr.New("PREREQUISITE_MISSING", "uv is required to install Aider")
		}
		python, err := Python312ForUV(rt)
		if err != nil {
			return Result{}, err
		}
		spec := agent.Package.Name
		if !options.Latest {
			spec = agent.Package.Name + "==" + locked
		}
		// --no-python-downloads is the boundary: OneAgent configures an
		// environment, it does not install language runtimes behind the user's
		// back.
		argv = []string{uv, "tool", "install", "--force", "--python", python, "--no-python-downloads", spec}

	default:
		return Result{}, oerr.Newf("PREREQUISITE_MISSING", "No allowlisted package manager for %s", agent.Name)
	}

	result, err := rt.Run(context.Background(), argv, runtime.RunOptions{Env: env, Timeout: options.Timeout})
	if err != nil {
		if runtime.IsTimeout(err) {
			return Result{}, oerr.Newf("TIMEOUT", "Installing %s timed out", agent.Name).Set(oerr.WithRetryable())
		}
		return Result{}, oerr.Newf("AGENT_INSTALL_FAILED", "Cannot start installer for %s: %v", agent.Name, err)
	}
	if result.ExitCode != 0 {
		message := fmt.Sprintf("Installing %s failed with exit code %d", agent.Name, result.ExitCode)
		if detail := FailureDetail(env, result.Stdout, result.Stderr); detail != "" {
			message += ": " + detail
		}
		return Result{}, oerr.New("AGENT_INSTALL_FAILED", message).Set(oerr.WithRetryable())
	}

	installed := Result{Installed: true, LockedVersion: locked, Registry: resolvedRegistry}
	if !options.Latest {
		installed.Version = locked
	}
	return installed, nil
}

// withRegistry copies an environment and adds the registry npm reads.
func withRegistry(env map[string]string, registry string) map[string]string {
	copied := make(map[string]string, len(env)+1)
	for key, value := range env {
		copied[key] = value
	}
	copied["npm_config_registry"] = registry
	return copied
}
