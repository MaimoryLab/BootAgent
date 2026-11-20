package app

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/catalog"
	oneerrors "github.com/MaimoryLab/BootAgent/internal/errors"
	"github.com/MaimoryLab/BootAgent/internal/install"
	"github.com/MaimoryLab/BootAgent/internal/process"
)

const (
	// A model writing one page usually finishes inside two minutes; the budget
	// leaves room for a slow Provider without letting a stuck run sit forever.
	firstTaskTimeout   = 5 * time.Minute
	firstTaskLogMax    = 8000
	firstTaskFilesMax  = 20
	firstTaskDirPrefix = "bootagent-first-task-"
)

type FirstTaskAgent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FirstTaskRequest struct {
	AgentID string `json:"agent_id"`
	Locale  string `json:"locale"`
}

// FirstTaskResult describes a run that started. OK is false when the Agent
// failed or wrote nothing, and Directory is empty then: an empty directory is
// removed rather than reported.
type FirstTaskResult struct {
	AgentID   string   `json:"agent_id"`
	OK        bool     `json:"ok"`
	Directory string   `json:"directory"`
	Files     []string `json:"files"`
	Log       string   `json:"log"`
}

// firstTaskAdapter is how one Agent runs unattended with writes confined to a
// directory. The prompt arrives on stdin, never on argv.
type firstTaskAdapter struct {
	ID      string
	Command string
	Argv    func(directory string) []string
}

var firstTaskAdapters = []firstTaskAdapter{
	{
		ID: "codex", Command: "codex",
		Argv: func(directory string) []string {
			return []string{
				"codex", "exec", "--sandbox", "workspace-write",
				// workspace-write has no network by default, but a user's own
				// config.toml can turn it on. The override wins over that file.
				"-c", "sandbox_workspace_write.network_access=false",
				"--skip-git-repo-check", "--ephemeral", "--ignore-rules", "--color", "never",
				"-C", directory, "-",
			}
		},
	},
	{
		ID: "claude-code", Command: "claude",
		// No working-directory flag exists, so the directory is where the
		// process starts. Write is the only tool: without Bash there is no way
		// to run a command or reach the network, and acceptEdits approves edits
		// under the working directory only.
		Argv: func(string) []string {
			return []string{
				"claude", "-p", "--safe-mode", "--tools", "Write", "--permission-mode", "acceptEdits",
				"--no-session-persistence", "--output-format", "text",
			}
		},
	},
}

func firstTaskAdapterFor(id string) (firstTaskAdapter, bool) {
	for _, adapter := range firstTaskAdapters {
		if adapter.ID == id {
			return adapter, true
		}
	}
	return firstTaskAdapter{}, false
}

