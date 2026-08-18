# Skills Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a local-only Skills Registry that discovers, imports, versions, projects, removes, backs up, and restores global Skills for Claude Code, Codex, OpenCode, and Hermes.

**Architecture:** `internal/skill` owns validated Skill trees, stable hashes, bounded folder/ZIP discovery, the private SSOT, Registry JSON, and backup primitives. `internal/app` uses catalog metadata and the existing `writeMu` to reconcile Agent roots and publish independent target projections. `internal/binding` exposes the app DTOs and native source dialogs, while React keeps drafts and conflict choices in page state and calls generated Wails bindings.

**Tech Stack:** Go 1.26 standard library (`archive/zip`, `crypto/sha256`, `io/fs`, `os`), existing `securefs` and `gopkg.in/yaml.v3`, Wails v3 generated TypeScript bindings, React, TypeScript, Vitest, and the existing Playwright E2E harness.

---

## File Map

Create the following focused files:

- `internal/skill/model.go` — Registry DTOs, ID/hash validation, metadata limits, and deterministic tree hashing.
- `internal/skill/discovery.go` — Agent-root scanning, recursive folder discovery, safe ZIP extraction, and bounded candidate snapshots.
- `internal/skill/store.go` — Private Registry/SSOT paths, atomic JSON persistence, variant publication, and backup/restore storage.
- `internal/skill/model_test.go`, `internal/skill/discovery_test.go`, `internal/skill/store_test.go` — red/green unit coverage for the security and persistence contracts.
- `internal/app/skill.go`, `internal/app/skill_test.go` — eligibility, scan, preview token lifecycle, Apply, target removal, uninstall, and restore orchestration.
- `internal/binding/skill.go`, `internal/binding/skill_dialog_wails.go`, `internal/binding/skill_dialog_other.go`, `internal/binding/skill_test.go` — narrow Wails service and injectable native selectors.
- `frontend/src/pages/SkillsPage.tsx`, `frontend/src/pages/SkillsPage.test.tsx` — registry table, imports, conflict resolution, target selection, backups, and Apply retry UI.

Modify only the existing integration files below unless a generated command updates another binding file:

- `internal/catalog/types.go`, `internal/catalog/manifest.go`, `internal/catalog/catalog_test.go`, `manifests/agents.lock.json` — catalog Skills contract.
- `internal/app/status.go`, `internal/app/mcp.go` — shared dirty-window state while preserving the MCP compatibility methods.
- `internal/binding/services.go`, `internal/binding/services_test.go`, `cmd/bootagent-desktop/main_wails.go` — service registration and close guard.
- `frontend/src/backend/wails.ts`, `frontend/src/backend/api.ts`, `frontend/src/types/api.ts`, `frontend/src/App.tsx`, `frontend/src/components/NavigationSidebar.tsx`, `frontend/src/i18n.tsx`, `frontend/src/styles/app.css` — bridge, route, navigation, translations, and layout.
- `README.md`, `README_ZH.md`, `docs/internal/skills-registry-acceptance.md` — user-visible behavior and maintainer acceptance notes.
- `frontend/bindings/**` — generated only by `task generate:bindings`; never hand edit.

Each task below is independently testable and ends with a small commit.

### Task 1: Add the catalog Skills contract

**Files:**

- Modify: `internal/catalog/types.go`
- Modify: `internal/catalog/manifest.go`
- Modify: `manifests/agents.lock.json`
- Test: `internal/catalog/catalog_test.go`

- [ ] **Step 1: Write the failing catalog contract test.**

Add this test beside `TestEmbeddedMCPMetadataMatchesRegistryContract`:

```go
func TestEmbeddedSkillsMetadataMatchesRegistryContract(t *testing.T) {
 manifest, err := LoadEmbedded()
 if err != nil { t.Fatal(err) }
 want := map[string][2]string{
  "claude-code": {".claude/skills", ""},
  "codex":       {".codex/skills", ""},
  "opencode":    {".config/opencode/skills", ""},
  "hermes":      {".hermes/skills", "AppData/Local/hermes/skills"},
 }
 for id, paths := range want {
  agent, ok := manifest.Agents[id]
  if !ok { t.Fatalf("missing Skills Agent %q", id) }
  if agent.SkillsPath != paths[0] || agent.SkillsWindowsPath != paths[1] {
   t.Errorf("%s Skills paths = %q/%q", id, agent.SkillsPath, agent.SkillsWindowsPath)
  }
 }
 for _, id := range []string{"aider", "openclaw", "kilo-cli"} {
  if agent := manifest.Agents[id]; agent.SkillsPath != "" || agent.SkillsWindowsPath != "" {
   t.Errorf("%s unexpectedly has Skills metadata", id)
  }
 }
}
```

- [ ] **Step 2: Run the focused test and verify it fails.**

