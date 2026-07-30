package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/app"
	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/platform"
	"github.com/MaimoryLab/OneAgent/internal/process"
	profileStore "github.com/MaimoryLab/OneAgent/internal/profile"
	"github.com/MaimoryLab/OneAgent/internal/provider"
	"github.com/MaimoryLab/OneAgent/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stdout)
		return 0
	}
	switch args[0] {
	case "--version", "version":
		_, _ = fmt.Fprintln(stdout, version.Version)
		return 0
	case "status":
		return runStatus(args[1:], stdout, stderr)
	case "agent":
		return runAgent(args[1:], stdout, stderr)
	default:
		return runInstall(args, stdout, stderr)
	}
}

func runStatus(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "write JSON")
	home := flags.String("home", "", "override the home directory")
	if err := flags.Parse(args); err != nil {
		return oneerrors.ExitCodes[oneerrors.InvalidRequest]
	}
	info := platform.Current()
	core := app.NewUseCases(app.StatusOptions{Home: *home, Platform: info})
	status, err := core.GetStatus(flagsContext())
	if err != nil {
		return writeError(stdout, stderr, err, *jsonOutput, "")
	}
	return writeValue(stdout, status, *jsonOutput)
}

func runAgent(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		flags := flag.NewFlagSet("agent list", flag.ContinueOnError)
		flags.SetOutput(stderr)
		jsonOutput := flags.Bool("json", false, "write JSON")
		home := flags.String("home", "", "override the home directory")
		if err := flags.Parse(args[1:]); err != nil {
			return oneerrors.ExitCodes[oneerrors.InvalidRequest]
		}
		core := newCLIUseCases(*home)
		bindings, err := core.ListAgentBindings(flagsContext())
		if err != nil {
			return writeError(stdout, stderr, err, *jsonOutput, "")
		}
		if *jsonOutput {
			return writeJSON(stdout, map[string]any{"ok": true, "agents": bindings})
		}
		if len(bindings) == 0 {
			_, _ = fmt.Fprintln(stdout, "[oneagent] no Agent has been configured yet")
			return 0
		}
		for _, agentID := range sortedBindingIDs(bindings) {
			binding := bindings[agentID]
			_, _ = fmt.Fprintf(stdout, "%-14s %-10s %s\n", agentID, binding.Provider, binding.Model)
		}
		return 0
	case "set":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			return writeError(stdout, stderr, oneerrors.New(oneerrors.InvalidRequest, "agent_id is required"), false, "")
		}
		flags := flag.NewFlagSet("agent set", flag.ContinueOnError)
		flags.SetOutput(stderr)
		providerID := flags.String("provider", "ppio", "Provider ID")
		baseURL := flags.String("api-base-url", "", "custom Provider base URL")
		apiKey := flags.String("api-key", "", "API key")
		model := flags.String("model", "", "model ID")
		profileID := flags.String("profile", "", "reuse a saved profile key")
		smallFast := flags.String("small-fast-model", "", "Claude Code fast model")
		jsonOutput := flags.Bool("json", false, "write JSON")
		home := flags.String("home", "", "override the home directory")
		if err := flags.Parse(args[2:]); err != nil {
			return oneerrors.ExitCodes[oneerrors.InvalidRequest]
		}
		key := *apiKey
		if key == "" {
			key = os.Getenv("ONEAGENT_API_KEY")
		}
		core := newCLIUseCases(*home)
		result, err := core.ActivateAgent(flagsContext(), app.ActivateAgentOptions{
			AgentID: args[1], Provider: *providerID, APIBaseURL: *baseURL,
			APIKey: key, Model: *model, ProfileID: *profileID, SmallFastModel: *smallFast,
		})
		if err != nil {
			return writeError(stdout, stderr, err, *jsonOutput, key)
		}
		if *jsonOutput {
			payload := map[string]any{
				"ok": true, "agent": result.AgentID, "config": result.Config,
				"provider": result.Provider, "model": result.Model, "binding": result.Binding,
				"restart": result.Restart, "next": result.Next,
			}
			return writeJSON(stdout, payload)
		}
		_, _ = fmt.Fprintf(stdout, "[oneagent] %s -> %s / %s\n", result.AgentID, result.Provider, result.Model)
		_, _ = fmt.Fprintln(stdout, "[oneagent] "+result.Restart)
		_, _ = fmt.Fprintln(stdout, "[oneagent] next: "+result.Next)
		return 0
	default:
		return writeError(stdout, stderr, oneerrors.New(oneerrors.InvalidRequest, "Unknown agent command"), false, "")
	}
}

