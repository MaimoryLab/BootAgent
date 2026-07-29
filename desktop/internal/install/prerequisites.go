package install

import (
	"context"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// probeTimeout bounds a --version call. Short because it is only a probe; a
// hung interpreter should not stall the wizard.
const probeTimeout = 30 * time.Second

// RequirePrerequisites checks what must already be on the machine before an
// Agent can be installed.
//
// The Windows requirements come from windows_prerequisites in the manifest, so a
// new Agent's requirement is declared there rather than added here.
func RequirePrerequisites(rt *runtime.Runtime, agent catalog.Agent) error {
	if agent.Package == nil {
		return oerr.Newf("PREREQUISITE_MISSING", "No allowlisted package manager for %s", agent.Name)
	}
	switch agent.Package.Manager {
	case "npm":
		if _, found := rt.Which("npm"); !found {
			return oerr.Newf("PREREQUISITE_MISSING", "npm is required to install %s", agent.Name)
		}
	case "uv":
		if _, found := rt.Which("uv"); !found {
			return oerr.New("PREREQUISITE_MISSING", "uv is required to install Aider")
		}
		if _, err := Python312ForUV(rt); err != nil {
			return err
		}
	}
	if rt.OSID == "windows" {
		for _, prerequisite := range agent.WindowsPrerequisites {
			if _, found := rt.Which(prerequisite); !found {
				return oerr.Newf(
					"PREREQUISITE_MISSING",
					"%s is required for %s on Windows", prerequisite, agent.Name,
				)
			}
		}
	}
	return nil
}

// Python312ForUV finds an existing Python 3.12, which Aider needs.
//
// OneAgent never downloads a language runtime -- the install passes
// --no-python-downloads -- so an absent interpreter is a prerequisite the user
// has to satisfy. This is the one place Python remains a requirement, and it
// belongs to Aider's upstream rather than to OneAgent: see ADR-008.
//
// The search widens rather than guessing: an explicitly versioned command first,
// then a generic one whose --version actually reports 3.12, then the Windows
// launcher. A python3 that turns out to be 3.14 is not accepted just because it
// exists.
func Python312ForUV(rt *runtime.Runtime) (string, error) {
	if direct, found := rt.Which("python3.12"); found {
		return direct, nil
	}
	for _, command := range []string{"python3", "python"} {
		executable, found := rt.Which(command)
		if !found {
			continue
		}
		result, err := rt.Run(context.Background(), []string{executable, "--version"},
			runtime.RunOptions{Env: rt.Env, Timeout: probeTimeout})
		if err != nil {
			// A probe that cannot run tells us nothing; try the next candidate
			// rather than failing the whole install here.
			continue
		}
		version := VersionFromOutput(result.Stdout + "\n" + result.Stderr)
		if result.ExitCode == 0 && strings.HasPrefix(version, "3.12.") {
			return executable, nil
		}
	}
	if rt.OSID == "windows" {
		if launcher, found := rt.Which("py"); found {
			result, err := rt.Run(context.Background(), []string{launcher, "-3.12", "--version"},
				runtime.RunOptions{Env: rt.Env, Timeout: probeTimeout})
			if err == nil && result.ExitCode == 0 {
				// uv accepts a bare version here and resolves it through the
				// launcher itself.
				return "3.12", nil
			}
		}
	}
	return "", oerr.New(
		"PREREQUISITE_MISSING",
		"An existing Python 3.12 installation is required for Aider; OneAgent will not download Python automatically",
	)
}

// InstalledVersion reports the version an Agent currently reports, or "" when it
// is not installed or cannot be asked.
func InstalledVersion(rt *runtime.Runtime, agent catalog.Agent) string {
	if agent.Command == "" {
		return ""
	}
	executable, found := rt.Which(agent.Command)
	if !found {
		return ""
	}
	args := agent.VersionArgs
	if len(args) == 0 {
		args = []string{"--version"}
	}
	argv := append([]string{executable}, args...)
	result, err := rt.Run(context.Background(), argv, runtime.RunOptions{Env: rt.Env, Timeout: probeTimeout})
	if err != nil {
		// Not installed enough to answer. Reporting "" rather than an error lets
		// status show the Agent as absent instead of failing the whole request.
		return ""
	}
	return VersionFromOutput(result.Stdout + "\n" + result.Stderr)
}
