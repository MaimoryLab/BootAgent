// Command oneagent-rc contains the networked release-candidate checks that are
// intentionally kept outside the desktop and headless application binaries.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/install"
)

var defaultAgents = []string{"codex", "claude-code", "opencode", "kilo-cli"}

type isolation struct {
	root string
	home string
	env  []string
	cli  string
}

type installResult struct {
	Agent   string `json:"agent"`
	Status  string `json:"status"`
	Version string `json:"version"`
}

type installPayload struct {
	OK      bool            `json:"ok"`
	Results []installResult `json:"results"`
	Log     string          `json:"log"`
}

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch args[0] {
	case "verify-agents":
		if err := verifyAgents(root, args[1:], false); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	case "adopted":
		if err := verifyAgents(root, args[1:], true); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	default:
		usage()
		return 2
	}
	return 0
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: oneagent-rc verify-agents [--agents ids] [--registry id-or-url]")
	fmt.Fprintln(os.Stderr, "       oneagent-rc adopted [--agents codex,claude-code] [--timeout seconds]")
}

func repositoryRoot() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory, nil
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "", errors.New("could not locate the OneAgent repository root")
		}
		directory = parent
	}
}

func verifyAgents(root string, args []string, adopted bool) error {
	flags := flag.NewFlagSet("oneagent-rc", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	idsValue := strings.Join(defaultAgents, ",")
	if adopted {
		idsValue = "codex,claude-code"
	}
	ids := flags.String("agents", idsValue, "comma-separated Agent IDs")
	registry := flags.String("registry", "", "npm mirror id or HTTPS URL")
	timeout := flags.Int("timeout", 900, "seconds per package operation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	agentIDs := splitIDs(*ids)
	if len(agentIDs) == 0 {
		return errors.New("at least one Agent is required")
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return err
	}
	for _, id := range agentIDs {
		agent, ok := manifest.Agents[id]
		if !ok || agent.ConfigMode != "auto" || agent.Package == nil {
			return fmt.Errorf("%s is not an installable auto Agent", id)
		}
		if agent.Package.Manager != "npm" {
			return fmt.Errorf("%s uses an optional package runtime; omit it from this check", id)
		}
	}

	isolated, cleanup, err := newIsolation(root)
	if err != nil {
		return err
	}
	defer cleanup()
	ctx := context.Background()
	installArgs := []string{
		"--agent", strings.Join(agentIDs, ","),
		"--install-agent", "--locked-version", "--check-agent-only", "--json",
		"--home", isolated.home,
	}
	if *registry != "" {
		installArgs = append(installArgs, "--registry", *registry)
	}
	result, err := runCLI(ctx, isolated, installArgs...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("locked Agent installation failed: %s", compact(result.Stdout+" "+result.Stderr))
	}
	var payload installPayload
	if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
		return fmt.Errorf("decode OneAgent install result: %w", err)
	}
	if !payload.OK {
		return fmt.Errorf("locked Agent installation reported failure: %s", compact(payload.Log))
	}
	byID := make(map[string]installResult, len(payload.Results))
	for _, item := range payload.Results {
		byID[item.Agent] = item
	}
	for _, id := range agentIDs {
		agent := manifest.Agents[id]
		item, ok := byID[id]
		if !ok || item.Status != "installed" {
			return fmt.Errorf("%s was not installed (status %q)", id, item.Status)
		}
		executable, ok := lookPath(isolated.env, agent.Command)
		if !ok {
			return fmt.Errorf("%s did not land on the isolated PATH", id)
		}
		versionValue, err := commandVersion(ctx, isolated.env, executable, agent.VersionArgs)
		if err != nil {
			return fmt.Errorf("read %s version: %w", id, err)
		}
		if versionValue != agent.Package.Version {
			return fmt.Errorf("%s reports %s, lock requires %s", id, versionValue, agent.Package.Version)
		}
		fmt.Printf("%s: %s\n", id, versionValue)
	}
	if adopted {
		return checkAdoption(ctx, isolated, agentIDs, time.Duration(*timeout)*time.Second)
	}
	return nil
}

func checkAdoption(ctx context.Context, isolated isolation, agentIDs []string, timeout time.Duration) error {
	const key = "oneagent-discard-key"
	configure := []string{
		"--agent", strings.Join(agentIDs, ","),
		"--provider", "custom", "--api-base-url", "http://127.0.0.1:9/openai",
		"--api-key", key, "--model", "oneagent-discard-model", "--skip-test", "--json",
		"--home", isolated.home,
	}
	result, err := runCLI(ctx, isolated, configure...)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("discard-endpoint configuration failed: %s", compact(result.Stdout+" "+result.Stderr))
	}
	for _, id := range agentIDs {
		command, argv, ok := adoptionCommand(id)
		if !ok {
			continue
		}
		executable, found := lookPath(isolated.env, command)
		if !found {
			return fmt.Errorf("%s is not on the isolated PATH", id)
		}
		env := append([]string(nil), isolated.env...)
		env = appendEnv(env, "CI", "1")
		env = appendEnv(env, "NO_COLOR", "1")
		env = appendEnv(env, "TERM", "dumb")
		env = appendEnv(env, "ONEAGENT_API_KEY_CODEX", key)
		commandArgs := append([]string{executable}, argv...)
		commandResult, runErr := runWithEnv(ctx, env, timeout, commandArgs...)
		output := compact(commandResult.Stdout + " " + commandResult.Stderr)
		if runErr != nil {
			return fmt.Errorf("%s: %w", id, runErr)
		}
		adopted, reason := classifyAdoption(output)
		if !adopted {
			return fmt.Errorf("%s did not adopt its configuration: %s", id, reason)
		}
		fmt.Printf("%s: %s\n", id, reason)
	}
	return nil
}