Run: `go test ./internal/catalog -run SkillsMetadata -count=1`

Expected: compilation fails because `catalog.Agent` has no `SkillsPath` fields.

- [ ] **Step 3: Add fields and validation.**

Add to `catalog.Agent`:

```go
SkillsPath        string `json:"skills_path,omitempty"`
SkillsWindowsPath string `json:"skills_windows_path,omitempty"`
```

Call `validateSkillsMetadata(id, agent)` from `validate` after MCP validation. The helper accepts an empty pair, otherwise requires a non-empty `skills_path`, rejects absolute paths, cleaned-path changes, `..` prefixes, drive-qualified values, and a non-empty `skills_windows_path` on non-Windows-only entries. Reuse the exact relative-path predicate already used by `validateMCPMetadata` so catalog paths cannot escape the user home. Add the four JSON fields to the four entries in `manifests/agents.lock.json` and leave every other entry absent.

- [ ] **Step 4: Run the focused test and the catalog suite.**

Run: `gofmt -w internal/catalog/types.go internal/catalog/manifest.go internal/catalog/catalog_test.go && go test ./internal/catalog -count=1`

Expected: PASS, including `TestEmbeddedSkillsMetadataMatchesRegistryContract` and the existing MCP contract tests.

- [ ] **Step 5: Commit the catalog boundary.**

```bash
git add internal/catalog/types.go internal/catalog/manifest.go internal/catalog/catalog_test.go manifests/agents.lock.json
git commit -m "feat: add catalog Skills paths"
```

### Task 2: Implement Skill identity, metadata, and deterministic hashing

**Files:**

- Create: `internal/skill/model.go`
- Test: `internal/skill/model_test.go`

- [ ] **Step 1: Write failing model tests.**

Create tests for the exact contracts below:

```go
func TestValidateIDRejectsPathAndReservedNames(t *testing.T) {
 for _, id := range []string{"", ".", "..", "../x", `a\\b`, "/tmp/x", "CON", "a\x00b", strings.Repeat("a", 129)} {
  if ValidateID(id) == nil { t.Errorf("ValidateID(%q) accepted", id) }
 }
}

func TestHashTreeIsStableAcrossCreationOrder(t *testing.T) {
 first, second := t.TempDir(), t.TempDir()
 writeSkill(t, first, map[string]string{"SKILL.md": "---\nname: Review\ndescription: Check code\n---\nbody\n", "z.txt": "z", "nested/a.txt": "a"})
 writeSkill(t, second, map[string]string{"nested/a.txt": "a", "z.txt": "z", "SKILL.md": "---\nname: Review\ndescription: Check code\n---\nbody\n"})
 a, err := HashTree(context.Background(), first); if err != nil { t.Fatal(err) }
 b, err := HashTree(context.Background(), second); if err != nil { t.Fatal(err) }
 if a.Hash != b.Hash || a.Files != 3 { t.Fatalf("hash/stats = %#v %#v", a, b) }
}

func TestHashTreeRejectsSymlink(t *testing.T) {
 root := t.TempDir(); writeSkill(t, root, map[string]string{"SKILL.md": "body"})
 if err := os.Symlink(filepath.Join(root, "SKILL.md"), filepath.Join(root, "escape")); err != nil { t.Fatal(err) }
 if _, err := HashTree(context.Background(), root); err == nil { t.Fatal("symlink accepted") }
}
```

`writeSkill` is a test helper that creates parent directories and writes mode `0600` files.

- [ ] **Step 2: Run the tests to verify the package is absent/failing.**

Run: `go test ./internal/skill -run 'ValidateID|HashTree' -count=1`

Expected: FAIL because the new package and symbols do not exist.

- [ ] **Step 3: Add the minimal model implementation.**

Define these exported types and functions in `model.go`:

```go
const RegistrySchemaVersion = 1
const MaxSkillIDLength = 128

type Registry struct { SchemaVersion int `json:"schema_version"`; Skills map[string]Fact `json:"skills"` }
type Fact struct { Name string `json:"name"`; Description string `json:"description"`; Variants []Variant `json:"variants"` }
type Variant struct { Hash string `json:"hash"`; Stored bool `json:"stored"`; ObservedAgents []string `json:"observed_agents"`; ImportSources []string `json:"import_sources"`; ManagedTargets []string `json:"managed_targets"` }
type TreeStats struct { Hash string `json:"hash"`; Files int `json:"files"`; Bytes int64 `json:"bytes"` }
type Candidate struct { ID, Name, Description, Hash, Source string; Files int; Bytes int64; Diagnostic string; Path string }

func ValidateID(id string) error
func HashTree(ctx context.Context, root string) (TreeStats, error)
func ReadMetadata(ctx context.Context, root, fallbackID string) (name, description, diagnostic string)
```

