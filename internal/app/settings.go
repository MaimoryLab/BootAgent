package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MaimoryLab/BootAgent/internal/platform"
)

// settingsSchemaVersion guards the on-disk shape. Only bump it for a change a
// previous build could not read.
const settingsSchemaVersion = 1

const (
	defaultBackupRetention = 3
	minBackupRetention     = 1
	maxBackupRetention     = 100
)

// regionProbeTimeout bounds the locale lookup. It is short on purpose: the
// answer only picks a default download host, so a slow or hung system call must
// never delay status.
const regionProbeTimeout = 3 * time.Second

// Settings holds non-secret machine-level preferences that must outlive one
// request and do not belong to a profile.
type Settings struct {
	SchemaVersion int `json:"schema_version"`
	// Autostart launches BootAgent when the user logs in.
	Autostart bool `json:"autostart"`
	// PreferMirror routes runtime archives through the mirror in
	// runtimes.lock.json and npm-managed Agents through the npmmirror registry.
	// Runtime archives keep their locked checksum verification; npm verifies
	// packages using the selected registry's metadata. uv is unaffected.
	PreferMirror bool `json:"prefer_mirror"`
	// MirrorFromRegion reports that PreferMirror above is a region-derived
	// default rather than the user's own choice. The UI uses it to explain why
	// the box is already ticked. It is never persisted: it is recomputed from the
	// machine each read, and storing it would let a stale answer outlive the
	// setting that produced it.
	MirrorFromRegion bool `json:"mirror_from_region"`
	// BackupRetention is the number of historical versions kept per target.
	BackupRetention int `json:"backup_retention"`
	// TerminalApp is the terminal used to launch CLI Agents. Empty means auto:
	// the platform's first installed terminal, which is what every build before
	// this setting did.
	TerminalApp string `json:"terminal_app"`
	// Terminals lists this platform's terminals and which are installed. It is
	// derived from the machine on each read and never persisted, so a terminal
	// installed or removed after the choice was stored is reflected immediately.
	Terminals []TerminalOption `json:"terminals"`
}

// SettingsPatch updates only the machine-level preferences named by the caller.
// A nil field means "leave the stored value unchanged".
type SettingsPatch struct {
	Autostart       *bool   `json:"autostart,omitempty"`
	PreferMirror    *bool   `json:"prefer_mirror,omitempty"`
	BackupRetention *int    `json:"backup_retention,omitempty"`
	TerminalApp     *string `json:"terminal_app,omitempty"`
}

// storedSettings is the on-disk shape. PreferMirror is a pointer so an absent
// field ("never chosen", which may take the regional default) is distinct from
// a stored false ("the user turned it off"). Without that distinction, a user in
// China who prefers the official source would find the box re-ticked on every
// launch.
type storedSettings struct {
	SchemaVersion   int     `json:"schema_version"`
	Autostart       *bool   `json:"autostart,omitempty"`
	PreferMirror    *bool   `json:"prefer_mirror,omitempty"`
	BackupRetention *int    `json:"backup_retention,omitempty"`
	TerminalApp     *string `json:"terminal_app,omitempty"`
}

func (u *UseCases) settingsPath() string {
	return filepath.Join(u.status.Home, ".bootagent", "settings.json")
}

// Settings reads the stored preferences, falling back to the regional default
// when the user has never chosen. A missing or unreadable file yields defaults
// rather than an error: a corrupt preference must not make the app unusable, and
// the next write repairs it.
func (u *UseCases) Settings(ctx context.Context) (Settings, error) {
	settings := Settings{SchemaVersion: settingsSchemaVersion, BackupRetention: defaultBackupRetention}
	if u == nil {
		return settings, nil
	}
	settings.Terminals = availableTerminals(u.status.Platform.OS, u.lookPath, u.pathExists)
	if stored, ok := u.readStoredSettings(); ok {
		if stored.Autostart != nil {
			settings.Autostart = *stored.Autostart
		}
		if stored.PreferMirror != nil {
			settings.PreferMirror = *stored.PreferMirror
		}
		if stored.BackupRetention != nil {
			settings.BackupRetention = storedBackupRetention(stored.BackupRetention)
		}
		if stored.TerminalApp != nil {
			settings.TerminalApp = normalizeTerminalApp(u.status.Platform.OS, *stored.TerminalApp)
		}
		if stored.PreferMirror != nil {
			return settings, nil
		}
		// A settings file written by an intermediate version can contain only the
		// retention field; still apply the regional mirror default below.
	}
	// Never chosen: a machine set to Chinese gets the mirror by default, because
	// the official hosts are consistently slow from there and a first-run user
	// has no way to know a faster option exists.
	if u.looksChinese(ctx) {
		settings.PreferMirror = true
		settings.MirrorFromRegion = true
	}
	return settings, nil
}