func adoptionCommand(id string) (string, []string, bool) {
	switch id {
	case "codex":
		return "codex", []string{"exec", "Reply with the single word: ready"}, true
	case "claude-code":
		return "claude", []string{"-p", "Reply with the single word: ready"}, true
	default:
		return "", nil, false
	}
}

func classifyAdoption(output string) (bool, string) {
	lowered := strings.ToLower(output)
	for _, marker := range []string{"not logged in", "please run /login", "login required", "authentication required", "api key not found", "missing api key", "unauthorized", "no credentials"} {
		if strings.Contains(lowered, marker) {
			return false, "auth/login error (configuration was not adopted)"
		}
	}
	for _, marker := range []string{"provider: oneagent", "reconnecting", "connection refused", "econnrefused", "could not connect", "failed to connect", "fetch failed", "network error", "unreachable", "os error 61", "os error 111"} {
		if strings.Contains(lowered, marker) {
			return true, "connection failure (configuration was adopted)"
		}
	}
	return false, "output showed neither a connection failure nor an authentication error"
}

func newIsolation(root string) (isolation, func(), error) {
	directory, err := os.MkdirTemp("", "oneagent-rc-")
	if err != nil {
		return isolation{}, func() {}, err
	}
	home := filepath.Join(directory, "home")
	prefix := filepath.Join(directory, "npm-prefix")
	if err := os.MkdirAll(home, 0o700); err != nil {
		os.RemoveAll(directory)
		return isolation{}, func() {}, err
	}
	pathEntry := filepath.Join(prefix, "bin")
	if runtime.GOOS == "windows" {
		pathEntry = prefix
	}
	env := make([]string, 0)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(name)
		if strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") {
			continue
		}
		if name == "NPM_CONFIG_USERCONFIG" || name == "npm_config_userconfig" || name == "UV_CONFIG_FILE" {
			continue
		}
		env = append(env, entry)
	}
	pathValue := os.Getenv("PATH")
	env = appendEnv(env, "HOME", home)
	env = appendEnv(env, "USERPROFILE", home)
	env = appendEnv(env, "ONEAGENT_HOME", home)
	env = appendEnv(env, "NPM_CONFIG_PREFIX", prefix)
	env = appendEnv(env, "npm_config_prefix", prefix)
	env = appendEnv(env, "NPM_CONFIG_CACHE", filepath.Join(directory, "npm-cache"))
	env = appendEnv(env, "NPM_CONFIG_USERCONFIG", filepath.Join(directory, "npmrc"))
	env = appendEnv(env, "PATH", pathEntry+string(os.PathListSeparator)+pathValue)
	cli, err := findCLI(root)
	if err != nil {
		os.RemoveAll(directory)
		return isolation{}, func() {}, err
	}
	return isolation{root: directory, home: home, env: env, cli: cli}, func() { _ = os.RemoveAll(directory) }, nil
}

func findCLI(root string) (string, error) {
	if value := os.Getenv("ONEAGENT_CLI_BINARY"); value != "" {
		if info, err := os.Stat(value); err == nil && !info.IsDir() {
			return value, nil
		}
	}
	names := []string{"oneagent"}
	if runtime.GOOS == "windows" {
		names = append([]string{"oneagent.exe"}, names...)
	}
	for _, name := range names {
		candidate := filepath.Join(root, "bin", name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if value, err := exec.LookPath("oneagent"); err == nil {
		return value, nil
	}
	return "", errors.New("build the Go CLI before running the RC checks")
}

func runCLI(ctx context.Context, isolated isolation, args ...string) (execResult, error) {
	return runWithEnv(ctx, isolated.env, 15*time.Minute, append([]string{isolated.cli}, args...)...)
}

type execResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func runWithEnv(ctx context.Context, env []string, timeout time.Duration, argv ...string) (execResult, error) {
	if len(argv) == 0 {
		return execResult{}, errors.New("empty command")
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(runCtx, argv[0], argv[1:]...)
	command.Env = env
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	result := execResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	}
	if runCtx.Err() != nil {
		return result, runCtx.Err()
	}
	if err != nil {
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return result, nil
		}
		return result, err
	}
	return result, nil
}

func commandVersion(ctx context.Context, env []string, executable string, args []string) (string, error) {
	if len(args) == 0 {
		args = []string{"--version"}
	}
	argv := append([]string{executable}, args...)
	result, err := runWithEnv(ctx, env, 30*time.Second, argv...)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("exit code %d: %s", result.ExitCode, compact(result.Stdout+" "+result.Stderr))
	}
	value := install.VersionFromOutput(result.Stdout + "\n" + result.Stderr)
	if value == "" {
		return "", fmt.Errorf("no semantic version in %q", compact(result.Stdout+" "+result.Stderr))
	}
	return value, nil
}

func lookPath(env []string, command string) (string, bool) {
	pathValue := ""
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok && name == "PATH" {
			pathValue = value
			break
		}
	}
	for _, directory := range filepath.SplitList(pathValue) {
		candidate := filepath.Join(directory, command)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, true
		}
		if runtime.GOOS == "windows" {
			for _, suffix := range []string{".exe", ".cmd", ".bat"} {
				candidate = filepath.Join(directory, command+suffix)
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					return candidate, true
				}
			}
		}
	}
	return "", false
}

func appendEnv(values []string, name, value string) []string {
	filtered := make([]string, 0, len(values)+1)
	for _, entry := range values {
		if key, _, ok := strings.Cut(entry, "="); !ok || key != name {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, name+"="+value)
}

func splitIDs(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func compact(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 500 {
		return value[len(value)-500:]
	}
	return value
}
