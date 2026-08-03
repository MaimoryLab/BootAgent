package process

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxLogEntryBytes bounds one stream inside one entry. Command output is
// already capped at MaxOutputBytes, but a megabyte per command would make the
// day's log unreadable.
const maxLogEntryBytes = 8 << 10

// LoggingRunner records every command it runs to a file under the OneAgent home
// and otherwise behaves exactly like the runner it wraps. It exists because a
// subprocess failure in the desktop build has no console to print to: without
// this, a command that fails between two Wails calls leaves nothing to read.
//
// Values are secret-scrubbed before they reach the file: argv and output are
// filtered through the sensitive values of the environment the command ran with,
// so an API key passed as an env var cannot land in the log.
type LoggingRunner struct {
	Inner Runner
	// Dir holds one file per day, named YYYY-MM-DD.log. An empty Dir disables
	// logging, which keeps the decorator safe to install before the home
	// directory is known.
	Dir string
}

// logPath is resolved per write rather than once per runner: the desktop process
// can outlive midnight, and a path fixed at construction would keep appending
// yesterday's file for the rest of the session.
func (l LoggingRunner) logPath() string {
	if l.Dir == "" {
		return ""
	}
	return filepath.Join(l.Dir, time.Now().Format("2006-01-02")+".log")
}

func (l LoggingRunner) LookPath(command string) (string, bool) {
	return l.Inner.LookPath(command)
}

func (l LoggingRunner) Run(ctx context.Context, argv []string, overrides map[string]string, timeout time.Duration) (Result, error) {
	return l.RunWithOutput(ctx, argv, overrides, timeout, nil)
}

func (l LoggingRunner) RunWithOutput(ctx context.Context, argv []string, overrides map[string]string, timeout time.Duration, listener OutputListener) (Result, error) {
	started := time.Now()
	var result Result
	var err error
	if streaming, ok := l.Inner.(StreamingRunner); ok {
		result, err = streaming.RunWithOutput(ctx, argv, overrides, timeout, listener)
	} else {
		result, err = l.Inner.Run(ctx, argv, overrides, timeout)
	}
	l.record("run", argv, overrides, time.Since(started), result, err)
	return result, err
}

// WithEnvironment keeps the decorator transparent to PATH injection: the
// managed runtime directories must reach the wrapped runner's own lookup, or a
// managed npm would be reported missing even though installs can run it.
func (l LoggingRunner) WithEnvironment(env map[string]string) Runner {
	if setter, ok := l.Inner.(EnvSetter); ok {
		l.Inner = setter.WithEnvironment(env)
	}
	return l
}

// AsLauncher returns the Launcher behind a runner, seeing through the logging
// decorator, and reports false when nothing in the chain can open a window. The
// unwrap matters: answering true for a runner that cannot launch would turn a
// "this build cannot open a terminal" answer into a generic failure.
func AsLauncher(runner Runner) (Launcher, bool) {
	if logger, ok := runner.(LoggingRunner); ok {
		inner, present := AsLauncher(logger.Inner)
		if !present {
			return nil, false
		}
		return launcherFunc(func(argv []string, overrides map[string]string) error {
			started := time.Now()
			err := inner.Start(argv, overrides)
			logger.record("launch", argv, overrides, time.Since(started), Result{ExitCode: 0}, err)
			return err
		}), true
	}
	launcher, ok := runner.(Launcher)
	return launcher, ok
}

type launcherFunc func(argv []string, overrides map[string]string) error

func (f launcherFunc) Start(argv []string, overrides map[string]string) error {
	return f(argv, overrides)
}

// EnvSetter is implemented by runners that resolve commands against their own
// environment rather than the process environment.
type EnvSetter interface {
	WithEnvironment(map[string]string) Runner
}

func (l LoggingRunner) record(kind string, argv []string, overrides map[string]string, elapsed time.Duration, result Result, err error) {
	if l.Dir == "" {
		return
	}
	secrets := sensitiveValues(overrides)
	if runner, ok := l.Inner.(OSRunner); ok {
		secrets = append(secrets, sensitiveValues(runner.Env)...)
	}
	entry := &strings.Builder{}
	fmt.Fprintf(entry, "%s %s %s", time.Now().UTC().Format(time.RFC3339), kind, scrub(strings.Join(argv, " "), secrets))
	if kind == "launch" {
		if err != nil {
			fmt.Fprintf(entry, "\n  error: %s", scrub(err.Error(), secrets))
		}
		entry.WriteByte('\n')
		l.append(entry.String())
		return
	}
	fmt.Fprintf(entry, "\n  exit %d in %s", result.ExitCode, elapsed.Round(time.Millisecond))
	if err != nil {
		fmt.Fprintf(entry, "\n  error: %s", scrub(err.Error(), secrets))
	}
	for _, stream := range []struct{ name, text string }{{"stdout", result.Stdout}, {"stderr", result.Stderr}} {
		text := strings.TrimSpace(stream.text)
		if text == "" {
			continue
		}
		fmt.Fprintf(entry, "\n  %s: %s", stream.name, indentLog(scrub(clampLog(text), secrets)))
	}
	entry.WriteByte('\n')
	l.append(entry.String())
}

// append writes one entry to today's file. A logging failure is deliberately
// swallowed: losing a diagnostic must never fail the operation being diagnosed.
func (l LoggingRunner) append(entry string) {
	path := l.logPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(l.Dir, 0o700); err != nil {
		return
	}
	// O_APPEND makes one Write per entry atomic enough that concurrent installs
	// interleave whole entries rather than lines, so no lock is needed here.
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(entry)
}

func clampLog(text string) string {
	if len(text) <= maxLogEntryBytes {
		return text
	}
	return text[:maxLogEntryBytes] + "\n[log truncated]"
}

func indentLog(text string) string {
	return strings.ReplaceAll(text, "\n", "\n    ")
}

func scrub(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	return text
}

// sensitiveValues lists the environment values that must never be written to
// the log. It keys off the name because the value of a credential is opaque.
func sensitiveValues(env map[string]string) []string {
	values := make([]string, 0, len(env))
	for name, value := range env {
		upper := strings.ToUpper(name)
		if value == "" {
			continue
		}
		if strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") ||
			strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") {
			values = append(values, value)
		}
	}
	return values
}
