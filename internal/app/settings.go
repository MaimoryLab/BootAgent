package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
}

// storedSettings is the on-disk shape. PreferMirror is a pointer so an absent
// field ("never chosen", which may take the regional default) is distinct from
// a stored false ("the user turned it off"). Without that distinction, a user in
// China who prefers the official source would find the box re-ticked on every
// launch.
type storedSettings struct {
	SchemaVersion   int   `json:"schema_version"`
	PreferMirror    *bool `json:"prefer_mirror"`
	BackupRetention *int  `json:"backup_retention"`
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
	if stored, ok := u.readStoredSettings(); ok {
		if stored.PreferMirror != nil {
			settings.PreferMirror = *stored.PreferMirror
		}
		if stored.BackupRetention != nil {
			settings.BackupRetention = storedBackupRetention(stored.BackupRetention)
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
	// Saving is always an explicit choice, so the key is written even for false.
	// That is what stops the regional default from re-ticking the box for a user
	// who turned it off.
	chosen := settings.PreferMirror
	retention := settings.BackupRetention
	data, err := json.MarshalIndent(storedSettings{
		SchemaVersion: settingsSchemaVersion, PreferMirror: &chosen, BackupRetention: &retention,
	}, "", "  ")
	if err != nil {
		return Settings{}, err
	}
	settings.MirrorFromRegion = false
	// Same coordinator as the other writers so a settings write cannot interleave
	// with an install that is about to read it.
	u.writeMu.Lock()
	defer u.writeMu.Unlock()
	if err := u.filesystem.EnsurePrivateDir(ctx, filepath.Dir(u.settingsPath())); err != nil {
		return Settings{}, err
	}
	if _, err := u.filesystem.AtomicWrite(ctx, u.settingsPath(), append(data, '\n'), false); err != nil {
		return Settings{}, err
	}
	return settings, nil
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
