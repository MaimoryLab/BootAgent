package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/MaimoryLab/OneAgent/internal/app"
	"github.com/MaimoryLab/OneAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/OneAgent/internal/errors"
	"github.com/MaimoryLab/OneAgent/internal/platform"
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
	if args[0] == "--version" || args[0] == "version" {
		_, _ = fmt.Fprintln(stdout, version.Version)
		return 0
	}
	switch args[0] {
	case "status":
		return runStatus(args[1:], stdout, stderr)
	case "agent":
		return runAgent(args[1:], stdout, stderr)
	default:
		// Keep the common check-only compatibility flags available while the
		// Python wrapper still points at its existing implementation. This Go
		// command is safe to opt into and never installs or writes credentials.
		return runCompatibilityFlags(args, stdout, stderr)
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
		return writeError(stdout, err, *jsonOutput)
	}
	return writeValue(stdout, status, *jsonOutput)
}

func runAgent(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "list" {
		flags := flag.NewFlagSet("agent list", flag.ContinueOnError)
		flags.SetOutput(stderr)
		jsonOutput := flags.Bool("json", false, "write JSON")
		remaining := args
		if len(args) > 0 && args[0] == "list" {
			remaining = args[1:]
		}
		if err := flags.Parse(remaining); err != nil {
			return oneerrors.ExitCodes[oneerrors.InvalidRequest]
		}
		manifest, err := catalog.LoadEmbedded()
		if err != nil {
			return writeError(stdout, err, *jsonOutput)
		}
		items := catalog.PublicCatalog(manifest, platform.Current().OS)
		if *jsonOutput {
			return writeJSON(stdout, map[string]any{"agents": items})
		}
		for _, item := range items {
			_, _ = fmt.Fprintf(stdout, "%s\t%s\n", item.ID, item.Name)
		}
		return 0
	}
	return writeError(stdout, oneerrors.New(oneerrors.InvalidRequest, "Unknown agent command"), false)
}

func runCompatibilityFlags(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("oneagent", flag.ContinueOnError)
	flags.SetOutput(stderr)
	agentID := flags.String("agent", "", "Agent ID")
	checkOnly := flags.Bool("check-agent-only", false, "only inspect the Agent")
	jsonOutput := flags.Bool("json", false, "write JSON")
	home := flags.String("home", "", "override the home directory")
	if err := flags.Parse(args); err != nil {
		return oneerrors.ExitCodes[oneerrors.InvalidRequest]
	}
	if !*checkOnly {
		return writeError(stdout, oneerrors.New(oneerrors.InvalidRequest, "The migration CLI currently supports status and check-agent-only only"), *jsonOutput)
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return writeError(stdout, err, *jsonOutput)
	}
	if *agentID == "" {
		return writeError(stdout, oneerrors.New(oneerrors.InvalidRequest, "--agent is required with --check-agent-only"), *jsonOutput)
	}
	agent, ok := manifest.Agents[*agentID]
	if !ok {
		return writeError(stdout, oneerrors.New(oneerrors.InvalidRequest, "Unknown Agent: "+*agentID), *jsonOutput)
	}
	_, installed := app.NewUseCases(app.StatusOptions{Home: *home, Platform: platform.Current()}).LookupForCLI(agent.Command)
	payload := map[string]any{
		"ok":        true,
		"agent":     *agentID,
		"installed": installed,
		"guideOnly": agent.ConfigMode == "guide",
	}
	if *jsonOutput {
		return writeJSON(stdout, payload)
	}
	_, _ = fmt.Fprintf(stdout, "%s: %t\n", agent.Name, installed)
	return 0
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

func writeError(stdout io.Writer, err error, jsonOutput bool) int {
	oneErr := oneerrors.As(err)
	if jsonOutput {
		_ = writeJSON(stdout, oneErr.APIShape())
	} else {
		_, _ = fmt.Fprintln(stdout, oneErr.Message)
	}
	return oneErr.ExitCode
}

func printUsage(stdout io.Writer) {
	_, _ = fmt.Fprintln(stdout, "OneAgent Go migration CLI")
	_, _ = fmt.Fprintln(stdout, "Usage: oneagent status [--json] | oneagent agent list [--json]")
	_, _ = fmt.Fprintln(stdout, "       oneagent --agent <id> --check-agent-only [--json]")
}

// flagsContext is kept as a function so future CLI cancellation wiring can be
// added without changing command handlers.
func flagsContext() context.Context { return context.Background() }