`HashTree` walks with `os.Lstat`, sorts slash-normalized relative paths, hashes a type byte, path length/path, file length, and file bytes, and rejects links or non-regular entries before opening them. It checks `ctx` on every entry and caps files at 10,000 and bytes at 512 MiB. `ReadMetadata` reads at most 64 KiB of `SKILL.md`, parses bounded YAML front matter with the existing yaml.v3 dependency, trims `name`/`description` to 256/1024 bytes, and returns fallback ID plus a redacted diagnostic for malformed or absent metadata.

- [ ] **Step 4: Run the model tests and race the package.**

Run: `gofmt -w internal/skill/model.go internal/skill/model_test.go && go test ./internal/skill -run 'ValidateID|HashTree' -count=1`

Expected: PASS with symlinks, special files, and over-limit trees rejected.

- [ ] **Step 5: Commit the model.**

```bash
git add internal/skill/model.go internal/skill/model_test.go
git commit -m "feat: add Skill identity and tree hashing"
```

### Task 3: Add bounded folder/ZIP discovery and directory publication primitives

**Files:**

- Create: `internal/skill/discovery.go`
- Test: `internal/skill/discovery_test.go`

- [ ] **Step 1: Write failing discovery tests.**

Cover one direct folder candidate, recursive nested candidates, ZIP traversal, duplicate normalized entries, and symlink rejection:

```go
func TestDiscoverFolderFindsNestedSkillDirectories(t *testing.T) {
 root := t.TempDir(); writeSkill(t, filepath.Join(root, "pack", "review"), map[string]string{"SKILL.md": "body"}); writeSkill(t, filepath.Join(root, "pack", "lint"), map[string]string{"SKILL.md": "body2"})
 got, err := DiscoverFolder(context.Background(), root)
 if err != nil || len(got) != 2 { t.Fatalf("candidates = %#v, err=%v", got, err) }
}

func TestExtractZIPRejectsTraversalAndDuplicate(t *testing.T) {
 for _, names := range [][]string{{"../SKILL.md"}, {"a/SKILL.md", "a/./SKILL.md"}} {
  zipPath := makeZip(t, names)
  if _, err := DiscoverZIP(context.Background(), zipPath, t.TempDir()); err == nil { t.Fatalf("accepted %v", names) }
 }
}
```

- [ ] **Step 2: Run the focused tests and verify failure.**

Run: `go test ./internal/skill -run 'Discover|ExtractZIP' -count=1`

Expected: FAIL because discovery functions are undefined.

- [ ] **Step 3: Implement safe discovery and publication.**

Define:

```go
func ScanAgentRoot(ctx context.Context, root, source string) ([]Candidate, error)
func DiscoverFolder(ctx context.Context, root string) ([]Candidate, error)
func DiscoverZIP(ctx context.Context, zipPath, stagingParent string) ([]Candidate, error)
func CopyTree(ctx context.Context, source, destination string) error
func PublishTree(ctx context.Context, source, destination string) error
```

`ScanAgentRoot` examines immediate child directories only. Folder discovery treats the selected directory as one candidate when it contains `SKILL.md`; otherwise it recursively examines directories and reports each invalid candidate independently. ZIP extraction enforces 128 MiB compressed input, 10,000 entries, 512 MiB expanded bytes, normalized slash paths, no absolute/`..`/duplicate names, and no symlink or special entries. All extraction and copy destinations are created under a caller-owned staging directory, and every destination path is checked with `filepath.Rel` before writing. `CopyTree` uses `Lstat` and never follows links. `PublishTree` stages beside the destination, renames an existing destination to a random `.bootagent-rollback-*` sibling, renames the staged directory into place, and restores the rollback sibling if publication fails.

- [ ] **Step 4: Add tests for copy preservation and failed rename rollback, then run them.**

Use an injected `rename` function in `discovery.go` only for tests; the production function is `os.Rename`. Assert unrelated siblings remain and a failed second rename restores the original content.

Run: `gofmt -w internal/skill/discovery.go internal/skill/discovery_test.go && go test ./internal/skill -run 'Discover|ExtractZIP|Publish|Copy' -count=1`

Expected: PASS.

- [ ] **Step 5: Commit discovery.**

```bash
git add internal/skill/discovery.go internal/skill/discovery_test.go
git commit -m "feat: safely discover and publish Skill trees"
```

### Task 4: Persist the private Registry and backup/restore data

**Files:**

- Create: `internal/skill/store.go`
- Test: `internal/skill/store_test.go`

- [ ] **Step 1: Write failing store tests.**

Assert absent-as-empty, malformed/new schema rejection, atomic save mode, deterministic variant paths, backup-before-delete, and newest-20 retention:

