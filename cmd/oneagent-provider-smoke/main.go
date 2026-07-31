// Command oneagent-provider-smoke runs the low-token checks for every Provider
// declared in providers.lock.json, as used by release-candidate verification.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/provider"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	flags := flag.NewFlagSet("oneagent-provider-smoke", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	providerID := flags.String("provider", "all", "Provider ID or all")
	timeout := flags.Duration("timeout", 30*time.Second, "per-request timeout")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	ids := catalog.ProviderIDs()
	if *providerID != "all" {
		ids = []string{*providerID}
	}
	client := provider.NewClientWithLimits(nil, *timeout, 1<<20)
	for _, id := range ids {
		if err := smoke(context.Background(), client, id, *timeout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return 0
}

func smoke(parent context.Context, client *provider.Client, id string, timeout time.Duration) error {
	if _, ok := catalog.ProviderByID(id); !ok {
		return fmt.Errorf("unknown Provider %q", id)
	}
	prefix := strings.ToUpper(id)
	key := os.Getenv("ONEAGENT_" + prefix + "_API_KEY")
	models := map[string]string{
		provider.ProtocolOpenAI:    os.Getenv("ONEAGENT_" + prefix + "_OPENAI_MODEL"),
		provider.ProtocolAnthropic: os.Getenv("ONEAGENT_" + prefix + "_ANTHROPIC_MODEL"),
		provider.ProtocolResponses: os.Getenv("ONEAGENT_" + prefix + "_RESPONSES_MODEL"),
	}
	for protocolID, model := range models {
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf("%s: ONEAGENT_%s_%s_MODEL is required", id, prefix, strings.ToUpper(protocolID))
		}
	}
	openAIBase := os.Getenv("ONEAGENT_" + prefix + "_OPENAI_BASE")
	anthropicBase := os.Getenv("ONEAGENT_" + prefix + "_ANTHROPIC_BASE")
	if key == "" {
		return errors.New("ONEAGENT_" + prefix + "_API_KEY is required")
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	listing, err := client.ListModels(ctx, id, key, openAIBase)
	if err != nil {
		return fmt.Errorf("%s models: %w", id, err)
	}
	if !listing.Reachable || listing.Status < 200 || listing.Status >= 300 {
		return fmt.Errorf("%s models: HTTP %d: %s", id, listing.Status, listing.Message)
	}
	fmt.Printf("%s models: HTTP %d\n", id, listing.Status)
	for _, protocolID := range []string{provider.ProtocolOpenAI, provider.ProtocolResponses, provider.ProtocolAnthropic} {
		base := openAIBase
		if protocolID == provider.ProtocolAnthropic {
			base = anthropicBase
		}
		result, probeErr := client.Probe(ctx, protocolID, id, key, models[protocolID], base)
		if probeErr != nil {
			return fmt.Errorf("%s %s: %w", id, protocolID, probeErr)
		}
		if !result.OK {
			return fmt.Errorf("%s %s: HTTP %d: %s", id, protocolID, result.Status, result.Message)
		}
		fmt.Printf("%s %s: HTTP %d\n", id, protocolID, result.Status)
	}
	return nil
}