type installCLIFlags struct {
	Agent          string
	Provider       string
	APIBaseURL     string
	APIKey         string
	Model          string
	SmallFastModel string
	RegisterURL    string
	Channel        string
	InstallAgent   bool
	CheckOnly      bool
	SkipTest       bool
	NoOpen         bool
	JSON           bool
	Locked         bool
	Latest         bool
	Registry       string
	Home           string
	Timeout        int
}

func runInstall(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("oneagent", flag.ContinueOnError)
	flags.SetOutput(stderr)
	options := installCLIFlags{}
	flags.StringVar(&options.Agent, "agent", "codex", "Agent ID; comma-separated for several")
	flags.StringVar(&options.Provider, "provider", "ppio", "Provider ID")
	flags.StringVar(&options.APIBaseURL, "api-base-url", "", "custom Provider base URL")
	flags.StringVar(&options.APIKey, "api-key", "", "API key")
	flags.StringVar(&options.Model, "model", "", "model ID")
	flags.StringVar(&options.SmallFastModel, "small-fast-model", "", "Claude Code fast model")
	flags.StringVar(&options.RegisterURL, "register-url", "", "registration URL")
	flags.StringVar(&options.Channel, "channel", "direct", "launch channel")
	flags.BoolVar(&options.InstallAgent, "install-agent", false, "install missing Agent packages")
	flags.BoolVar(&options.CheckOnly, "check-agent-only", false, "only inspect Agents")
	flags.BoolVar(&options.SkipTest, "skip-test", false, "skip Provider probes")
	flags.BoolVar(&options.NoOpen, "no-open", false, "do not open registration URL")
	flags.BoolVar(&options.JSON, "json", false, "write JSON")
	flags.BoolVar(&options.Locked, "locked-version", false, "enforce locked versions")
	flags.BoolVar(&options.Latest, "latest", false, "install latest version")
	flags.StringVar(&options.Registry, "registry", "", "package registry mirror or HTTPS URL")
	flags.StringVar(&options.Home, "home", "", "override the home directory")
	flags.IntVar(&options.Timeout, "timeout", 180, "operation timeout in seconds")
	if err := flags.Parse(args); err != nil {
		return oneerrors.ExitCodes[oneerrors.InvalidRequest]
	}
	if options.Locked && options.Latest {
		return writeError(stdout, stderr, oneerrors.New(oneerrors.InvalidRequest, "--locked-version and --latest cannot be used together"), options.JSON, "")
	}
	key, err := resolveCLIKey(options, stderr)
	if err != nil {
		return writeError(stdout, stderr, err, options.JSON, key)
	}
	agents := splitAgents(options.Agent)
	if len(agents) == 0 {
		return writeError(stdout, stderr, oneerrors.New(oneerrors.InvalidRequest, "At least one Agent is required"), options.JSON, key)
	}
	if options.Timeout <= 0 {
		return writeError(stdout, stderr, oneerrors.New(oneerrors.InvalidRequest, "timeout must be greater than zero"), options.JSON, key)
	}
	core := newCLIUseCases(options.Home)
	result, err := core.InstallAgents(flagsContext(), app.InstallAgentsOptions{
		Agents: agents, Provider: options.Provider, APIBaseURL: options.APIBaseURL,
		APIKey: key, Model: options.Model, SmallFastModel: options.SmallFastModel,
		Configure: !options.CheckOnly, InstallAgent: options.InstallAgent,
		CheckAgentOnly: options.CheckOnly, SkipTest: options.SkipTest,
		LockedVersion: options.Locked, Latest: options.Latest,
		Timeout: time.Duration(options.Timeout) * time.Second, Registry: options.Registry,
	})
	if err != nil {
		return writeError(stdout, stderr, err, options.JSON, key)
	}
	if options.JSON {
		return writeJSON(stdout, result)
	}
	if result.Log != "" {
		_, _ = fmt.Fprintln(stdout, result.Log)
	}
	for _, line := range strings.Split(result.Next, "\n") {
		if strings.TrimSpace(line) != "" {
			_, _ = fmt.Fprintln(stdout, "[oneagent] next: "+line)
		}
	}
	if result.OK {
		return 0
	}
	return result.Code
}