```go
func TestStoreAbsentRegistryIsEmpty(t *testing.T) {
 s := NewStore(t.TempDir(), securefs.New(securefs.Options{OS: "linux"}))
 r, err := s.Load(); if err != nil || r.SchemaVersion != RegistrySchemaVersion || r.Skills == nil { t.Fatalf("registry = %#v, err=%v", r, err) }
}

func TestVariantPathUsesComputedHash(t *testing.T) {
 s := NewStore("/home/user", securefs.Store{})
 if got := s.VariantPath("review", strings.Repeat("a", 64)); got != "/home/user/.bootagent/skills/review/variants/"+strings.Repeat("a", 64) { t.Fatal(got) }
}
```

- [ ] **Step 2: Run and observe failure.**

Run: `go test ./internal/skill -run 'Store|VariantPath' -count=1`

Expected: FAIL because `NewStore` and persistence methods are missing.

- [ ] **Step 3: Implement Store with existing securefs primitives.**

Define:

```go
type Store struct { home string; fs securefs.Store }
func NewStore(home string, fs securefs.Store) Store
func (s Store) RegistryPath() string
func (s Store) SkillsRoot() string
func (s Store) BackupRoot() string
func (s Store) VariantPath(id, hash string) string
func (s Store) Load() (Registry, error)
func (s Store) Save(ctx context.Context, registry Registry) error
func (s Store) SaveVariant(ctx context.Context, id, source string, stats TreeStats) error
func (s Store) RemoveSkill(ctx context.Context, id string) error
func (s Store) CreateBackup(ctx context.Context, id string, facts Fact) (BackupSummary, error)
func (s Store) ListBackups() ([]BackupSummary, error)
func (s Store) RestoreBackup(ctx context.Context, backupID string) (Fact, error)
```

`Load` returns schema 1 with an empty map for a missing file and rejects malformed/new schemas without writing. `Save` validates IDs, hashes, sorted/deduplicated association lists, JSON-encodes with indentation, and uses `securefs.AtomicWrite(..., true)`. `SaveVariant` validates and publishes a complete tree to the computed `skills/<id>/variants/<hash>` path; it never accepts a caller path as the destination. `CreateBackup` copies every variant under a timestamped private directory, writes `metadata.json` through `AtomicWrite`, deletes the incomplete directory on any failure, and then prunes only entries older than the newest 20. Restore validates metadata, re-hashes restored trees, and returns a fact for the app to project to explicitly selected targets.

- [ ] **Step 4: Run store tests and inspect permissions.**

Run: `gofmt -w internal/skill/store.go internal/skill/store_test.go && go test ./internal/skill -run 'Store|VariantPath|Backup|Restore' -count=1`

Expected: PASS; registry and backup files are mode `0600`, private directories `0700`, and incomplete backups are absent.

- [ ] **Step 5: Commit persistence.**

```bash
git add internal/skill/store.go internal/skill/store_test.go
git commit -m "feat: persist Skill registry and backups"
```

### Task 5: Add the app-level scan, preview, Apply, uninstall, and restore use cases

**Files:**

- Modify: `internal/app/status.go`
- Modify: `internal/app/mcp.go`
- Create: `internal/app/skill.go`
- Test: `internal/app/skill_test.go`

- [ ] **Step 1: Add failing use-case tests and fixtures.**

Create a fake home with `.claude/skills/review/SKILL.md`, a fake lookup that reports `claude`, `codex`, `opencode`, and `hermes`, and tests for:

```go
func TestScanSkillDoesNotCreateUninitializedRoots(t *testing.T)
func TestScanSkillReportsUnmanagedCandidateWithoutStoringIt(t *testing.T)
func TestApplySkillImportsAndProjectsOnlySelectedTarget(t *testing.T)
func TestApplySkillKeepsSuccessfulAgentWhenAnotherTargetFails(t *testing.T)
func TestRemoveSkillTargetRequiresMatchingManagedHash(t *testing.T)
func TestUninstallCreatesBackupBeforeRemovingSSOT(t *testing.T)
func TestSkillDraftSharesCloseGuardWithMCP(t *testing.T)
```

The first test asserts `os.IsNotExist(~/.codex/skills)` after `ScanSkills`; the second asserts the scan candidate has `Stored == false` and `.bootagent/skills` is absent; Apply assertions check unrelated children remain and target result rows identify success/failure independently.

- [ ] **Step 2: Run the tests to verify the app API is missing.**

Run: `go test ./internal/app -run 'Skill|DraftShares' -count=1`

Expected: FAIL because the Skill use-case methods and types do not exist.

- [ ] **Step 3: Add the shared dirty state without breaking MCP callers.**

Keep `SetMCPDraftState` and `MCPDraftState`, add `skillDraftState`, `SetSkillDraftState`, and:

```go
func (u *UseCases) DraftState() (dirty bool, locale string) {
 if u == nil { return false, "zh" }
 mcpDirty, mcpLocale := u.MCPDraftState()
 skillDirty, skillLocale := u.SkillDraftState()
 locale = mcpLocale
 if skillLocale != "" { locale = skillLocale }
 return mcpDirty || skillDirty, locale
}
```

