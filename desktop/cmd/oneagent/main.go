// Command oneagent is the Go core's command-line interface.
//
// It links internal/app and nothing else: no Wails, no GTK, no WebView. That is
// deliberate and is the reason the use case layer has no transport of its own --
// a headless server or a CI job must be able to install and configure an Agent
// without a display, which is exactly what the Python CLI could do.
//
// The flags mirror oneagent/cli.py because they are an external contract that
// scripts and the installers already use.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/desktop/internal/app"
	"github.com/MaimoryLab/OneAgent/desktop/internal/oerr"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is separated from main so a test can drive it and read its exit code
// instead of ending the process.
func run(argv []string, stdout, stderr *os.File) int {
	if len(argv) > 0 && argv[0] == "agent" {
		return runAgent(argv[1:], stdout, stderr)
	}
	if len(argv) > 0 && argv[0] == "status" {
		return runStatus(argv[1:], stdout, stderr)
	}
	return runInstall(argv, stdout, stderr)
}

// options carries the flags shared by the install path.
type installFlags struct {
	agents         string
	provider       string
	apiBaseURL     string
	apiKey         string
	model          string
	smallFastModel string
	installAgent   bool
	checkAgentOnly bool
	skipTest       bool
	lockedVersion  bool
	latest         bool
	registry       string
	jsonOutput     bool
	home           string
	timeout        time.Duration
}

func runInstall(argv []string, stdout, stderr *os.File) int {
	set := flag.NewFlagSet("oneagent", flag.ContinueOnError)
	set.SetOutput(stderr)
	options := installFlags{}
	set.StringVar(&options.agents, "agent", "", "Agent ID; comma-separated for several")
	set.StringVar(&options.provider, "provider", "ppio", "Provider ID")
	set.StringVar(&options.apiBaseURL, "api-base-url", "", "Custom endpoint")
	set.StringVar(&options.apiKey, "api-key", "", "API key; prefer ONEAGENT_API_KEY")
	set.StringVar(&options.model, "model", "", "Defaults to a model the provider lists")
	set.StringVar(&options.smallFastModel, "small-fast-model", "", "Claude Code's background model")
	set.BoolVar(&options.installAgent, "install-agent", false, "Let OneAgent install the package")
	set.BoolVar(&options.checkAgentOnly, "check-agent-only", false, "Only report what is present")
	set.BoolVar(&options.skipTest, "skip-test", false, "Skip every network round trip")
	set.BoolVar(&options.lockedVersion, "locked-version", false, "Reinstall when the version differs from the pin")
	set.BoolVar(&options.latest, "latest", false, "Install the floating tag")
	set.StringVar(&options.registry, "registry", "", "Mirror id or https:// URL")
	set.BoolVar(&options.jsonOutput, "json", false, "Print the raw response")
	set.StringVar(&options.home, "home", "", "")
	set.DurationVar(&options.timeout, "timeout", 180*time.Second, "Per-operation timeout")
	if err := set.Parse(argv); err != nil {
		return oerr.ExitCodeFor("INVALID_REQUEST")
	}

	ids := []string{}
	for _, part := range strings.Split(options.agents, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			ids = append(ids, trimmed)
		}
	}

	service := app.NewService(newRuntime(options.home), options.timeout)
	// The key may come from the environment so it never has to appear in argv,
	// where it would be visible in a process list.
	apiKey := options.apiKey
	if apiKey == "" {
		apiKey = os.Getenv("ONEAGENT_API_KEY")
	}

	result, err := service.Install(app.InstallOptions{
		Agents: ids, Provider: options.provider, APIBaseURL: options.apiBaseURL,
		APIKey: apiKey, Model: options.model, SmallFastModel: options.smallFastModel,
		Configure: !options.checkAgentOnly, InstallAgent: options.installAgent,
		CheckAgentOnly: options.checkAgentOnly, SkipTest: options.skipTest,
		LockedVersion: options.lockedVersion, Latest: options.latest,
		Timeout: options.timeout, Registry: options.registry,
	})
	if err != nil {
		return reportError(err, options.jsonOutput, apiKey, stdout, stderr)
	}

	if options.jsonOutput {
		printJSON(stdout, result)
		return result.Code
	}
	for _, entry := range result.Results {
		line := fmt.Sprintf("[oneagent] %-14s %s", entry.Agent, entry.Status)
		if entry.Message != "" {
			line += ": " + entry.Message
		}
		fmt.Fprintln(stdout, line)
	}
	if result.Next != "" {
		fmt.Fprintf(stdout, "[oneagent] next:\n%s\n", result.Next)
	}
	return result.Code
}

