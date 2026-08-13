package install

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/platform"
	"github.com/MaimoryLab/BootAgent/internal/process"
	"github.com/MaimoryLab/BootAgent/internal/securefs"
)

// A Runner that answers PowerShell lookups and records the argv it is handed,
// so the Windows PATH branch can be asserted on any host without touching the
// real user environment.
type pathRunner struct {
	found    map[string]string
	calls    [][]string
	stdout   string
	exitCode int
}

func (r *pathRunner) LookPath(command string) (string, bool) {
	path, ok := r.found[command]
	return path, ok
}

func (r *pathRunner) Run(_ context.Context, argv []string, _ map[string]string, _ time.Duration) (process.Result, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	return process.Result{Args: argv, Stdout: r.stdout, ExitCode: r.exitCode}, nil
}

func windowsRuntime(home string, runner process.Runner) Runtime {
	return NewRuntime(home, platform.For("windows", "amd64"), runner, map[string]string{"USERPROFILE": home})
}

func TestPersistRuntimePathRewritesOnlyTheUserPathOnWindows(t *testing.T) {
	runner := &pathRunner{found: map[string]string{"powershell": `C:\Windows\powershell.exe`}, stdout: "updated\n"}
	managed := filepath.Join(RuntimeRoot(`C:\Users\u`), "node", "v1", "bin")
	changed, err := PersistRuntimePath(context.Background(), windowsRuntime(`C:\Users\u`, runner), securefs.New(securefs.Options{OS: "windows"}), []string{managed})
	if err != nil || !changed {
		t.Fatalf("PersistRuntimePath = %v, %v", changed, err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected one PowerShell call, got %d", len(runner.calls))
	}
	argv := runner.calls[0]
	script := strings.Join(argv, " ")
	// The directory has to reach the script, or the install silently records
	// nothing. Separators are not asserted: filepath.Join follows the host that
	// compiled the test, so a windows/amd64 build emits "\" here while this same
	// test run on macOS emits "/". Only the windows binary's output is the
	// product's behaviour.
	if !strings.Contains(script, managed) {
		t.Errorf("managed directory %q is absent from the script: %s", managed, script)
	}
	// 'User' scope only: the machine-wide PATH needs elevation and would leak
	// this install into every other account on the box.
	if !strings.Contains(script, "'Path','User'") {
		t.Errorf("script does not scope PATH to the user: %s", script)
	}
	if strings.Contains(script, "'Machine'") {
		t.Errorf("script touches the machine-wide PATH: %s", script)
	}
	// -NoProfile keeps a user's profile from altering the result; Bypass is
	// needed because the default client policy refuses to run the script at all.
	for _, flag := range []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass"} {
		if !containsArg(argv, flag) {
			t.Errorf("missing %s in %v", flag, argv)
		}
	}
}

func TestPersistRuntimePathReportsNoChangeWhenPowerShellIsSilent(t *testing.T) {
	// The script only prints "updated" when it actually rewrote PATH. Reporting
	// a change anyway would make the installer claim work it did not do.
	runner := &pathRunner{found: map[string]string{"powershell": `C:\Windows\powershell.exe`}}
	changed, err := PersistRuntimePath(context.Background(), windowsRuntime(`C:\Users\u`, runner), securefs.New(securefs.Options{OS: "windows"}), []string{`C:\Users\u\.bootagent\runtimes\node\v1\bin`})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("reported a PATH change when PowerShell printed nothing")
	}
}

func TestPersistRuntimePathFallsBackToPwsh(t *testing.T) {
	// Windows without the legacy powershell.exe still has pwsh; failing there
	// would block the install on an otherwise healthy machine.
	runner := &pathRunner{found: map[string]string{"pwsh": `C:\pwsh.exe`}, stdout: "updated"}
	if _, err := PersistRuntimePath(context.Background(), windowsRuntime(`C:\Users\u`, runner), securefs.New(securefs.Options{OS: "windows"}), []string{`C:\dir`}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != `C:\pwsh.exe` {
		t.Fatalf("did not fall back to pwsh: %v", runner.calls)
	}
}

func TestPersistRuntimePathSurfacesAMissingPowerShell(t *testing.T) {
	runner := &pathRunner{found: map[string]string{}}
	_, err := PersistRuntimePath(context.Background(), windowsRuntime(`C:\Users\u`, runner), securefs.New(securefs.Options{OS: "windows"}), []string{`C:\dir`})
	if err == nil {
		t.Fatal("expected an error when no PowerShell is present")
	}
	if !strings.Contains(err.Error(), "PowerShell") {
		t.Errorf("error does not name the missing prerequisite: %v", err)
	}
}

func TestPersistRuntimePathIsANoOpWithoutDirectories(t *testing.T) {
	runner := &pathRunner{found: map[string]string{"powershell": `C:\Windows\powershell.exe`}}
	changed, err := PersistRuntimePath(context.Background(), windowsRuntime(`C:\Users\u`, runner), securefs.New(securefs.Options{OS: "windows"}), nil)
	if err != nil || changed {
		t.Fatalf("PersistRuntimePath = %v, %v", changed, err)
	}
	if len(runner.calls) != 0 {
		t.Errorf("spawned PowerShell with nothing to record: %v", runner.calls)
	}
}
