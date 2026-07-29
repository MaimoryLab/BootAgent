package catalog_test

import (
	"sync"
	"testing"

	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
)

// The manifest is read by several Wails bindings, each on its own goroutine. An
// earlier Load() cached into a package variable behind a nil check, which -race
// reports as a data race under exactly that access pattern -- and it would first
// have surfaced at the moment the orchestration layer was wired into the shell,
// with the shell as the apparent cause. Reverting Load to the check-then-assign
// form makes this test fail with WARNING: DATA RACE, which is what keeps it from
// being decoration.
func TestLoadIsSafeForConcurrentReaders(t *testing.T) {
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			manifest, err := catalog.Load()
			if err != nil {
				t.Error(err)
				return
			}
			if len(manifest.Agents) == 0 {
				t.Error("the manifest came back empty")
			}
		}()
	}
	group.Wait()
}
