# MCP Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement a private MCP Registry that discovers configured servers, preserves conflicts and native extensions, and lets users apply selected MCP servers to every eligible supported Agent without delaying the first screen.

**Architecture:** A normalized model and five small format adapters live in internal/mcp. internal/app owns eligibility, background-safe scanning, conflict reconciliation, Apply orchestration, import/export, and the existing writeMu. internal/binding exposes one generated Wails service. React keeps drafts in page memory and calls only the generated binding.

**Tech Stack:** Go 1.26, existing securefs.AtomicWrite, encoding/json, TOML/YAML parsers already in the repository, github.com/tailscale/hujson for OpenCode/Kilo JSONC, Wails beta.7, React/TypeScript, Vitest, pnpm.

---

## File Map

### Go

- Modify internal/catalog/types.go, internal/catalog/manifest.go, manifests/agents.lock.json: add MCP adapter, section, and global-path metadata for Claude Code, Codex, OpenCode, Kilo CLI, and Hermes; validate and clone the new fields.
- Create internal/catalog/mcp_test.go: verify embedded metadata and platform path selection.
- Modify go.mod, go.sum, scripts/generate_third_party_licenses.py, and generated third-party license output: add hujson with its BSD-3-Clause attribution.
- Create internal/mcp/model.go and internal/mcp/model_test.go: portable specs, Registry schema v1, canonical normalization, ID validation, redaction, and secret-path metadata.
- Create internal/mcp/crypto.go and internal/mcp/crypto_test.go: password export encryption/decryption with standard-library PBKDF2-SHA256 and AES-256-GCM.
- Create internal/mcp/adapter.go, claude.go, codex.go, opencode.go, kilo.go, hermes.go: native readers, projectors, and patchers.
- Create matching adapter tests under internal/mcp, including JSONC comments/trailing commas and unrelated-field preservation.
- Create internal/mcp/store.go, internal/mcp/transfer.go, and tests: private Registry persistence and import/export envelopes.
- Create internal/app/mcp.go and internal/app/mcp_test.go: scan, conflict reconciliation, Apply, partial failures, draft close state, and eligibility.
- Modify internal/binding/services.go and internal/binding/services_test.go; create internal/binding/mcp.go and internal/binding/mcp_test.go: register and test the Wails MCP service.
- Modify cmd/oneagent-desktop/main_wails.go: register MCPService and install the localized native close confirmation hook.

### Frontend

- Regenerate frontend/bindings after DTO changes; never hand-edit generated files.
- Modify frontend/src/backend/wails.ts and frontend/src/types/api.ts: expose typed MCP calls and aliases.
- Modify frontend/src/i18n.tsx, frontend/src/App.tsx, frontend/src/components/NavigationSidebar.tsx, and frontend/src/styles/app.css: route, navigation, translations, and scoped layout.
- Create frontend/src/pages/MCPPage.tsx and frontend/src/pages/MCPPage.test.tsx: table, modal structured editor, Advanced JSON, conflict/import/apply states, and background scan.
- Reuse existing transfer file dialogs where possible; add only MCP-specific controls and tests.

### Documentation

- Modify README.md and README_ZH.md: document the MCP Registry, supported Agents, global-only scope, and local file transfer.
- Add docs/internal/mcp-registry-acceptance.md: maintainer acceptance checklist matching implemented behavior.

---

## Task 1: Catalog Metadata And Dependency

**Files:** internal/catalog/types.go, internal/catalog/manifest.go, manifests/agents.lock.json, internal/catalog/mcp_test.go, go.mod, go.sum, scripts/generate_third_party_licenses.py

- [ ] Write a failing catalog test that loads catalog.LoadEmbedded and asserts the five MCP entries expose the expected adapter, section, and platform path; assert Aider and OpenClaw have no MCP metadata.
- [ ] Run go test ./internal/catalog -run MCP -count=1; confirm it fails because the fields do not exist.
- [ ] Add these fields to catalog.Agent: MCPAdapter string, MCPSection string, MCPConfigPath string, and MCPWindowsConfigPath string. Keep them optional so non-MCP Agents remain valid.
- [ ] Extend manifest decoding, validation, and clone logic. Reject a non-empty MCP adapter without a section or path, and reject an MCP path that is absolute or project-local.
- [ ] Add metadata to the lock manifest: Claude Code claude / .claude.json / mcpServers; Codex codex / .codex/config.toml / mcp_servers; OpenCode opencode / .config/opencode/opencode.json / mcp; Kilo kilo / .config/kilo/kilo.jsonc / mcp; Hermes hermes / .hermes/config.yaml / mcp_servers. Kilo accepts an existing .json fallback in its adapter without a new catalog entry.
- [ ] Add platform-aware path selection tests for Unix and Windows metadata.
- [ ] Add github.com/tailscale/hujson with GOPROXY=https://proxy.golang.org go get github.com/tailscale/hujson; run go mod tidy.
- [ ] Add the module to GO_LICENSES in scripts/generate_third_party_licenses.py, run python3 scripts/generate_third_party_licenses.py, and verify the generated attribution is BSD-3-Clause.
- [ ] Run go test ./internal/catalog ./internal/config and commit feat: add MCP catalog metadata.