func normalizeBackupRetention(value int) int {
	if value < minBackupRetention {
		return minBackupRetention
	}
	if value > maxBackupRetention {
		return maxBackupRetention
	}
	return value
}

func storedBackupRetention(value *int) int {
	if value == nil || *value < minBackupRetention || *value > maxBackupRetention {
		return defaultBackupRetention
	}
	return *value
}

func (u *UseCases) readStoredSettings() (storedSettings, bool) {
	data, err := os.ReadFile(u.settingsPath())
	if err != nil {
		return storedSettings{}, false
	}
	var stored storedSettings
	if json.Unmarshal(data, &stored) != nil {
		return storedSettings{}, false
	}
	return stored, true
}

// backupRetentionFromFile is used by the filesystem writer without requiring
// a dependency from securefs back into app settings.
func backupRetentionFromFile(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultBackupRetention
	}
	var stored storedSettings
	if json.Unmarshal(data, &stored) != nil || stored.BackupRetention == nil {
		return defaultBackupRetention
	}
	return storedBackupRetention(stored.BackupRetention)
}

// SaveSettings persists the preferences and returns what was stored.
func (u *UseCases) SaveSettings(ctx context.Context, settings Settings) (Settings, error) {
	if u == nil {
		return Settings{}, nil
	}
	if err := contextError(ctx, "Settings request was cancelled"); err != nil {
		return Settings{}, err
	}
	settings.SchemaVersion = settingsSchemaVersion
	if settings.BackupRetention == 0 {
		settings.BackupRetention = backupRetentionFromFile(u.settingsPath())
	} else {
		settings.BackupRetention = normalizeBackupRetention(settings.BackupRetention)
	}
	chosen := settings.PreferMirror
	autostart := settings.Autostart
	retention := settings.BackupRetention
	terminal := normalizeTerminalApp(u.status.Platform.OS, settings.TerminalApp)
	return u.saveStoredSettings(ctx, storedSettings{
		SchemaVersion: settingsSchemaVersion, Autostart: &autostart, PreferMirror: &chosen, BackupRetention: &retention,
		TerminalApp: &terminal,
	})
}

// UpdateSettings merges a partial update with the stored preferences. It does
// not materialize a regional mirror default when that preference was untouched.
func (u *UseCases) UpdateSettings(ctx context.Context, patch SettingsPatch) (Settings, error) {
	if u == nil {
		return Settings{}, nil
	}
	if err := contextError(ctx, "Settings request was cancelled"); err != nil {
		return Settings{}, err
	}
	u.writeMu.Lock()
	stored, _ := u.readStoredSettings()
	stored.SchemaVersion = settingsSchemaVersion
	if patch.Autostart != nil {
		value := *patch.Autostart
		stored.Autostart = &value
	}
	if patch.PreferMirror != nil {
		value := *patch.PreferMirror
		stored.PreferMirror = &value
	}
	if patch.BackupRetention != nil && *patch.BackupRetention != 0 {
		value := normalizeBackupRetention(*patch.BackupRetention)
		stored.BackupRetention = &value
	}
	if patch.TerminalApp != nil {
		value := normalizeTerminalApp(u.status.Platform.OS, *patch.TerminalApp)
		stored.TerminalApp = &value
	}
	err := u.writeStoredSettings(ctx, stored)
	u.writeMu.Unlock()
	if err != nil {
		return Settings{}, err
	}
	return u.Settings(ctx)
}

func (u *UseCases) saveStoredSettings(ctx context.Context, stored storedSettings) (Settings, error) {
	u.writeMu.Lock()
	err := u.writeStoredSettings(ctx, stored)
	u.writeMu.Unlock()
	if err != nil {
		return Settings{}, err
	}
	return u.Settings(ctx)
}

