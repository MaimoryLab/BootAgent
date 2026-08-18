# Backup Retention Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task with verification checkpoints.

**Goal:** Store configuration history under `~/.bootagent/backup`, prune each target independently, and expose a persisted retention setting that defaults to three versions.

**Architecture:** Extend the existing `securefs.Store` policy with a configured backup root and a callback that reads the current retention setting. `AtomicWrite` owns ordinary-file backup creation and cleanup; `skill.Store` owns Skill directory snapshots but uses the same root and retention callback. The existing app-wide filesystem instance is configured once in `newUseCases`, so Profile, Provider, MCP, Agent adapters, and Skills share the policy without new abstractions.

**Tech Stack:** Go standard library (`crypto/sha256`, `encoding/json`, `os`, `filepath`, `sort`), existing Wails bindings, React/TypeScript, Vitest.

---

## Task 1: Add bounded ordinary-file backups

**Files:**

- Modify: `internal/securefs/securefs.go`
- Test: `internal/securefs/securefs_test.go`

- [ ] **Step 1: Write failing tests for location, isolation, and retention**

Add tests that construct `securefs.New(Options{OS: "linux", BackupRoot: root, Retention: func() int { return 3 }, Now: fixedClock})`, write an existing target repeatedly, and assert:

```go
group := BackupGroupPath(root, target)
entries, _ := os.ReadDir(group)
if len(entries) != 3 { t.Fatalf("backups = %d, want 3", len(entries)) }
if _, err := os.Stat(filepath.Join(filepath.Dir(target), "profile.json.backup-20260730123456")); !os.IsNotExist(err) { t.Fatal("backup remained beside target") }
```

Use a second target and assert its group still has its own three entries. Add a test with `Retention: func() int { return 1 }` and a test that a missing/zero callback resolves to the default three.

- [ ] **Step 2: Run the focused tests and verify the expected failure**

Run: `go test ./internal/securefs -run 'TestAtomicWrite|TestBackup'`

Expected: FAIL because `Options.BackupRoot`, `BackupGroupPath`, and the new location/cleanup behavior do not exist yet.

- [ ] **Step 3: Implement the smallest shared backup policy**

Add to `securefs.Options` a `BackupRoot string` and `Retention func() int`, copy them into `Store`, and default retention to 3. Add:

```go
func BackupGroupPath(root, target string) string {
    if root == "" { return "" }
    absolute, err := filepath.Abs(target)
    if err != nil { absolute = filepath.Clean(target) }
    sum := sha256.Sum256([]byte(filepath.Clean(absolute)))
    return filepath.Join(root, "files", hex.EncodeToString(sum[:]))
}
```

Change `Backup` to create a private target group below `BackupRoot/files`, copy the old file there with a UTC timestamp/collision suffix, and retain the legacy beside-file fallback only when no backup root is configured. Change `AtomicWrite` to call cleanup after the replacement is renamed. Cleanup must sort generated regular files newest-first, remove entries after the effective retention, and remove recognized legacy `target.backup-*` files when the managed root is enabled. Return existing retryable write errors for cleanup failures; do not undo a published replacement.

- [ ] **Step 4: Run focused tests and the package suite**

Run: `go test ./internal/securefs -run 'TestAtomicWrite|TestBackup' && go test ./internal/securefs`

Expected: PASS, with backups only under the managed root and secret backups still mode `0600`.

- [ ] **Step 5: Commit the filesystem change**

```bash
git add internal/securefs/securefs.go internal/securefs/securefs_test.go
git commit -m "feat: bound ordinary configuration backups"
```

### Task 2: Make retention a persisted application setting

**Files:**

- Modify: `internal/app/settings.go`
- Modify: `internal/app/status.go`
- Test: `internal/app/settings_test.go`
- Test: `internal/app/status_test.go` if backup-state assertions need coverage

- [ ] **Step 1: Add failing settings tests**

Add tests asserting a fresh `UseCases` returns `BackupRetention == 3`, an explicit value survives a new `UseCases`, a missing field in an old settings file resolves to 3, and values below 1 or above 100 resolve to the default when read. Add a save test that sends `Settings{PreferMirror: true}` with zero retention and verifies an existing configured retention is preserved for old clients.

- [ ] **Step 2: Run the focused settings tests and verify they fail**

Run: `go test ./internal/app -run 'TestSettings|TestBackupRetention'`

Expected: FAIL because `Settings.BackupRetention` and its persistence are absent.

- [ ] **Step 3: Implement validation and shared lookup**

Define `defaultBackupRetention = 3`, `minBackupRetention = 1`, and `maxBackupRetention = 100`. Add `BackupRetention int \`json:"backup_retention"\`` to `Settings` and a pointer field to `storedSettings`. Make`Settings` use the stored value only when it is in range; otherwise return 3. Make `SaveSettings` preserve the stored value when the request is zero (backward-compatible callers), clamp non-zero values to 1..100, and write both mirror and retention fields.

Configure `newUseCases` to construct its shared filesystem with:

```go
BackupRoot: filepath.Join(options.Home, ".bootagent", "backup"),
Retention: func() int { return backupRetentionFromFile(filepath.Join(options.Home, ".bootagent", "settings.json")) },
```

`backupRetentionFromFile` must return 3 for missing, malformed, or out-of-range files and must not log or expose secrets. Update `backupState` to report backups in the managed group as well as legacy beside-file matches.

- [ ] **Step 4: Run app tests**

Run: `go test ./internal/app -run 'TestSettings|TestBackupRetention|TestStatus'`