`SetSkillDraftState` mirrors the existing locale default and atomics. Add `SkillDraftState`; modify the native close hook later to call `DraftState` and clear both setters only after confirmation. Existing MCP tests remain unchanged.

- [ ] **Step 4: Implement catalog-driven eligibility and scan.**

In `skill.go`, define these DTOs and methods:

```go
type SkillSummary struct { ID, Name, Description string; Agents []string; Variants int; Conflict bool; Candidates []SkillCandidate }
type SkillCandidate struct { ID, Name, Description, Hash, Source string; Files int; Bytes int64; Stored bool; Diagnostic string }
type SkillScanResult struct { Skills []SkillSummary; EligibleAgents []string; Candidates []SkillCandidate; Diagnostics []string }
type SkillImportRequest struct { Kind string }
type SkillImportPreview struct { Token string; Candidates []SkillCandidate; ExpiresAt string }
type SkillChange struct { ID, VariantHash, SourceToken, CandidateHash string; Targets []string; Delete bool; ImportSource string }
type SkillApplyRequest struct { Changes []SkillChange }
type SkillAgentApplyResult struct { Agent, ID string; ContentUpdated, RegistryUpdated bool; Code, Message string }
type SkillApplyResult struct { Results []SkillAgentApplyResult }
type SkillUninstallResult struct { ID string; BackupID string; Results []SkillAgentApplyResult; Removed bool }
type SkillBackupSummary struct { ID, BackupID, CreatedAt string; Variants int }

func (u *UseCases) ListSkills(ctx context.Context) ([]SkillSummary, error)
func (u *UseCases) ScanSkills(ctx context.Context) (SkillScanResult, error)
func (u *UseCases) GetSkill(ctx context.Context, id string) (SkillSummary, error)
func (u *UseCases) PreviewSkillImport(ctx context.Context, request SkillImportRequest, selectedPath string) (SkillImportPreview, error)
func (u *UseCases) ApplySkills(ctx context.Context, request SkillApplyRequest) SkillApplyResult
func (u *UseCases) UninstallSkill(ctx context.Context, id string) SkillUninstallResult
func (u *UseCases) ListSkillBackups(ctx context.Context) ([]SkillBackupSummary, error)
func (u *UseCases) RestoreSkillBackup(ctx context.Context, backupID string, targets []string) SkillApplyResult
```

`eligibleSkillAgents` loads the embedded catalog, requires a non-empty Skills path, an allowed platform, `Lookup(agent.Command)` success, and an existing directory; it never calls `MkdirAll`. `ScanSkills` holds `writeMu`, clears only factual associations for readable eligible roots, hashes immediate child candidates, preserves old facts on read errors, and returns unstored observations as candidates. `PreviewSkillImport` validates kind (`agent`, `folder`, `zip`), uses `ScanAgentRoot`, `DiscoverFolder`, or `DiscoverZIP`, stores an opaque random token in a process-memory map with a five-minute expiry, and writes nothing. Use `context` checks between candidates.

- [ ] **Step 5: Implement Apply and target deletion with revalidation.**

`ApplySkills` holds `writeMu`, validates every ID/hash/target against the current catalog and Registry, resolves a token candidate and re-hashes it, publishes an unstored candidate to SSOT before touching any Agent, then calls `PublishTree` per target. Before `Delete`, it hashes the target directory and removes it only when the Registry’s `ManagedTargets` includes that Agent and the current hash equals `VariantHash`. Persist the Registry after each successful Agent so partial results survive a later failure. Return stable codes (`invalid_input`, `source_changed`, `conflict`, `permission`, `filesystem_retryable`) and no paths or file contents.

- [ ] **Step 6: Implement uninstall, backup listing, and restore.**

`UninstallSkill` first calls `CreateBackup`, aborting before deletion if it fails; it then revalidates/deletes managed projections, removes SSOT only after all requested deletes succeed, and removes the Registry entry only when every operation succeeded. `RestoreSkillBackup` validates metadata, publishes each variant to SSOT, and projects only the explicitly supplied eligible targets. Keep failed targets and Registry facts for retry.

- [ ] **Step 7: Run app tests, race the package, and commit.**

Run: `gofmt -w internal/app/status.go internal/app/mcp.go internal/app/skill.go internal/app/skill_test.go && go test ./internal/app -run 'Skill|DraftShares' -count=1 && go test -race ./internal/app -run 'Skill|DraftShares' -count=1`

Expected: PASS, with no Agent root created by scan and no deletion after external content changes.

```bash
git add internal/app/status.go internal/app/mcp.go internal/app/skill.go internal/app/skill_test.go
git commit -m "feat: add Skills Registry use cases"
```