func (u *UseCases) writeStoredSettings(ctx context.Context, stored storedSettings) error {
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	if err := u.filesystem.EnsurePrivateDir(ctx, filepath.Dir(u.settingsPath())); err != nil {
		return err
	}
	_, err = u.filesystem.AtomicWrite(ctx, u.settingsPath(), append(data, '\n'), false)
	return err
}

// normalizeTerminalApp keeps only an id this platform actually offers. An
// unknown id is stored as auto rather than rejected: the picker is the guard for
// a live choice, and refusing the whole save would block an unrelated setting
// because of a value the user cannot see.
func normalizeTerminalApp(osID, requested string) string {
	if requested == terminalAuto {
		return terminalAuto
	}
	for _, definition := range terminalsFor(osID) {
		if strings.EqualFold(definition.id, requested) {
			return definition.id
		}
	}
	return terminalAuto
}

// terminalApp reports the stored terminal choice for a launch. A failed read
// yields auto, because refusing to open a window over an unreadable preference
// would be worse than opening the default one.
func (u *UseCases) terminalApp(ctx context.Context) string {
	settings, err := u.Settings(ctx)
	if err != nil {
		return terminalAuto
	}
	return settings.TerminalApp
}

// lookPath resolves a command without requiring a runner, so reading settings on
// a build with no runner cannot panic.
func (u *UseCases) lookPath(command string) (string, bool) {
	if u == nil || u.runner == nil {
		return "", false
	}
	return u.runner.LookPath(command)
}

// pathExists reports whether a macOS application bundle is present.
func (u *UseCases) pathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// preferMirror reports the effective download preference. Reading it here keeps
// every runtime install honoring the setting, whether it was started from the
// runtime list or triggered by activating an Agent.
func (u *UseCases) preferMirror(ctx context.Context) bool {
	settings, err := u.Settings(ctx)
	if err != nil {
		return false
	}
	return settings.PreferMirror
}

// packageRegistry resolves the npm registry an install should use. An explicit
// per-request registry must not be silently overridden by a stored preference.
func (u *UseCases) packageRegistry(ctx context.Context, requested string) string {
	if requested != "" {
		return requested
	}
	if u.preferMirror(ctx) {
		return packageMirrorID
	}
	return ""
}

// packageMirrorID names the catalog mirror used when the preference is on.
const packageMirrorID = "npmmirror"

// looksChinese reports whether this machine's language or region points at
// mainland China. The answer cannot change without the user
// changing a system setting, so a successful answer is cached for the process
// lifetime: Settings is read on every status poll, which must not spawn a
// subprocess each time. A failed probe is not cached — a cancelled status poll
// would otherwise pin "not China" for the whole session and silently cost every
// later download its mirror.
func (u *UseCases) looksChinese(ctx context.Context) bool {
	u.regionMu.Lock()
	defer u.regionMu.Unlock()
	if u.regionKnown {
		return u.regionIsChinese
	}
	chinese, answered := u.detectChineseRegion(ctx)
	if answered {
		u.regionKnown = true
		u.regionIsChinese = chinese
	}
	return chinese
}

// detectChineseRegion reports the region and whether the machine actually
// answered. A probe that failed reports (false, false) so the caller can retry
// rather than remember it.
func (u *UseCases) detectChineseRegion(ctx context.Context) (bool, bool) {
	// The environment is free to read and, on Linux, authoritative.
	if platform.IsChineseLocale(platform.LocaleFromEnvironment(u.environment)) {
		return true, true
	}
	// The production Windows desktop reads this through Win32 before opening the
	// window. Avoiding a cold PowerShell process is what makes the first settings
	// read both fast and reliable.
	if platform.IsChineseLocale(u.status.SystemRegion) {
		return true, true
	}
	argv := platform.RegionCommand(u.status.Platform.OS)
	if len(argv) == 0 || u.runner == nil {
		// Nothing further to ask: on Linux the environment was the whole answer.
		return false, true
	}
	// The probe only picks a download host, so it must not inherit a caller's
	// cancellation: a status poll from a view the user navigated away from would
	// otherwise decide the mirror question for the rest of the session.
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), regionProbeTimeout)
	defer cancel()
	result, err := u.runner.Run(probeCtx, argv, nil, regionProbeTimeout)
	if err != nil || result.ExitCode != 0 {
		// A machine that will not answer is treated as "not China" for this call:
		// the official source is the safer default to guess wrong about, since it
		// is where the artifacts actually come from.
		return false, false
	}
	return platform.IsChineseLocale(result.Stdout), true
}
