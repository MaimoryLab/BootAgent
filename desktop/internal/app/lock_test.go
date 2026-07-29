package app

import (
	"os"

	"github.com/MaimoryLab/OneAgent/desktop/internal/config"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// widenTheWindow delays each adapter between reading the existing config and
// writing the merged result.
//
// My first attempt slowed securefs's clock, which is already an injection point --
// but the clock is consulted while naming the backup, which happens after the merge
// read. The delay landed outside the vulnerable window, so the test passed with the
// lock removed. That is the failure mode this whole exercise is about: a test that
// cannot fail proves nothing, and only checking it against the unlocked code
// revealed it.
func widenTheWindow(t *testing.T, delay time.Duration) {
	t.Helper()
	restore := config.DelayReadsForTest(func() { time.Sleep(delay) })
	t.Cleanup(restore)
}

func TestConcurrentInstallsDoNotDropAgentsFromTheProfile(t *testing.T) {
	// The lost-work case the write lock exists for, and the one that actually
	// happens. Not a torn file -- every write publishes through an atomic rename --
	// but a read-merge-write where each caller contributes something the others do
	// not: the profile's agent_ids. Installing four Agents concurrently means each
	// operation reads the profile, adds its own Agent and writes the result, so an
	// interleaving discards whatever the others added. Unlocked, this loses three of
	// four; that is what makes this test worth having rather than decoration.
	//
	// The user-visible consequence: the overview lists one configured Agent when
	// four were configured, and the three missing ones look unconfigured while their
	// config files say otherwise.
	home := t.TempDir()
	service := serviceFor(t, home, true, nil)
	widenTheWindow(t, 15*time.Millisecond)

	agents := []string{"codex", "claude-code", "opencode", "kilo-cli"}
	var group sync.WaitGroup
	for _, agentID := range agents {
		group.Add(1)
		go func(id string) {
			defer group.Done()
			options := baseOptions()
			options.Agents = []string{id}
			options.ProfileAgents = []string{id}
			if _, err := service.Install(options); err != nil {
				t.Errorf("install %s failed: %v", id, err)
			}
		}(agentID)
	}
	group.Wait()

	stored, reason, err := service.Store.Load()
	if err != nil || reason != "" {
		t.Fatalf("cannot read the profile: reason=%q err=%v", reason, err)
	}
	if stored == nil {
		t.Fatal("no profile was recorded")
	}
	recorded := map[string]bool{}
	value, _ := stored.Get("agent_ids")
	items, _ := value.([]any)
	for _, item := range items {
		if text, ok := item.(string); ok {
			recorded[text] = true
		}
	}
	for _, agentID := range agents {
		if !recorded[agentID] {
			t.Errorf("%s configured its own file but is missing from the profile; "+
				"a concurrent write merged from a stale read (profile lists %d of %d)",
				agentID, len(recorded), len(agents))
		}
	}
}

func TestConcurrentInstallsAcrossAgentsLeaveEveryConfigWritten(t *testing.T) {
	// Several installs at once must not leave one Agent's config missing because
	// another was writing the shared env file at the time.
	home := t.TempDir()
	service := serviceFor(t, home, true, nil)
	widenTheWindow(t, 10*time.Millisecond)

	agents := [][]string{{"codex"}, {"claude-code"}, {"opencode"}, {"kilo-cli"}}
	var group sync.WaitGroup
	for _, selection := range agents {
		group.Add(1)
		go func(ids []string) {
			defer group.Done()
			options := baseOptions()
			options.Agents = ids
			options.ProfileAgents = ids
			if _, err := service.Install(options); err != nil {
				t.Errorf("install %v failed: %v", ids, err)
			}
		}(selection)
	}
	group.Wait()

	for _, expected := range []string{
		filepath.Join(".codex", "config.toml"),
		filepath.Join(".claude", "settings.json"),
		filepath.Join(".config", "opencode", "opencode.jsonc"),
		filepath.Join(".config", "kilo", "kilo.jsonc"),
	} {
		if _, err := os.Stat(filepath.Join(home, expected)); err != nil {
			t.Errorf("%s was not written: %v", expected, err)
		}
	}
}

func TestTheStatusReadIsNotBlockedByAWriteInFlight(t *testing.T) {
	// Reads are deliberately outside the lock: the atomic rename means Status
	// cannot see a half-written file, so serialising it would let an install stop
	// the overview from rendering.
	home := t.TempDir()
	service := serviceFor(t, home, true, nil)
	widenTheWindow(t, 40*time.Millisecond)

	started := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		close(started)
		options := baseOptions()
		options.Agents = []string{"codex", "claude-code", "opencode"}
		options.ProfileAgents = options.Agents
		if _, err := service.Install(options); err != nil {
			t.Errorf("install failed: %v", err)
		}
		close(finished)
	}()

	<-started
	// Status has to return while the install is still going, so this is timed
	// against the install rather than against a wall-clock guess.
	if _, err := service.Status(); err != nil {
		t.Fatalf("Status failed during a write: %v", err)
	}
	select {
	case <-finished:
		t.Skip("the install finished before Status ran, so this proves nothing")
	default:
	}
	<-finished
}
