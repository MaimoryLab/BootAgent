package install

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/process"
)

type Options struct {
	Version  string
	Timeout  time.Duration
	Registry string
}

type Result struct {
	Installed bool
	Version   string
	Registry  string
}

var (
	versionPattern        = regexp.MustCompile(`(^|[^0-9])([0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]+)?)`)
	packageVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z][0-9A-Za-z.+-]*)?$`)
)

func VersionFromOutput(text string) string {
	match := versionPattern.FindStringSubmatch(text)
	if len(match) == 0 {
		return ""
	}
	return match[2]
}

func InstalledVersion(ctx context.Context, runtime Runtime, agent catalog.Agent) string {
	if agent.Command == "" || runtime.Runner == nil {
		return ""
	}
	executable, ok := runtime.Runner.LookPath(agent.Command)
	if !ok || executable == "" {
		return ""
	}
	args := append([]string{executable}, agent.VersionArgs...)
	if len(agent.VersionArgs) == 0 {
		args = append(args, "--version")
	}
	result, err := runtime.command(ctx, args, nil, VersionCommandTimeout)
	if err != nil {
		return ""
	}
	return VersionFromOutput(result.Stdout + "\n" + result.Stderr)
}

// AiderPythonVersion is the interpreter Aider is pinned against. It is passed
// to uv as a request, not as a path: uv reuses a matching system interpreter
// when one exists and downloads a managed one when it does not.
const AiderPythonVersion = "3.12"

// managedNPM reports whether this npm came from OneAgent's runtime root.
func managedNPM(runtime Runtime, npm string) bool {
	if npm == "" {
		return false
	}
	root := RuntimeRoot(runtime.Home)
	relative, err := filepath.Rel(root, npm)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func NPMEnvironment(runtime Runtime, npm, registry string) map[string]string {
	environment := map[string]string{}
	if managedNPM(runtime, npm) {
		environment["npm_config_prefix"] = GlobalPrefix(runtime.Home)
	}
	if registry != "" {
		environment["npm_config_registry"] = registry
	}
	return environment
}

func ResolveRegistry(value string) (string, error) {
	official := officialRegistry()
	if value == "" {
		return official, nil
	}
	for _, mirror := range catalog.Mirrors() {
		if value == mirror.ID {
			return mirror.Registry, nil
		}
	}
	for _, character := range value {
		if character < 32 || character == 127 {
			return "", oneerrors.New(oneerrors.InvalidRequest, "Registry URL contains control characters")
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Registry URL must start with https://")
	}
	if parsed.User != nil {
		return "", oneerrors.New(oneerrors.InvalidRequest, "Registry URL must not contain credentials")
	}
	return strings.TrimRight(value, "/") + "/", nil
}

// ValidateVersion accepts only exact versions shared by the supported package managers.
func ValidateVersion(version string) error {
	if version != "" && !packageVersionPattern.MatchString(version) {
		return oneerrors.New(oneerrors.InvalidRequest, "Agent version must be an exact version such as 1.2.3")
	}
	return nil
}

func InstallAgent(ctx context.Context, runtime Runtime, agent catalog.Agent, options Options) (Result, error) {
	if err := checkContext(ctx); err != nil {
		return Result{}, err
	}
	if agent.Package == nil {
		return Result{}, prerequisiteError(fmt.Sprintf("%s has no package installation contract", agent.Name))
	}
	version := strings.TrimSpace(options.Version)
	if err := ValidateVersion(version); err != nil {
		return Result{}, err
	}
	packageInfo := *agent.Package
	result := Result{}
	var executable string
	if agent.Command != "" && runtime.Runner != nil {
		executable, _ = runtime.Runner.LookPath(agent.Command)
	}
	current := ""
	if executable != "" {
		var versionErr error
		current, versionErr = installedVersion(ctx, runtime, agent)
		if versionErr != nil {
			return Result{}, oneerrors.New(oneerrors.AgentInstallFailed, fmt.Sprintf("%s version check failed", agent.Name), oneerrors.WithRetryable(true), oneerrors.WithCause(versionErr))
		}
		if version == "" || current == version {
			result.Version = current
			return result, nil
		}
	}

	if err := requirePrerequisites(runtime, agent); err != nil {
		return Result{}, err
	}
	registry, err := ResolveRegistry(options.Registry)
	if err != nil {
		return Result{}, err
	}
	manager := packageInfo.Manager
	packageName := packageInfo.Name
	var argv []string
	environment := map[string]string{}
	switch manager {
	case "npm":
		npm, ok := runtime.Runner.LookPath("npm")
		if !ok || npm == "" {
			// Same wording as requirePrerequisites: this is the same condition
			// reached by a second LookPath, and two different messages for it
			// meant the one a user saw was not predictable from the UI.
			return Result{}, missingToolError(npmPrerequisiteMessage(agent.Name))
		}
		// A managed Node has no writable system prefix, and a system Node often
		// needs sudo for -g. Directing global installs at OneAgent's own prefix
		// makes both cases work without elevation and keeps the Agent CLIs on
		// the same PATH entry the runtime bootstrap already records.
		if managedNPM(runtime, npm) {
			environment["npm_config_prefix"] = GlobalPrefix(runtime.Home)
		}
		spec := packageName
		if version != "" {
			spec += "@" + version
		}
		argv = []string{npm, "install", "-g"}
		if registry != officialRegistry() {
			// Keep the environment override for npm-compatible wrappers, but put
			// the registry in argv too. Windows may resolve npm through a .cmd
			// shim whose environment handling differs from a native executable.
			environment["npm_config_registry"] = registry
			argv = append(argv, "--registry="+registry)
		}
		argv = append(argv, spec)
	case "uv":
		uv, ok := runtime.Runner.LookPath("uv")
		if !ok || uv == "" {
			return Result{}, missingToolError(uvPrerequisiteMessage)
		}
		spec := packageName
		if version != "" {
			spec += "==" + version
		}
		// uv resolves Python itself. When a matching interpreter is already on
		// the machine it is reused; otherwise uv downloads a managed CPython
		// into OneAgent's runtime root, which is why a preinstalled Python 3.12
		// is no longer a prerequisite for Aider.
		environment["UV_PYTHON_INSTALL_DIR"] = filepath.Join(RuntimeRoot(runtime.Home), "python")
		environment["UV_TOOL_BIN_DIR"] = GlobalBinDir(runtime.Home, runtime.Platform.OS)
		argv = []string{uv, "tool", "install", "--force", "--python", AiderPythonVersion, spec}
	default:
		return Result{}, prerequisiteError(fmt.Sprintf("No allowlisted package manager for %s", agent.Name))
	}
	commandResult, runErr := runtime.command(ctx, argv, environment, runtime.timeout(options.Timeout))
	if runErr != nil {
		if isContextError(runErr) {
			return Result{}, oneerrors.New(oneerrors.Timeout, fmt.Sprintf("Installing %s timed out", agent.Name), oneerrors.WithRetryable(true), oneerrors.WithCause(runErr))
		}
		return Result{}, oneerrors.New(oneerrors.AgentInstallFailed, fmt.Sprintf("Cannot start installer for %s", agent.Name), oneerrors.WithRetryable(true), oneerrors.WithCause(runErr))
	}
	if commandResult.ExitCode != 0 {
		detail := installerFailureDetail(commandResult, runtime.Env)
		message := fmt.Sprintf("Installing %s failed with exit code %d", agent.Name, commandResult.ExitCode)
		if detail != "" {
			message += ": " + detail
		}
		return Result{}, oneerrors.New(oneerrors.AgentInstallFailed, message, oneerrors.WithRetryable(true))
	}
	result.Installed = true
	result.Version = version
	if result.Version == "" {
		result.Version, runErr = installedVersion(ctx, runtime, agent)
		if runErr != nil {
			return Result{}, oneerrors.New(oneerrors.AgentInstallFailed, fmt.Sprintf("%s version check failed", agent.Name), oneerrors.WithRetryable(true), oneerrors.WithCause(runErr))
		}
	}
	result.Registry = registry
	return result, nil
}

func installedVersion(ctx context.Context, runtime Runtime, agent catalog.Agent) (string, error) {
	if agent.Command == "" || runtime.Runner == nil {
		return "", nil
	}
	executable, ok := runtime.Runner.LookPath(agent.Command)
	if !ok || executable == "" {
		return "", nil
	}
	args := append([]string{executable}, agent.VersionArgs...)
	if len(agent.VersionArgs) == 0 {
		args = append(args, "--version")
	}
	result, err := runtime.command(ctx, args, nil, VersionCommandTimeout)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("exit code %d", result.ExitCode)
	}
	return VersionFromOutput(result.Stdout + "\n" + result.Stderr), nil
}

func requirePrerequisites(runtime Runtime, agent catalog.Agent) error {
	if runtime.Runner == nil {
		return prerequisiteError("A process runner is required for Agent installation")
	}
	packageInfo := agent.Package
	if packageInfo == nil {
		return prerequisiteError(fmt.Sprintf("%s has no package installation contract", agent.Name))
	}
	if packageInfo.Manager == "npm" {
		if _, ok := runtime.Runner.LookPath("npm"); !ok {
			return missingToolError(npmPrerequisiteMessage(agent.Name))
		}
	}
	if packageInfo.Manager == "uv" {
		if _, ok := runtime.Runner.LookPath("uv"); !ok {
			return missingToolError(uvPrerequisiteMessage)
		}
	}
	if runtime.Platform.OS == "windows" {
		missing := make([]string, 0)
		for _, prerequisite := range agent.WindowsPrerequisites {
			if _, ok := runtime.Runner.LookPath(prerequisite); !ok {
				missing = append(missing, prerequisite)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return missingToolError(fmt.Sprintf("%s is required for %s on Windows. Install it, then retry.", missing[0], agent.Name))
		}
	}
	return nil
}

func installerFailureDetail(result process.Result, environment map[string]string) string {
	text := redact(result.Stderr+"\n"+result.Stdout, secretValues(environment))
	text = ansiPattern.ReplaceAllString(text, "")
	lines := make([]string, 0)
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return trimRunes(strings.Join(lines, " | "), 600)
}

func Redact(text string, secrets []string) string {
	return redact(text, secrets)
}

func redact(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	return text
}

func secretValues(environment map[string]string) []string {
	values := make([]string, 0)
	for key, value := range environment {
		upper := strings.ToUpper(key)
		if value != "" && (strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD")) {
			values = append(values, value)
		}
	}
	sort.Strings(values)
	return values
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func checkContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return timeoutError("Agent installation request was cancelled", err)
	}
	return nil
}

func timeoutError(message string, cause error) error {
	return oneerrors.New(oneerrors.Timeout, message, oneerrors.WithRetryable(true), oneerrors.WithCause(cause))
}

// The two runtime prerequisites are each reached from two places -- once in
// requirePrerequisites and again at the LookPath in the manager switch -- so the
// text lives here rather than being written twice with different wording.
func npmPrerequisiteMessage(agentName string) string {
	return fmt.Sprintf("npm is required to install %s. Install the Node.js runtime first, then retry.", agentName)
}

const uvPrerequisiteMessage = "uv is required to install Aider. Install the uv runtime first, then retry."

// prerequisiteError reports a missing prerequisite the user cannot fix by
// retrying: a manifest without an installation contract, an unsupported archive
// format, a platform the Agent does not serve. Retrying runs the same code
// against the same inputs, so the UI is right to offer no retry button.
func prerequisiteError(message string) error {
	return oneerrors.New(oneerrors.PrerequisiteMissing, message)
}

// missingToolError reports a prerequisite the user can supply, which is a
// different outcome from the one above even though both are PrerequisiteMissing.
//
// Retryable drives whether a retry button is rendered at all
// (ActivationPage.tsx gates on it), and these were the failures most likely to
// be fixable by hand -- install Node, install uv, install the Windows
// dependency -- while offering the least in-app recourse. Without the flag the
// row was terminal: no retry, and no primary action either, because that only
// appears once every Agent has finished.
func missingToolError(message string) error {
	return oneerrors.New(oneerrors.PrerequisiteMissing, message, oneerrors.WithRetryable(true))
}

func officialRegistry() string {
	for _, mirror := range catalog.Mirrors() {
		if mirror.ID == "official" {
			return mirror.Registry
		}
	}
	return "https://registry.npmjs.org/"
}

func trimRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)