// FirstTaskAgents lists the installed Agents RunFirstTask can drive.
func (u *UseCases) FirstTaskAgents(ctx context.Context) ([]FirstTaskAgent, error) {
	if u == nil || u.runner == nil {
		return nil, oneerrors.New(oneerrors.InternalError, "First task is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "First task request was cancelled"); err != nil {
		return nil, err
	}
	manifest, err := catalog.LoadEmbedded()
	if err != nil {
		return nil, err
	}
	runtime := u.installRuntime(nil)
	agents := make([]FirstTaskAgent, 0, len(firstTaskAdapters))
	for _, adapter := range firstTaskAdapters {
		if _, ok := runtime.Runner.LookPath(adapter.Command); !ok {
			continue
		}
		if entry, ok := manifest.Agents[adapter.ID]; ok {
			agents = append(agents, FirstTaskAgent{ID: adapter.ID, Name: entry.Name})
		}
	}
	return agents, nil
}

// RunFirstTask has an installed Agent write a small page in a directory of its
// own, using whatever model the Agent is already configured for. It sends real
// requests to that model, so it must only ever run from an explicit user action.
func (u *UseCases) RunFirstTask(ctx context.Context, request FirstTaskRequest) (FirstTaskResult, error) {
	if u == nil || u.runner == nil {
		return FirstTaskResult{}, oneerrors.New(oneerrors.InternalError, "First task is not configured", oneerrors.WithStatus(501))
	}
	if err := contextError(ctx, "First task request was cancelled"); err != nil {
		return FirstTaskResult{}, err
	}
	adapter, ok := firstTaskAdapterFor(strings.TrimSpace(request.AgentID))
	if !ok {
		return FirstTaskResult{}, oneerrors.New(oneerrors.InvalidRequest, "This Agent cannot run the first task")
	}
	runtime := u.installRuntime(nil)
	executable, present := runtime.Runner.LookPath(adapter.Command)
	if !present || executable == "" {
		return FirstTaskResult{}, oneerrors.New(oneerrors.PrerequisiteMissing, "The selected Agent is not installed")
	}
	directory, err := os.MkdirTemp("", firstTaskDirPrefix)
	if err != nil {
		return FirstTaskResult{}, oneerrors.New(oneerrors.InternalError, "Could not create a directory for the first task", oneerrors.WithCause(err))
	}
	argv := adapter.Argv(directory)
	// The path that made the Agent count as installed, for the reason
	// RecommendMarketplace gives: a bare name can resolve differently in the child.
	argv[0] = executable
	// The Agent may print nothing while the model works, so files appearing in
	// the directory count as progress too.
	runContext := process.WithActivityPaths(process.WithWorkingDirectory(ctx, directory), directory)
	run, runErr := process.RunPrivateInput(runContext, runtime.Runner, argv, runtime.Env, firstTaskTimeout, firstTaskPrompt(request.Locale))
	if ctx.Err() != nil {
		_ = os.RemoveAll(directory)
		return FirstTaskResult{}, contextError(ctx, "First task request was cancelled")
	}
	output := strings.TrimSpace(run.Stderr + "\n" + run.Stdout)
	if runErr != nil {
		output = strings.TrimSpace(output + "\n" + runErr.Error())
	}
	result := FirstTaskResult{
		AgentID: adapter.ID,
		Files:   firstTaskFiles(directory),
		Log:     tailRunes(install.Redact(output, u.firstTaskSecrets(runtime.Env)), firstTaskLogMax),
	}
	if len(result.Files) == 0 {
		_ = os.RemoveAll(directory)
		return result, nil
	}
	// Kept on purpose: the page is what the user came for. It lives under the
	// system temporary directory, which the OS clears on its own schedule.
	result.Directory = directory
	result.OK = runErr == nil && run.ExitCode == 0
	return result, nil
}

func firstTaskPrompt(locale string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "en") {
		return "Create one file named index.html in the current directory: a self-contained page, with its HTML and CSS in that one file, " +
			"that introduces what you can help with as an AI coding assistant. Create only that file. " +
			"Do not run any command, do not access the network, and do not read or change anything outside the current directory. " +
			"When you are done, say in one sentence what you did.\n"
	}
	return "在当前目录创建一个名为 index.html 的文件：一个自包含的页面（HTML 和 CSS 都写在这一个文件里），用中文介绍你作为 AI 编程助手能帮用户做哪些事。" +
		"只创建这一个文件。不要运行任何命令，不要访问网络，不要读取或修改当前目录以外的任何内容。" +
		"完成后用一句话说明你做了什么。\n"
}

// firstTaskFiles lists the regular files under directory, relative and with
// forward slashes. Links are skipped: one could point outside the directory.
func firstTaskFiles(directory string) []string {
	files := []string{}
	_ = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return nil
		}
		if relative, relErr := filepath.Rel(directory, path); relErr == nil {
			files = append(files, filepath.ToSlash(relative))
		}
		return nil
	})
	sort.Strings(files)
	if len(files) > firstTaskFilesMax {
		files = files[:firstTaskFilesMax]
	}
	return files
}

// firstTaskSecrets collects every key the Agent's output could repeat: the saved
// Provider keys, the local compatibility-mode key, and key-like variables.
func (u *UseCases) firstTaskSecrets(environment map[string]string) []string {
	secrets := []string{u.conversionAPIKey()}
	if providers, err := u.providers.Public(); err == nil {
		for id := range providers {
			if entry, getErr := u.providers.Get(id); getErr == nil {
				secrets = append(secrets, entry.APIKey)
			}
		}
	}
	for name, value := range environment {
		upper := strings.ToUpper(name)
		if strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") {
			secrets = append(secrets, value)
		}
	}
	return secrets
}

func tailRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[len(runes)-limit:])
}
