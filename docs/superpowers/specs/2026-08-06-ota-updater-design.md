# OTA Updater Design

## Goal

Add opt-in desktop OTA updates backed by GitHub Releases. OneAgent checks once
per launch, asks before downloading, reports the download through the existing
task center, supports real cancellation, and lets the user restart when ready.

## User Flow

1. A release build silently checks the latest GitHub Release after the frontend
   starts. Development builds do not check.
2. No update or a failed background check produces no task or notification.
3. When a newer version exists, a native Wails question dialog shows the target
   version with `Update` and `Not Now` actions.
4. `Not Now` dismisses the update for the current launch only. The next launch
   checks again.
5. `Update` starts a task-center entry and downloads the selected artifact.
6. The task card shows byte progress and can cancel the underlying Wails call.
7. After verification and staging, the completed task exposes `Restart &
   Update`. The Wails updater swaps the executable or app bundle and relaunches
   OneAgent only after that action.

The built-in updater window is not used. In the pinned Wails beta.3,
`CheckAndInstall` opens that window but immediately calls
`DownloadAndInstall`; its buttons do not gate the download. A native question
dialog provides the requested confirmation without copying or forking Wails'
updater template.

## Architecture

### Backend

- Configure `app.Updater` with the GitHub provider for
  `MaimoryLab/OneAgent`, `SHA256SUMS`, the linker-injected current version, and
  `updater.WindowNone`.
- Strip the leading `v` from `internal/version.Version` before passing it to
  Wails. Do not configure the updater for the default `-dev` version.
- Add a small Wails service exposing `Check`, `DownloadAndInstall`, and
  `Restart`. `Check` returns only the available version string; an empty string
  means current or disabled.
- Translate Wails `EventDownloadProgress` payloads into the existing
  `oneagent:install-output` progress event with one stable OTA target.

Wails remains responsible for release comparison, download, checksum
verification, safe archive extraction, staging, executable/app-bundle swap,
rollback, and relaunch.

### Frontend

- Mount one update coordinator inside the existing task-center provider.
- Call `Check` once, then use `Dialogs.Question` only when a version is found.
- On approval, register an `update` task, attach the generated binding's
  canceller, and call `DownloadAndInstall`.
- Extend task records with one optional terminal action. The OTA task uses it
  for `Restart & Update`; existing tasks remain unchanged.
- Reuse the existing progress event listener, byte progress UI, cancellation
  state, failure state, dismissal, and task locking.

## Failure Handling

- Background check failures stay silent because there is no user-started task.
- Download cancellation ends the task as cancelled and cancels the binding
  context, which stops the HTTP request.
- Download, checksum, extraction, and staging failures end the task as failed.
- Restart failures keep the app running and show the failure on the task card;
  the restart action remains available for retry.
- A second startup check or download is not started while the OTA task is
  already active.

## Release Workflow

Reuse `.github/workflows/build-artifacts.yml` and trigger it for stable tags
matching `vX.Y.Z`. Keep the existing Windows/macOS and amd64/arm64 build
matrix, inject the tag through the existing linker flag, and publish these
GitHub Release assets:

- `OneAgent-darwin-amd64.zip`
- `OneAgent-darwin-arm64.zip`
- `OneAgent-windows-amd64.zip`
- `OneAgent-windows-arm64.zip`
- `SHA256SUMS`

Each macOS archive contains exactly one top-level `OneAgent.app`. Each Windows
archive contains exactly one top-level `oneagent-desktop.exe`. The platform and
architecture tokens intentionally match the Wails GitHub provider's default
asset matcher. The release job generates `SHA256SUMS` after collecting all four
archives and creates or updates the release for the pushed tag.

The checksum detects corruption and is the integrity mechanism supported by
the GitHub provider. Signing-key infrastructure is out of scope; add it only if
the release source moves to a provider or manifest that carries Wails signature
metadata.

## Verification

- Go tests cover disabled/current/new-version checks and the update service's
  delegation to Wails.
- Frontend tests cover `Update` versus `Not Now`, task creation, binding
  cancellation, completion, and the restart action.
- Existing task-center tests continue to cover shared progress rendering and
  task lifecycle behavior; add only the optional terminal-action cases.
- Run all Go tests, frontend tests, frontend build/typecheck, binding generation,
  and available workflow/archive validation before completion.

## Non-Goals

- Forced updates, periodic checks, prerelease channels, persisted skip-version
  preferences, a custom updater window, and release-note rendering.
- Reimplementing download, verification, extraction, swap, or rollback logic.