## Task 2: Portable Model, Secrets, And Transfer Cryptography

**Files:** internal/mcp/model.go, internal/mcp/model_test.go, internal/mcp/crypto.go, internal/mcp/crypto_test.go

- [ ] Write table tests for valid/invalid IDs, omitted type inference, empty-field normalization, order-insensitive object comparison, ordered args, and extension preservation.
- [ ] Define the minimal model:

~~~go
type Spec struct {
    Type       string
    Command    string
    Args       []string
    Env        map[string]string
    Cwd        string
    URL        string
    Headers    map[string]string
    Extensions map[string]json.RawMessage
}
type Variant struct { Agents []string; Spec Spec }
type ServerFact struct { Variants []Variant }
type Registry struct { SchemaVersion int; Servers map[string]ServerFact }
~~~

- [ ] Implement ValidateID, Normalize, EqualNormalized, and transport validation. Normalize map keys for comparison, preserve array order, infer stdio from a command, reject contradictory fields, and cap user-controlled strings at the request limits.
- [ ] Implement secret-path traversal for env, environment, headers, http_headers, and nested names authorization, token, api_key, apikey, client_secret, and clientSecret. Return redacted copies and metadata paths; never include secret values in summaries or errors.
- [ ] Write crypto tests for round-trip, wrong password, tampered ciphertext, random salt/nonce, and malformed envelope.
- [ ] Implement a versioned encrypted payload with PBKDF2-SHA256, 256-bit key, fresh random salt and nonce, and AES-GCM authentication using only crypto/rand, crypto/aes, crypto/cipher, and crypto/sha256 plus the repository's available PBKDF2 package. Enforce bounded iterations and reject oversized plaintext.
- [ ] Run go test ./internal/mcp -run 'Test(ID|Normalize|Secret|Crypto)' -count=1 and commit feat: add portable MCP model and secret handling.

## Task 3: Native Adapter Contract And JSONC Adapters

**Files:** internal/mcp/adapter.go, internal/mcp/opencode.go, internal/mcp/kilo.go, and their tests

- [ ] Write adapter tests first for reading a missing section as empty; decoding stdio and remote entries; preserving unknown native fields under the Agent extension; patching one ID while retaining unrelated MCP and non-MCP fields; deleting one ID; and rejecting malformed input without returning a deletion.
- [ ] Define one adapter contract:

~~~go
type Adapter interface {
    Read(ctx context.Context, path string) (Observed, error)
    Apply(ctx context.Context, path string, current []byte, changes map[string]*Spec) (content []byte, secret bool, err error)
}
type Observed struct { Servers map[string]ObservedServer }
type ObservedServer struct { Spec Spec; Native json.RawMessage }
~~~

- [ ] Implement a shared JSONC helper using hujson.Parse, Value.Find, RFC6902 Value.Patch, and Value.Format. Select .jsonc when it exists, then .json; never rewrite unrelated keys by decoding into a generic map.
- [ ] Keep raw native entries for lossy remote transport. Decode generic remote entries as http; when a prior variant contains an sse hint, compare its projection before deciding whether to retain that hint.
- [ ] Make Apply replace or remove exactly the requested server IDs, preserving comments, trailing commas, unknown members, and all untouched top-level content. Return secret=true whenever the resulting section can contain credentials.
- [ ] Run go test ./internal/mcp -run 'Test(OpenCode|Kilo|JSONC)' -count=1 and commit feat: support OpenCode and Kilo MCP formats.

## Task 4: Claude Code, Codex, And Hermes Adapters

**Files:** internal/mcp/claude.go, internal/mcp/codex.go, internal/mcp/hermes.go, and tests

- [ ] Add failing fixtures for each native syntax, including existing unrelated settings, nested server data, missing files, malformed files, and delete operations.
- [ ] Implement Claude Code JSON mcpServers using the existing JSON parser/writer conventions, retaining unrelated keys and using secure atomic writes.
- [ ] Implement Codex TOML mcp_servers with ordered command, args, env, cwd, and remote URL/header mapping; preserve unrelated TOML tables through the repository parser.
- [ ] Implement Hermes YAML mcp_servers with the same portable mapping and retain unrelated YAML fields.
- [ ] Ensure every adapter reports parse errors with a redacted path/diagnostic and never converts an unreadable file into an empty observation.
- [ ] Run go test ./internal/mcp -run 'Test(Claude|Codex|Hermes)' -count=1 and commit feat: add Claude Code Codex and Hermes adapters.