### Task 6: Expose the service through Wails and update the close guard

**Files:**

- Create: `internal/binding/skill.go`
- Create: `internal/binding/skill_dialog_wails.go`
- Create: `internal/binding/skill_dialog_other.go`
- Modify: `internal/binding/services.go`
- Modify: `internal/binding/services_test.go`
- Modify: `cmd/bootagent-desktop/main_wails.go`
- Test: `internal/binding/skill_test.go`

- [ ] **Step 1: Write failing service allowlist and cancellation tests.**

Add `SkillService` to `TestServiceMethodAllowlist` with exactly `Apply`, `Get`, `List`, `ListBackups`, `PreviewImport`, `RestoreBackup`, `Scan`, `SetDraftState`, and `Uninstall`. Add a canceled-context test matching the MCP service pattern.

- [ ] **Step 2: Run binding tests and verify failure.**

Run: `go test ./internal/binding -run 'Skill|ServiceMethodAllowlist' -count=1`

Expected: FAIL because `Services.Skill` and `SkillService` are absent.

- [ ] **Step 3: Implement the narrow service and injected dialogs.**

Add `Skill *SkillService` to `binding.Services`, initialize it in `NewServicesWithOptions`, and implement methods that only check context/readiness then delegate to `UseCases`. `SkillImportRequest` contains only `kind`; the Wails service selects a directory or `.zip` through injectable callbacks and passes the path to the app use case, so source paths never enter React state. The non-Wails selector returns the same stable “desktop file dialog is unavailable” error used by transfer tests. Folder selection uses `OpenFile().CanChooseDirectories(true).CanChooseFiles(false)`; ZIP selection uses a single `*.zip` filter.

- [ ] **Step 4: Register the service and combine dirty state in the native close hook.**

Register `services.Skill` in `main_wails.go`. Replace the close hook’s `MCPDraftState` call with `DraftState`; use a generic message (“MCP 或 Skills 草稿尚未应用” / “MCP or Skills changes are not applied”), and on confirmation call both `SetMCPDraftState(false, locale)` and `SetSkillDraftState(false, locale)` before closing.

- [ ] **Step 5: Run binding tests and commit.**

Run: `gofmt -w internal/binding/skill*.go internal/binding/services.go internal/binding/services_test.go cmd/bootagent-desktop/main_wails.go && go test ./internal/binding -run 'Skill|ServiceMethodAllowlist' -count=1`

Expected: PASS; reflection confirms no extra exported service methods.

```bash
git add internal/binding/skill*.go internal/binding/services.go internal/binding/services_test.go cmd/bootagent-desktop/main_wails.go
git commit -m "feat: expose Skills Registry through Wails"
```

### Task 7: Regenerate bindings and add the typed frontend bridge

**Files:**

- Modify: `frontend/src/backend/wails.ts`
- Modify: `frontend/src/backend/api.ts`
- Modify: `frontend/src/types/api.ts`
- Generate: `frontend/bindings/**`
- Test: `frontend/src/backend/wails.test.ts`

- [ ] **Step 1: Add failing bridge tests.**

Mock the generated `SkillService` module and assert `wailsApi.listSkills`, `scanSkills`, `previewSkillImport`, `applySkills`, `uninstallSkill`, `listSkillBackups`, `restoreSkillBackup`, and `setSkillDraftState` forward exact snake_case DTOs and normalize empty lists to `[]`.

- [ ] **Step 2: Run the focused frontend test and verify failure.**

Run: `cd frontend && pnpm test -- --run src/backend/wails.test.ts`

Expected: FAIL because generated Skill bindings and bridge methods are absent.

- [ ] **Step 3: Regenerate Wails bindings from Go.**

Run from the repository root:

```bash
task generate:bindings
```

Expected: generated `skillservice.ts` and app model types appear under `frontend/bindings`; no generated file is hand edited.

- [ ] **Step 4: Add aliases and bridge methods.**

Import the generated `skillservice.js` and add aliases for all `Skill*` app DTOs in `frontend/src/types/api.ts`. Add bridge methods with the same shape as:

```ts
listSkills: (): Promise<SkillSummary[]> => call(() => SkillService.List()).then((items) => items ?? []),
scanSkills: (): Promise<SkillScanResult> => call(() => SkillService.Scan()) as Promise<SkillScanResult>,
previewSkillImport: (kind: string): Promise<SkillImportPreview> => call(() => SkillService.PreviewImport({ kind })) as Promise<SkillImportPreview>,
applySkills: (request: SkillApplyRequest): Promise<SkillApplyResult> => call(() => SkillService.Apply(request)) as Promise<SkillApplyResult>,
uninstallSkill: (id: string): Promise<SkillUninstallResult> => call(() => SkillService.Uninstall({ id })) as Promise<SkillUninstallResult>,
listSkillBackups: (): Promise<SkillBackupSummary[]> => call(() => SkillService.ListBackups()).then((items) => items ?? []),
restoreSkillBackup: (backupID: string, targets: string[]): Promise<SkillApplyResult> => call(() => SkillService.RestoreBackup({ backup_id: backupID, targets })) as Promise<SkillApplyResult>,
setSkillDraftState: (dirty: boolean, locale: string): Promise<void> => call(() => SkillService.SetDraftState(dirty, locale)).then(() => undefined),
```