Expected: PASS, including existing mirror-region behavior.

- [ ] **Step 5: Commit settings and app wiring**

```bash
git add internal/app/settings.go internal/app/settings_test.go internal/app/status.go internal/app/status_test.go
git commit -m "feat: persist backup retention setting"
```

### Task 3: Move and prune Skill snapshots per Skill

**Files:**

- Modify: `internal/skill/store.go`
- Test: `internal/skill/store_test.go`

- [ ] **Step 1: Write failing per-Skill retention tests**

Set the injected filesystem retention callback to 3, create four snapshots for Skill `review` and one for Skill `other`, then assert `ListBackups` returns three `review` snapshots and one `other` snapshot. Assert the completed directories are below `filepath.Join(home, ".bootagent", "backup", "skills", "review")` and that `RestoreBackup` still restores the newest snapshot. Add a compatibility test that places a valid old snapshot below `.bootagent/skill-backups` and verifies it remains listable/restorable until pruning.

- [ ] **Step 2: Run the Skill tests and verify failure**

Run: `go test ./internal/skill -run 'TestCreateBackup|TestListBackups|TestRestoreBackup'`

Expected: FAIL because the store currently uses `.bootagent/skill-backups` and a global retention of 20.

- [ ] **Step 3: Implement the new Skill root and cleanup**

Derive the managed root from `s.fs.BackupRoot()` with a fallback to `home/.bootagent/backup` for direct stores. Change `BackupRoot` to `backup/skills`, create per-ID directories, and keep the existing pending-directory validation/atomic rename. Make `ListBackups`, `loadBackup`, `InspectBackup`, and `RestoreBackup` resolve both the new root and the legacy root. Replace the global `backupRetention` constant with `s.fs.BackupRetention()` and prune only entries whose metadata ID equals the requested Skill ID; remove oldest recognized snapshots across new and legacy roots after a successful creation.

- [ ] **Step 4: Run Skill package tests**

Run: `go test ./internal/skill`

Expected: PASS, including tamper, incomplete-backup, cancellation, and restore tests.

- [ ] **Step 5: Commit Skill changes**

```bash
git add internal/skill/store.go internal/skill/store_test.go
git commit -m "feat: retain Skill backups per target"
```

### Task 4: Expose the setting in the Wails frontend

**Files:**

- Modify: `frontend/src/pages/SettingsPage.tsx`
- Modify: `frontend/src/components/MirrorSetting.tsx`
- Modify: `frontend/src/i18n.tsx`
- Modify: `frontend/src/components/MirrorSetting.test.tsx`
- Modify: `frontend/src/components/RuntimeSection.test.tsx` and any other settings fixtures reported by TypeScript
- Regenerate: `frontend/bindings/**` with the repository Taskfile

- [ ] **Step 1: Add a failing UI test for load/save**

Extend the existing settings mock to return `backup_retention: 3`, render the Settings page, change the number input to `7`, blur it, and assert `api.saveSettings` receives `{schema_version: 1, prefer_mirror: false, mirror_from_region: false, backup_retention: 7}`. Add an out-of-range input assertion that the control clamps to the `1..100` HTML range and does not send an invalid value.

- [ ] **Step 2: Run the focused frontend test and verify failure**

Run: `cd frontend && pnpm run test -- src/pages/SettingsPage.test.tsx src/components/MirrorSetting.test.tsx`

Expected: FAIL because the retention control and DTO field are absent.

- [ ] **Step 3: Implement the minimal control and compatibility forwarding**

Add a Settings-page data row with `<input type="number" min={1} max={100} step={1}>`, initialize it from `getSettings`, clamp on blur, and save through the existing API while preserving the mirror fields. Update `MirrorSetting` to retain the loaded `backup_retention` when it toggles mirror, so the shared settings file is not reset by the existing download controls. Add Chinese source strings and English translations for the label, range hint, and save failure.

- [ ] **Step 4: Regenerate bindings and update typed fixtures**

Run: `task generate:bindings`, then update every TypeScript settings fixture to include `backup_retention`. Do not hand-edit generated files.

- [ ] **Step 5: Run frontend tests and build**

Run: `cd frontend && pnpm run test && pnpm run build`

Expected: PASS and a successful production TypeScript/Vite build.

- [ ] **Step 6: Commit frontend and generated bindings**

```bash
git add frontend/src frontend/bindings
git commit -m "feat: add backup retention setting"
```

### Task 5: Documentation and full verification

**Files:**

- Modify: `README.md`
- Modify: `README_ZH.md`
- Modify: `docs/ai-agent-kit/en/02-api-key.md` only if its backup-location wording is now inaccurate

- [ ] **Step 1: Update user-facing backup wording**

Document the default three historical versions, the per-target rule, the `.bootagent/backup` location, and the Settings control in both README languages. Keep commands and terminology aligned with the current repository.

- [ ] **Step 2: Run repository checks**

Run:

```bash
go test ./...
go test -race ./...
go vet ./...
cd frontend && pnpm run test && pnpm run build
cd .. && python3 scripts/check-docs.py
```

Expected: all commands exit 0; any generated-binding diff is reviewed before committing.

- [ ] **Step 3: Review the final diff and commit documentation**

Run `git diff --check` and `git status --short`, confirm no backup files or generated temporary files are tracked, then commit:

```bash
git add README.md README_ZH.md docs/ai-agent-kit/en/02-api-key.md
git commit -m "docs: describe bounded configuration backups"
```