## Task 5: Private Registry Store And Import/Export Envelope

**Files:** internal/mcp/store.go, internal/mcp/transfer.go, and tests

- [ ] Write store tests for absent file, valid schema v1, malformed/newer schema, atomic replacement, private mode, and backup failure behavior.
- [ ] Implement Store with an injected filesystem and path. Read absent ~/.oneagent/mcp.json as an empty v1 Registry; reject malformed or newer versions without overwriting; write through securefs.AtomicWrite with secret mode and the existing backup/permission sequence.
- [ ] Write transfer tests for omit/encrypt/plaintext modes, metadata path retention, 4 MiB cap, duplicate IDs, and schema validation.
- [ ] Define a dedicated envelope carrying schema version, selected factual variants, Agent associations, secret mode, and encrypted payload metadata. Omit mode removes secret values while retaining their paths; encrypted mode wraps complete specs; plaintext requires an explicit confirmation flag.
- [ ] Implement import preview as pure validation/diff data. Apply no writes while previewing. Resolve collisions with Keep local, Use imported, or Save as new ID, rejecting invalid new IDs and ambiguous missing-secret merges.
- [ ] Run go test ./internal/mcp -run 'Test(Store|Transfer|Import|Export)' -count=1 and commit feat: add private MCP store and transfer envelope.

## Task 6: App Scan, Conflict Reconciliation, And Apply

**Files:** internal/app/mcp.go, internal/app/mcp_test.go

- [ ] Write use-case tests for initialized/command-detected eligibility, hidden uninitialized Agents, first scan import, repeat scan external additions/deletions, same-ID conflicts, parse-error retention, lossy SSE retention, dirty draft preservation, partial Apply, forced overwrite of externally changed touched IDs, and Registry-write failure after config success.
- [ ] Add a path helper that resolves the catalog MCP path under the existing home/platform status and checks that the parent configuration root already exists. Command detection uses the existing status/runtime lookup; no directories are created during eligibility or scan.
- [ ] Add app-level DTOs for redacted list rows, explicit detail, scan diagnostics, draft state, per-Agent Apply results, and import/export requests. Keep full Spec values out of list/status DTOs.
- [ ] Implement List as an atomic read of the Registry redacted factual snapshot. Implement Scan under writeMu: snapshot eligible Agents, read adapters concurrently only after eligibility is known, merge normalized observations, preserve prior facts on parse errors, and atomically store the result.
- [ ] Implement Apply under writeMu: validate all changes first; for each selected Agent reread its file, patch only touched IDs, write via securefs.AtomicWrite, then persist the successful factual association. Return independent success/failure records and keep failed changes retryable.
- [ ] Add a small in-memory dirty flag with locale and a method that returns/clears it for the desktop close hook. The flag contains no server data or secrets.
- [ ] Run go test ./internal/app -run MCP -count=1 and go test ./internal/app ./internal/mcp -race; commit feat: implement MCP scan and Apply use cases.

## Task 7: Wails Binding And Native Close Confirmation

**Files:** internal/binding/services.go, internal/binding/mcp.go, tests, cmd/oneagent-desktop/main_wails.go

- [ ] Write binding tests for method allowlisting, redacted List, explicit Get, Scan delegation, partial Apply serialization, transfer preview/export, and draft-state propagation.
- [ ] Implement MCPService methods with this surface: List, Scan, Get(id, sourceAgent), Apply(request), Export(request), PreviewImport(request), and SetDraftState(dirty, locale). Convert domain errors through the existing binding error path.
- [ ] Add MCP *MCPService to binding.Services and instantiate it in NewServicesWithOptions; update the service method allowlist test.
- [ ] Regenerate Wails bindings using the repository's existing generation command, then update handwritten frontend/src/backend/wails.ts and frontend/src/types/api.ts from generated signatures.
- [ ] In the desktop entry, register MCPService and a WindowClosing hook. On a dirty flag, cancel the first event, show app.Dialog.Question with Chinese/English text selected by locale, clear the flag and set a one-shot bypass before calling window.Close on discard; leave the window open on cancel. Verify the hook cannot recurse.
- [ ] Run go test ./internal/binding ./cmd/oneagent-desktop/... -count=1 and commit feat: expose MCP registry through Wails.

## Task 8: React MCP Page And Draft Workflow

**Files:** frontend/src/backend/wails.ts, frontend/src/types/api.ts, frontend/src/pages/MCPPage.tsx, frontend/src/pages/MCPPage.test.tsx, frontend/src/App.tsx, frontend/src/components/NavigationSidebar.tsx, frontend/src/i18n.tsx, frontend/src/styles/app.css

