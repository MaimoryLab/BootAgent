package app

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/MaimoryLab/OneAgent/internal/catalog"
	"github.com/MaimoryLab/OneAgent/internal/install"
)

// latestVersionConcurrency bounds how many registry lookups run at once. The
// npm registry rate-limits by client, and a status poll asking for every Agent
// at the same instant is exactly the shape that trips it.
const latestVersionConcurrency = 3

// latestVersionTTL is how long registry answers stay usable. Agent releases are
// a daily event at most, while the status call runs on every poll and on every
// refresh the user triggers -- without a cache that is a request per Agent per
// poll, which is both wasteful and the fastest way to get rate-limited.
const latestVersionTTL = 30 * time.Minute

// latestVersionCache holds one batch of answers and when they were taken.
type latestVersionCache struct {
	mu      sync.Mutex
	taken   time.Time
	answers map[string]*string
}

// read returns the cached answers while they are fresh. The second result
// distinguishes a fresh empty batch -- every lookup failed, or nothing is
// installed -- from having nothing cached at all, so a failing registry is not
// retried on every poll.
func (c *latestVersionCache) read() (map[string]*string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.taken.IsZero() || time.Since(c.taken) > latestVersionTTL {
		return nil, false
	}
	return c.answers, true
}

func (c *latestVersionCache) write(answers map[string]*string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.taken = time.Now()
	c.answers = answers
}

// latestAgentVersions asks the registry for each installed npm Agent's newest
// published version. Only installed Agents are queried: the answer exists to
// mark an update as available, and there is nothing to update on a machine that
// does not have the Agent.
//
// Every failure is silent by design. This decorates the UI, so an offline
// machine, a rate-limited registry, or a captive portal should leave the row
// looking exactly as it did rather than turning a status poll into an error.
// The whole batch is bounded by one timeout so a hanging registry cannot hold
// the status call open.
func (u *UseCases) latestAgentVersions(ctx context.Context, manifest catalog.Manifest, lookup func(string) (string, bool)) map[string]*string {
	if u == nil {
		return nil
	}
	if cached, fresh := u.latestVersions.read(); fresh {
		return cached
	}
	// httpDoer is only set when a caller injected one, which in production is
	// nobody: it exists so tests can answer without a network. Falling back to a
	// bounded client here matches downloadArtifact, and without it this would be
	// dead code outside the test suite.
	client := u.httpDoer
	if client == nil {
		client = &http.Client{Timeout: install.LatestVersionTimeout}
	}
	type query struct {
		id    string
		agent catalog.Agent
	}
	queries := make([]query, 0, len(manifest.Agents))
	for id, agent := range manifest.Agents {
		if agent.Package == nil || agent.Package.Manager != "npm" || agent.Command == "" {
			continue
		}
		if _, installed := lookup(agent.Command); !installed {
			continue
		}
		queries = append(queries, query{id: id, agent: agent})
	}
	if len(queries) == 0 {
		// Nothing installed is a real answer, and caching it keeps a first run from
		// re-deciding this on every poll.
		u.latestVersions.write(nil)
		return nil
	}
	registry, err := install.ResolveRegistry(u.packageRegistry(ctx, ""))
	if err != nil {
		return nil
	}
	// One deadline for the batch, not per lookup: the caller is a status poll,
	// and its total cost is what the user feels.
	lookupCtx, cancel := context.WithTimeout(ctx, install.LatestVersionTimeout)
	defer cancel()

	var mu sync.Mutex
	found := make(map[string]*string, len(queries))
	var group sync.WaitGroup
	tokens := make(chan struct{}, latestVersionConcurrency)
	for _, item := range queries {
		group.Add(1)
		go func(id string, agent catalog.Agent) {
			defer group.Done()
			tokens <- struct{}{}
			defer func() { <-tokens }()
			version := install.LatestVersion(lookupCtx, client, agent, registry)
			if version == "" {
				return
			}
			mu.Lock()
			found[id] = &version
			mu.Unlock()
		}(item.id, item.agent)
	}
	group.Wait()
	// A cancelled status poll answers nothing and must not be cached as "the
	// registry said nothing", or one cancelled refresh would suppress the dot for
	// the whole TTL.
	if lookupCtx.Err() != nil && len(found) == 0 {
		return nil
	}
	if len(found) == 0 {
		u.latestVersions.write(nil)
		return nil
	}
	u.latestVersions.write(found)
	return found
}
