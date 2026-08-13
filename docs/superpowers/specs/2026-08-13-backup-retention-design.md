# Backup Retention Design

## Goal

Keep configuration history bounded without mixing backup files with the active
configuration. Each backup target has its own retention count. The default is
three historical versions.

## Scope

The change covers Profile, Provider, MCP, Agent configuration files written by
the shared filesystem writer, and Skill registry/tree snapshots. Runtime and
update helper files are not user configuration and keep their existing behavior.

## Storage Layout

Backups live below `~/.bootagent/backup`:

```
backup/
  files/<target-key>/<timestamp>[...]
  skills/<skill-id>/<snapshot-directory>
```

`target-key` is a stable SHA-256 encoding of the absolute target path. A file
target therefore has an independent history even when other targets are saved
more often. Skill snapshots are grouped by Skill ID and retain their existing
metadata/content format so restore remains compatible.

The existing beside-file backups and `skill-backups` directory remain readable
during migration. A successful save prunes newly-created history and removes
recognized obsolete entries for that target after the new backup is safely
published. Invalid or unrelated files are left untouched.

## Write Flow

`securefs.AtomicWrite` creates the private backup root, copies the prior target
into that target's group, applies the existing secret permission hardening, and
atomically publishes the replacement. After publication it calls cleanup for
that target. Cleanup sorts recognized backups newest-first and removes entries
after the configured retention count. Cleanup failures are returned as a
retryable configuration-write error; the already-published target is not rolled
back.

Skill `CreateBackup` uses the same retention setting and its new per-Skill path.
It keeps the existing temporary-directory validation and atomic completion
semantics, then prunes only snapshots for the requested Skill ID.

## Settings

`Settings` gains `backup_retention`. The persisted value is validated and
clamped to the inclusive range 1..100; missing, malformed, or out-of-range
values resolve to the default of 3. Saving settings preserves the existing
mirror preference behavior and writes the retention value. The Settings page
exposes a numeric control and saves it through the existing Wails service.

## Compatibility and Security

Backup contents remain private (directory mode 0700, files mode 0600 where
secret). API keys never enter the settings DTO. Existing Skill restore IDs and
metadata remain valid. Status backup indicators recognize both the new managed
directory and legacy beside-file names while migration is in progress.

## Testing

- Secure filesystem tests verify the new directory, per-target isolation,
  default/configured retention, collision-safe timestamps, secret permissions,
  and legacy cleanup behavior.
- Skill store tests verify per-Skill retention and restore from the new layout.
- Settings tests verify default 3, persistence, validation, and migration from
  the old settings shape.
- Frontend tests verify the retention control loads, saves, and rejects values
  outside the backend-supported range.
