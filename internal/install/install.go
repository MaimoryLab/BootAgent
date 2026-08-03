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
	EnforceLocked bool
	Latest        bool
	Timeout       time.Duration
	Registry      string
}

type Result struct {
	Installed     bool
	Version       string
	LockedVersion string
	Registry      string
}

var versionPattern = regexp.MustCompile(`(^|[^0-9])([0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]+)?)`)

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

// ResolveAiderPython312 reports an existing Python 3.12 when the host has one.
// It is retained for the CLI's diagnostics; installation itself no longer needs
// it because uv provisions the interpreter.
func ResolveAiderPython312(ctx context.Context, runtime Runtime) (string, error) {
	if err := checkContext(ctx); err != nil {
		return "", err
	}
	if runtime.Runner == nil {
		return "", prerequisiteError("An existing Python 3.12 installation is required for Aider; OneAgent will not download Python automatically")
	}
	if executable, ok := runtime.Runner.LookPath("python3.12"); ok && executable != "" {
		return executable, nil
	}
	for _, command := range []string{"python3", "python"} {
		executable, ok := runtime.Runner.LookPath(command)
		if !ok || executable == "" {
			continue
		}
		result, err := runtime.command(ctx, []string{executable, "--version"}, nil, VersionCommandTimeout)
		if err != nil || result.ExitCode != 0 {
			if isContextError(err) {
				return "", timeoutError("Checking for Python 3.12 was cancelled", err)
			}
			continue
		}
		version := VersionFromOutput(result.Stdout + "\n" + result.Stderr)
		if strings.HasPrefix(version, "3.12.") {
			return executable, nil
		}
	}
	if runtime.Platform.OS == "windows" {
		if launcher, ok := runtime.Runner.LookPath("py"); ok && launcher != "" {
			result, err := runtime.command(ctx, []string{launcher, "-3.12", "--version"}, nil, VersionCommandTimeout)
			if err != nil {
				if isContextError(err) {
					return "", timeoutError("Checking for Python 3.12 was cancelled", err)
				}
			} else if result.ExitCode == 0 {
				return "3.12", nil
			}
		}
	}
	return "", prerequisiteError("An existing Python 3.12 installation is required for Aider; OneAgent will not download Python automatically")
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

func VerifyNPMIntegrity(ctx context.Context, runtime Runtime, npm, spec, expected, registry string, timeout time.Duration) error {
	if expected == "" {
		return nil
	}
	result, err := runtime.command(ctx, []string{npm, "view", spec, "dist.integrity", "--registry=" + registry}, nil, runtime.timeout(timeout))
	if err != nil {
		if isContextError(err) {
			return oneerrors.New(oneerrors.AgentInstallFailed, "Timed out reading the checksum for "+spec, oneerrors.WithRetryable(true), oneerrors.WithCause(err))
		}
		return oneerrors.New(oneerrors.AgentInstallFailed, "Cannot read the checksum for "+spec, oneerrors.WithRetryable(true), oneerrors.WithCause(err))
	}
	if result.ExitCode != 0 {
		return oneerrors.New(oneerrors.AgentInstallFailed, fmt.Sprintf("%s is not available on %s", spec, registry), oneerrors.WithRetryable(true))
	}
	reported := strings.TrimSpace(result.Stdout)
	if reported != expected {
		return oneerrors.New(oneerrors.AgentInstallFailed, fmt.Sprintf("Checksum mismatch for %s on %s: manifest expects %s, registry reports %s", spec, registry, expected, valueOrNone(reported)))
	}
	return nil
}

func InstallLockedAgent(ctx context.Context, runtime Runtime, agentID string, agent catalog.Agent, options Options) (Result, error) {
	if err := checkContext(ctx); err != nil {
		return Result{}, err
	}
	if agent.Package == nil {
		return Result{}, prerequisiteError(fmt.Sprintf("%s has no package installation contract", agent.Name))
	}
	packageInfo := *agent.Package
	locked := packageInfo.Version
	result := Result{LockedVersion: locked}
	var executable string
	if agent.Command != "" && runtime.Runner != nil {
		executable, _ = runtime.Runner.LookPath(agent.Command)
	}
	current := ""
	if executable != "" {
		current = InstalledVersion(ctx, runtime, agent)
		if !options.EnforceLocked {
			result.Version = current
			return result, nil
		}
		if current == locked {
			result.Version = current
			return result, nil
		}
	}

	if err := requirePrerequisites(ctx, runtime, agent); err != nil {
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
			return Result{}, prerequisiteError(fmt.Sprintf("npm is required to install %s", agent.Name))
		}
		// A managed Node has no writable system prefix, and a system Node often
		// needs sudo for -g. Directing global installs at OneAgent's own prefix
		// makes both cases work without elevation and keeps the Agent CLIs on
		// the same PATH entry the runtime bootstrap already records.
		if managedNPM(runtime, npm) {
			environment["npm_config_prefix"] = GlobalPrefix(runtime.Home)
		}
		spec := packageName
		if !options.Latest {
			spec += "@" + locked
			if err := VerifyNPMIntegrity(ctx, runtime, npm, spec, pointerValue(packageInfo.Integrity), registry, options.Timeout); err != nil {
				return Result{}, err
			}
		}
		if registry != officialRegistry() {
			environment["npm_config_registry"] = registry
		}
		argv = []string{npm, "install", "-g", spec}
	case "uv":
		uv, ok := runtime.Runner.LookPath("uv")
		if !ok || uv == "" {
			return Result{}, prerequisiteError("uv is required to install Aider")
		}
		spec := packageName
		if !options.Latest {
			spec += "==" + locked
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
	if !options.Latest {
		result.Version = locked
	}
	result.Registry = registry
	return result, nil
}

func requirePrerequisites(ctx context.Context, runtime Runtime, agent catalog.Agent) error {
	if runtime.Runner == nil {
		return prerequisiteError("A process runner is required for Agent installation")
	}
	packageInfo := agent.Package
	if packageInfo == nil {
		return prerequisiteError(fmt.Sprintf("%s has no package installation contract", agent.Name))
	}
	if packageInfo.Manager == "npm" {
		if _, ok := runtime.Runner.LookPath("npm"); !ok {
			return prerequisiteError(fmt.Sprintf("npm is required to install %s. Install the Node.js runtime first.", agent.Name))
		}
	}
	if packageInfo.Manager == "uv" {
		if _, ok := runtime.Runner.LookPath("uv"); !ok {
			return prerequisiteError("uv is required to install Aider. Install the uv runtime first.")
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
			return prerequisiteError(fmt.Sprintf("%s is required for %s on Windows", missing[0], agent.Name))
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
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return timeoutError("Agent installation request was cancelled", err)
	}
	return nil
}

func timeoutError(message string, cause error) error {
	return oneerrors.New(oneerrors.Timeout, message, oneerrors.WithRetryable(true), oneerrors.WithCause(cause))
}

func prerequisiteError(message string) error {
	return oneerrors.New(oneerrors.PrerequisiteMissing, message)
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func valueOrNone(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
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
