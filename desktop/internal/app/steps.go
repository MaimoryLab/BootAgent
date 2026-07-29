package app

import (
	"github.com/MaimoryLab/OneAgent/desktop/internal/catalog"
	"github.com/MaimoryLab/OneAgent/desktop/internal/config"
	"github.com/MaimoryLab/OneAgent/desktop/internal/runtime"
)

// nextStep is the command that starts this Agent against what was just written.
//
// Derived from the manifest -- the command name and whether a credential arrives
// through an env file are both declared there. Spelling the commands out per Agent
// meant Claude Code's line said plain "claude" while its credential sat in a file
// nothing told the user to source.
func nextStep(rt *runtime.Runtime, agent catalog.Agent, agentID, model string) string {
	if agent.GuideOnly() || agent.Command == "" {
		return ""
	}
	// Each Agent sources its own file, so two Agents pointing at different
	// providers do not overwrite each other's credential in one shell.
	joiner := "&&"
	if rt.OSID == "windows" {
		joiner = ";"
	}

	// Aider's credential is the config the adapter writes, which is itself a shell
	// script, and the model is a launch argument rather than a field.
	if agent.ConfigAdapter == config.AdapterAider {
		source := "source ~/.oneagent/aider.env"
		if rt.OSID == "windows" {
			source = `. "$HOME\.oneagent\aider.ps1"`
		}
		return source + " " + joiner + " " + agent.Command + " --model openai/" + model
	}
	if config.NeedsEnvFile(agent) {
		source := "source ~/.oneagent/agents/" + agentID + ".env"
		if rt.OSID == "windows" {
			source = `. "$HOME\.oneagent\agents\` + agentID + `.env.ps1"`
		}
		return source + " " + joiner + " " + agent.Command
	}
	return agent.Command
}

// restartHint is how the user makes a rewritten config take effect.
//
// Agents read their config at startup, so a rewrite is invisible to an already
// running process. Saying "activated" without this is how a user concludes the
// switch silently failed.
func restartHint(agent catalog.Agent, agentID string) string {
	if agent.Command == "" {
		return "Restart " + agentID
	}
	if agent.ConfigAdapter == config.AdapterAider {
		return "Restart " + agent.Command + " in a shell that sources ~/.oneagent/aider.env"
	}
	if config.NeedsEnvFile(agent) {
		// Restarting alone is not enough when the credential lives in a file: the
		// new shell has to source it, or the Agent starts unauthenticated.
		return "Quit any running " + agent.Command +
			" process, then start it again in a shell that sources ~/.oneagent/agents/" + agentID + ".env"
	}
	return "Quit any running " + agent.Command + " process, then start it again"
}