Extend `BackendApi` only through the existing `typeof wailsApi` export; do not create a second client.

- [ ] **Step 5: Run typecheck and bridge tests, then commit.**

Run: `cd frontend && pnpm test -- --run src/backend/wails.test.ts && pnpm exec tsc --noEmit`

Expected: PASS with generated method signatures and no `any` casts beyond existing bridge casts.

```bash
git add frontend/bindings frontend/src/backend/wails.ts frontend/src/backend/api.ts frontend/src/types/api.ts frontend/src/backend/wails.test.ts
git commit -m "feat: add typed Skills Wails bridge"
```

### Task 8: Build the Skills page, route, navigation, and localized UI

**Files:**

- Create: `frontend/src/pages/SkillsPage.tsx`
- Create: `frontend/src/pages/SkillsPage.test.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/components/NavigationSidebar.tsx`
- Modify: `frontend/src/i18n.tsx`
- Modify: `frontend/src/styles/app.css`

- [ ] **Step 1: Write failing page behavior tests.**

Mock `api` and assert the page:

```tsx
it("renders scan candidates without writing until Apply", async () => { /* list + scan, click Import folder, select candidate, expect Apply enabled */ });
it("keeps failed targets pending after a partial Apply", async () => { /* result has one success and one failure; expect retry row */ });
it("requires an explicit variant for a conflict", async () => { /* conflict row has no implicit target and Apply stays disabled */ });
```

Use the existing `I18nProvider` and React Testing Library conventions from `MCPPage.test.tsx`; do not test generated bindings in this page suite.

- [ ] **Step 2: Run the focused page tests and verify failure.**

Run: `cd frontend && pnpm test -- --run src/pages/SkillsPage.test.tsx`

Expected: FAIL because the page and route do not exist.

- [ ] **Step 3: Implement the smallest complete page workflow.**

`SkillsPage` loads `listSkills` then starts `scanSkills` on mount, keeps a local draft map of `{ variant_hash, targets, delete, import_source, source_token }`, and calls `setSkillDraftState` whenever the map is non-empty. Render a dense table with search, status/conflict badges, eligible-target checkboxes, Refresh, Import folder, Import ZIP, Backups, and Apply. Import buttons call `previewSkillImport("folder"|"zip")`; the candidate dialog lists ID/name/hash/size and requires checkbox selection before adding draft entries. Conflict rows render a radio selection for each hash and never choose a winner automatically. Apply sends all touched changes, clears only rows whose `registry_updated` result is true, and refreshes facts after every response. Uninstall opens the existing question dialog and calls `uninstallSkill`; backups show `listSkillBackups` and call `restoreSkillBackup` with explicitly checked targets.

- [ ] **Step 4: Add route, sidebar item, translations, and responsive styles.**

Add `<Route path="/skills" element={<SkillsPage />} />`, a `WandSparkles` sidebar item labeled `Skills`, and Chinese source keys with English values for every new visible string. Add `.skills-page`, `.skills-table`, `.skills-import-dialog`, and compact/mobile rules to `app.css`; use lucide icons in every tool button and stable table/button dimensions. Keep page sections unframed, with cards only for the candidate/backup modal.

- [ ] **Step 5: Run page tests, full frontend tests, and build.**

Run: `cd frontend && pnpm test -- --run src/pages/SkillsPage.test.tsx src/backend/wails.test.ts && pnpm run build`

Expected: PASS and a production bundle generated with no TypeScript errors.

```bash
git add frontend/src/pages/SkillsPage.tsx frontend/src/pages/SkillsPage.test.tsx frontend/src/App.tsx frontend/src/components/NavigationSidebar.tsx frontend/src/i18n.tsx frontend/src/styles/app.css
git commit -m "feat: add Skills Registry page"
```

### Task 9: Add documentation and fake-runner coverage

**Files:**

- Modify: `README.md`
- Modify: `README_ZH.md`
- Create: `docs/internal/skills-registry-acceptance.md`
- Modify: `cmd/bootagent-desktop/core_e2e.go`, `internal/binding/skill_dialog_server.go`, `frontend/e2e/wails-server.mjs`, `frontend/e2e/wails.spec.ts`
- Test: `frontend/e2e/wails.spec.ts`

- [ ] **Step 1: Document the shipped behavior in both README languages.**

