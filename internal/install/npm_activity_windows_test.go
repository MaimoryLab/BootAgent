package install

import (
	"slices"
	"testing"

	"github.com/MaimoryLab/BootAgent/internal/platform"
)

// The same layout as TestNPMActivityPathsFollowWindowsLayoutAndOverrides, with
// the drive-letter paths a real Windows install has, which only a Windows host
// treats as absolute.
func TestNPMActivityPathsUseRealWindowsLocations(t *testing.T) {
	runtime := Runtime{
		Home:     `C:\Users\u`,
		Platform: platform.For("windows", "amd64"),
		Env:      map[string]string{"APPDATA": `C:\Users\u\AppData\Roaming`, "LOCALAPPDATA": `C:\Users\u\AppData\Local`},
	}
	paths := NPMActivityPaths(runtime, `C:\Program Files\nodejs\npm.cmd`, "@anthropic-ai/claude-code", nil)
	for _, want := range []string{
		`C:\Users\u\AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code`,
		`C:\Program Files\nodejs\node_modules\@anthropic-ai\claude-code`,
		`C:\Users\u\AppData\Local\npm-cache\_cacache\tmp`,
	} {
		if !slices.Contains(paths, want) {
			t.Errorf("paths = %v, want %s among them", paths, want)
		}
	}
}