func runAgent(argv []string, stdout, stderr *os.File) int {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, "usage: oneagent agent (list|set) ...")
		return oerr.ExitCodeFor("INVALID_REQUEST")
	}
	set := flag.NewFlagSet("oneagent agent", flag.ContinueOnError)
	set.SetOutput(stderr)
	var provider, apiBaseURL, apiKey, model, profileID, home string
	var jsonOutput bool
	set.StringVar(&provider, "provider", "ppio", "Provider ID")
	set.StringVar(&apiBaseURL, "api-base-url", "", "Custom endpoint")
	set.StringVar(&apiKey, "api-key", "", "API key")
	set.StringVar(&model, "model", "", "Model ID")
	set.StringVar(&profileID, "profile", "", "Reuse the key saved for this profile")
	set.BoolVar(&jsonOutput, "json", false, "Print the raw response")
	set.StringVar(&home, "home", "", "")

	action := argv[0]
	rest := argv[1:]
	agentID := ""
	if action == "set" {
		if len(rest) == 0 || strings.HasPrefix(rest[0], "-") {
			fmt.Fprintln(stderr, "usage: oneagent agent set <agent-id> [flags]")
			return oerr.ExitCodeFor("INVALID_REQUEST")
		}
		agentID, rest = rest[0], rest[1:]
	}
	if err := set.Parse(rest); err != nil {
		return oerr.ExitCodeFor("INVALID_REQUEST")
	}

	service := app.NewService(newRuntime(home), 180*time.Second)

	switch action {
	case "list":
		bindings, err := service.Store.ListBindings()
		if err != nil {
			return reportError(err, jsonOutput, apiKey, stdout, stderr)
		}
		if jsonOutput {
			printJSON(stdout, bindings)
			return 0
		}
		if len(bindings) == 0 {
			fmt.Fprintln(stdout, "[oneagent] no Agent has been configured yet")
			return 0
		}
		for _, id := range sortedNames(bindings) {
			fmt.Fprintf(stdout, "%-14s %-10s %s\n",
				id, bindings[id].GetString("provider"), bindings[id].GetString("model"))
		}
		return 0

	case "set":
		if apiKey == "" {
			apiKey = os.Getenv("ONEAGENT_API_KEY")
		}
		if apiKey == "" && profileID != "" {
			// Reuse the key already stored for a profile, so switching an Agent
			// back to a saved provider does not require pasting it again.
			stored, err := service.Store.ReadSecret(profileID)
			if err != nil {
				return reportError(err, jsonOutput, apiKey, stdout, stderr)
			}
			apiKey = stored
		}
		result, err := service.Activate(app.ActivateOptions{
			AgentID: agentID, Provider: provider, APIBaseURL: apiBaseURL,
			APIKey: apiKey, Model: model, Timeout: 180 * time.Second,
		})
		if err != nil {
			return reportError(err, jsonOutput, apiKey, stdout, stderr)
		}
		if jsonOutput {
			printJSON(stdout, result)
			return 0
		}
		fmt.Fprintf(stdout, "[oneagent] %s -> %s / %s\n", result.Agent, result.Provider, result.Model)
		fmt.Fprintf(stdout, "[oneagent] %s\n", result.Restart)
		fmt.Fprintf(stdout, "[oneagent] next: %s\n", result.Next)
		return 0

	default:
		fmt.Fprintf(stderr, "unknown action %q\n", action)
		return oerr.ExitCodeFor("INVALID_REQUEST")
	}
}

func runStatus(argv []string, stdout, stderr *os.File) int {
	set := flag.NewFlagSet("oneagent status", flag.ContinueOnError)
	set.SetOutput(stderr)
	var home string
	set.StringVar(&home, "home", "", "")
	if err := set.Parse(argv); err != nil {
		return oerr.ExitCodeFor("INVALID_REQUEST")
	}
	service := app.NewService(newRuntime(home), 30*time.Second)
	status, err := service.Status()
	if err != nil {
		return reportError(err, true, "", stdout, stderr)
	}
	printJSON(stdout, status)
	return 0
}

// newRuntime builds the runtime, honouring --home so a review or a test can point
// the whole operation at a scratch directory.
func newRuntime(home string) *runtime.Runtime {
	if home == "" {
		return runtime.New()
	}
	return runtime.New(runtime.WithHome(home))
}

// reportError prints a failure in the shape the CLI contract promises and returns
// the exit code for it. The key is redacted because a message can quote a URL that
// carried one.
func reportError(err error, asJSON bool, apiKey string, stdout, stderr *os.File) int {
	var converted *oerr.Error
	if !errors.As(err, &converted) {
		converted = oerr.Newf("INTERNAL_ERROR", "%v", err)
	}
	if asJSON {
		printJSON(stdout, converted.Payload())
		return converted.ExitCode
	}
	message := converted.Message
	if apiKey != "" {
		message = strings.ReplaceAll(message, apiKey, "[redacted]")
	}
	fmt.Fprintf(stderr, "[oneagent] error: %s\n", message)
	return converted.ExitCode
}

func printJSON(out *os.File, value any) {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	// The status payload holds user-entered endpoints; HTML-escaping them would
	// print &amp; where the user typed &.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintf(os.Stderr, "[oneagent] cannot encode the response: %v\n", err)
	}
}

func sortedNames[V any](items map[string]V) []string {
	names := make([]string, 0, len(items))
	for name := range items {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
