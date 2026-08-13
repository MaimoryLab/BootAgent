# ADR-009: Standalone Public Site and GitHub Release as the Source of Truth

- Status: Partially Superseded (2026-08-04) -- "maintained in the same repository" in
  decision 1 has been overturned, and the site moved out to
  [MaimoryLab/BootAgent-site](https://github.com/MaimoryLab/BootAgent-site); "do not add
  marketing routes to the local Launcher" in that same decision, and decisions 2-5,
  still hold. The read path in decision 4 becomes the `data/` copy vendored by the site
  repository, refreshed from release tags. For the current operating guide see
  [public-site-operations.md](../public-site-operations.md); this file is kept as
  background only.
- Date: 2026-07-28 (originally numbered ADR-006, which collided with "Multiple Profiles
  and Long-Term Environment Management", renumbered to 009 on 2026-08-04; the
  Supersedes line in ADR-008 points at that one, not at this document)

## Context

The BootAgent React frontend is the operating UI bundled with the local Wails Launcher.
Public downloads, searchable content, release evidence, and enterprise services need
static indexable pages, and the two differ in security, caching, routing, and release
cadence. The site build reads the Release API and repository JSON directly, keeping an
independent release cadence.

## Decision

1. Maintain a standalone `site/` Astro static site in the same repository, and do not
   add marketing routes to the local Launcher.
2. The App workflow only creates a GitHub Release; the site workflow is triggered
   independently by site changes, a Release publication, or manual action.
3. Public versions, release dates, download assets, sizes, and digests are read only
   from the GitHub Releases API, not from a local App build directory, and no
   hand-maintained fallback version is kept.
4. The Agent compatibility catalog reads `agents.lock.json` directly; Provider runtime
   endpoints and commercial disclosure both read `providers.lock.json`, and commercial
   fields must not influence rank or technical conclusions.
5. The site loads no client-side analytics scripts by default; the Launcher stays
   telemetry-free by default.

## Consequences

- The Launcher needs no refactoring for site SEO, domains, or external hosting.
- Drafts and local builds never appear on the site; only a published GitHub Release can
  produce a version and a download button.
- A GitHub Pages deploy does not build the App, and an App release neither builds nor
  deploys Pages.
- The site needs only the Node toolchain; the App source package no longer carries the
  site source.