- [ ] Add failing component tests for immediate first render, asynchronous scan indicator, hidden ineligible Agents, table row statuses, modal open/edit/delete, structured fields, Advanced JSON unknown-field preservation, conflict resolution, target toggles, partial Apply retry, and discard confirmation on route leave.
- [ ] Add typed wrappers for every MCP binding method. Keep full specs in component state only after explicit Get, edit, import preview, or export; list and scan state use redacted summaries.
- [ ] Add the /mcp route and a Network/Cable navigation item. Use Chinese-first translation keys and add English translations in the existing dictionary.
- [ ] Implement MCPPage with a table of server ID, transport, source Agents, conflict/pending/failed status, and target controls. Render the table before awaiting Scan; trigger Scan from useEffect and keep the scan promise independent of initial route render.
- [ ] Implement a modal editor with structured stdio/http/sse controls plus an Advanced JSON textarea. Parse and validate JSON before accepting it; merge edits without dropping unknown extensions fields. Use icon buttons with accessible labels for edit/delete/close, and stable dimensions for table rows and controls.
- [ ] Implement import preview and collision choice controls. Confirmed choices update only page-local draft. Apply sends all touched IDs and selected eligible targets; successful targets are removed from pending work, failures remain retryable, and the response never displays secret values.
- [ ] Add React Router navigation blocking for dirty drafts. Call SetDraftState whenever dirty/locale changes, clear it after discard or successful Apply, and ensure native close confirmation receives only the flag and locale.
- [ ] Add focused styles to app.css using existing tokens, compact table spacing, modal sizing, responsive overflow, and no nested cards.
- [ ] Run cd frontend && pnpm run test -- --run MCPPage, then pnpm run build, and commit feat: add MCP registry page.

## Task 9: Documentation And Release Compliance

**Files:** README.md, README_ZH.md, docs/internal/mcp-registry-acceptance.md, generated license output

- [ ] Add matching English and Chinese README text: MCP Registry is global user-level, supports Claude Code/Codex/OpenCode/Kilo CLI/Hermes, scans asynchronously, preserves conflicts, applies explicitly, and transfers locally with omit/encrypt/plaintext secret modes. Do not list Aider/OpenClaw as supported MCP targets.
- [ ] Add an internal acceptance checklist covering first-render timing, eligibility hiding, scan/error retention, conflict resolution, JSONC preservation, secret redaction, partial Apply, native close confirmation, import preview, and no config-root creation.
- [ ] Run python3 scripts/check-docs.py, python3 scripts/generate_third_party_licenses.py --check, and git diff --check; commit docs: document MCP registry.

## Task 10: Full Verification And Review

**Files:** all files changed above

- [ ] Run go test ./....
- [ ] Run go test -race ./....
- [ ] Run go vet ./....
- [ ] Run cd frontend && pnpm install --frozen-lockfile && pnpm run test && pnpm run build.
- [ ] Run python3 -m unittest scripts/test_generate_third_party_licenses.py, python3 scripts/generate_third_party_licenses.py --check, and python3 scripts/check-docs.py.
- [ ] Run the repository Wails generation/check command and confirm generated bindings are clean.
- [ ] Review git diff --stat, git diff --check, and rg -n 'api[_-]?key|authorization|client_secret|token' internal/mcp internal/app/mcp.go frontend/src/pages/MCPPage.tsx. Every match must be a secret-path classifier, an explicitly redacted test fixture, or a local form field that never enters global state, logs, or status.
- [ ] Start the desktop/browser test path, capture the MCP page at desktop and mobile widths, and verify the table renders before the scan completes, ineligible Agents are absent, controls do not overlap, and no credentials appear.
- [ ] Run git status --short; confirm only intended source, generated binding, documentation, module, and license files remain. Request a code review before merging.

---

## Self-review Checklist

- [ ] Every catalog-supported Agent has one adapter and one explicit global path; React does not duplicate the support list.
- [ ] Scan is asynchronous from the page effect and never part of application startup.
- [ ] Parse failures retain prior facts; external deletions are distinguished from unreadable files.
- [ ] Apply rereads and patches current files, preserves unrelated data, serializes through writeMu, and reports per-Agent partial results.
- [ ] Registry persistence follows securefs.AtomicWrite; no rollback is attempted after a successful native write.
- [ ] Secret values are absent from summaries, diagnostics, logs, browser storage, and ordinary status; explicit detail/import/export is the only full-spec boundary.
- [ ] Drafts are page-memory only and native close uses a non-secret dirty flag with a reentrant-safe one-shot bypass.
- [ ] JSONC comments/trailing commas and unknown extension fields survive edits.
- [ ] Import preview performs no writes and every confirmed import still requires Apply.
- [ ] README language pairs and generated bindings/license output are synchronized.