func splitAgents(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func newCLIUseCases(home string) *app.UseCases {
	info := platform.Current()
	current := process.Current()
	return app.NewUseCases(app.StatusOptions{
		Home: home, Platform: info, Runner: current, Environment: current.Env,
	})
}

func resolveCLIKey(options installCLIFlags, stderr io.Writer) (string, error) {
	if options.CheckOnly {
		return "", nil
	}
	if value := os.Getenv("ONEAGENT_API_KEY"); value != "" {
		return value, nil
	}
	if options.APIKey != "" {
		return options.APIKey, nil
	}
	if !stdinIsTerminal() {
		return "", oneerrors.New(oneerrors.InvalidRequest, "API key is required; set ONEAGENT_API_KEY or pass --api-key (pasting interactively needs a TTY)")
	}
	registration := options.RegisterURL
	if registration == "" {
		if home, ok := catalog.ProviderByID(options.Provider); ok {
			registration = home.Home
		} else {
			registration, _ = provider.ProviderHome("ppio")
		}
	}
	if !options.NoOpen {
		_ = openRegistrationURL(registration)
	}
	_, _ = fmt.Fprintln(stderr, "Create or copy an API key from: "+registration)
	_, _ = fmt.Fprint(stderr, "Paste API key: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", oneerrors.New(oneerrors.InvalidRequest, "API key is required")
	}
	return strings.TrimSpace(line), nil
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func openRegistrationURL(value string) error {
	parsed, err := provider.ValidateBaseURL(value)
	if err != nil {
		// Provider home URLs end in a path and satisfy the same URL safety rules;
		// preserve the stable request error if a caller supplied an unsafe value.
		return err
	}
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command, args = "open", []string{parsed}
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler", parsed}
	default:
		command, args = "xdg-open", []string{parsed}
	}
	return exec.Command(command, args...).Start()
}

func writeValue(stdout io.Writer, value any, jsonOutput bool) int {
	if jsonOutput {
		return writeJSON(stdout, value)
	}
	_, _ = fmt.Fprintln(stdout, "OneAgent status is available with --json")
	return 0
}

func writeJSON(stdout io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return oneerrors.ExitCodes[oneerrors.InternalError]
	}
	return 0
}

func writeError(stdout, stderr io.Writer, err error, jsonOutput bool, secret string) int {
	oneErr := oneerrors.As(err)
	if jsonOutput {
		if code := writeJSON(stdout, oneErr.APIShape()); code != 0 {
			return code
		}
		return oneErr.ExitCode
	}
	message := oneErr.Message
	if secret != "" {
		message = strings.ReplaceAll(message, secret, "[redacted]")
	}
	_, _ = fmt.Fprintln(stderr, "[oneagent] error: "+message)
	return oneErr.ExitCode
}

func printUsage(stdout io.Writer) {
	_, _ = fmt.Fprintln(stdout, "OneAgent Go migration CLI")
	_, _ = fmt.Fprintln(stdout, "Usage: oneagent [flags] | oneagent status [--json] | oneagent agent list|set")
}

// flagsContext is kept as a function so future CLI signal cancellation wiring
// can be added without changing command handlers.
func flagsContext() context.Context { return context.Background() }

func sortedBindingIDs(bindings map[string]profileStore.AgentBinding) []string {
	ids := make([]string, 0, len(bindings))
	for id := range bindings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