Add a matching “Skills Registry” paragraph/table to both READMEs describing the four supported Agents, local folder/ZIP/Agent import, copy-based Apply, conflict variants, and private backup/restore location. Keep commands and paths synchronized and do not mention remote catalogs.

- [ ] **Step 2: Write the maintainer acceptance checklist.**

Create `docs/internal/skills-registry-acceptance.md` with the eight numbered acceptance criteria from the design, exact commands (`go test ./...`, `go test -race ./...`, `go vet ./...`, `cd frontend && pnpm run test:e2e`, `python3 scripts/check-docs.py`), and a note that no Agent root is created by Scan.

- [ ] **Step 3: Add deterministic server-mode fixtures and two E2E flows.**

Add a server-only `selectSkillFolder` implementation in `internal/binding/skill_dialog_server.go` that reads the test-only `ONEAGENT_E2E_SKILL_FOLDER` environment variable and returns it, while the non-E2E server path returns the existing unavailable-dialog error. In `frontend/e2e/wails-server.mjs`, create one valid folder candidate below the temporary home and set that environment variable before spawning Wails. In `cmd/bootagent-desktop/core_e2e.go`, create initialized Claude/Codex Skills roots containing equal and differing fixture trees so the scan has one synced row and one conflict. Add two tests to `frontend/e2e/wails.spec.ts`: import the folder candidate, choose Codex, Apply, and assert the success state; then select a conflict hash and assert Apply is disabled until the explicit variant choice exists. Keep source paths out of assertions and browser storage.

- [ ] **Step 4: Run documentation and E2E checks, then commit.**

Run: `python3 scripts/check-docs.py && cd frontend && pnpm run test:e2e`

Expected: `check-docs.py` reports `ok` and both Skills scenarios pass.

```bash
git add README.md README_ZH.md docs/internal/skills-registry-acceptance.md cmd internal frontend
git commit -m "docs: document Skills Registry workflow"
```

### Task 10: Run repository gates and inspect the final diff

**Files:**

- Test only; no source changes unless a gate identifies a concrete defect.

- [ ] **Step 1: Run all Go gates.**

Run: `go test ./... && go test -race ./... && go vet ./...`

Expected: all commands exit `0`.

- [ ] **Step 2: Run frontend and release-compliance gates.**

Run: `cd frontend && pnpm install --frozen-lockfile && pnpm run test && pnpm run build && pnpm run test:e2e && cd .. && python3 -m unittest scripts/test_generate_third_party_licenses.py && python3 scripts/generate_third_party_licenses.py --check && python3 scripts/check-docs.py`

Expected: all commands exit `0`; no dependency or NOTICE change is required.

- [ ] **Step 3: Inspect for leaks and unrelated churn.**

Run: `git diff --check`, `git status --short`, and `rg -n 'source path|staging|SKILL.md body|api[_-]?key' internal/skill internal/app/skill.go frontend/src/pages/SkillsPage.tsx`.

Expected: only redacted diagnostics and intentional UI labels are present; no source path or Skill content is logged or stored in frontend state.

- [ ] **Step 4: Commit any concrete gate fix and rerun the failing gate.**

Use a focused commit such as `fix: reject changed Skill import source` only when a test demonstrates the defect; rerun that gate before proceeding.

- [ ] **Step 5: Record completion evidence.**

Capture the exact passing commands and the final commit IDs in the handoff. Do not claim completion until the verification command output is fresh.

---

## Self-review

- **Spec coverage:** catalog paths are Task 1; identity/hash/front matter are Task 2; recursive folder and ZIP limits are Task 3; private SSOT, Registry, backups, and retention are Task 4; scan/observation semantics, TOCTOU validation, partial Apply, deletion safety, uninstall, restore, and shared dirty state are Task 5; native dialogs and close guard are Task 6; generated binding and bridge constraints are Task 7; UI, conflicts, imports, backups, route blocking, and localization are Task 8; README and fake-runner acceptance are Task 9; all repository gates are Task 10.
- **Placeholder scan:** the plan contains no `TODO`, `TBD`, “implement later”, or unresolved file-discovery instruction. The only path wildcard is the generated `frontend/bindings/**` output, whose exact generation command is specified.
- **Type consistency:** `SkillSummary`, `SkillCandidate`, `SkillScanResult`, `SkillImportPreview`, `SkillChange`, `SkillApplyRequest`, `SkillApplyResult`, `SkillUninstallResult`, and `SkillBackupSummary` are defined in Task 5 and reused unchanged by Tasks 6–8. `PreviewImport` receives only `kind` over Wails and returns a token; `Apply` consumes `source_token` and never receives a renderer filesystem path.
- **Deliberate simplifications:** first release has one process-wide write lock, copy-only projections, a five-minute in-memory import token, and a bounded newest-20 backup list. Upgrade those only when concurrent throughput, symlink compatibility, or remote catalog requirements become real product needs.
