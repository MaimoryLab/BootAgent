# OneAgent public site operations and release handbook

Status: moved out. For the site's own build commands, environment variables, and
deployment steps, see the README in
[MaimoryLab/OneAgent-site](https://github.com/MaimoryLab/OneAgent-site).

The public site used to be this repository's `site/` directory and is now a separate
repository. This file keeps only the parts that still constrain this repository, and no
longer repeats the site-side steps -- two copies would drift apart sooner or later.

The `.github/workflows/technical-preview.yml` and `.github/workflows/site.yml` this
document originally described no longer exist (this repository now has only
`build-artifacts.yml`), so the release sequence written around those two workflows is
obsolete. Do not follow it.

## What this repository still owns

**A GitHub Release is the source of truth for a public version and its assets.** At build
time the site calls the GitHub Releases API and reads only published, non-draft releases.
The version tag, release date, download URL, file size, and SHA-256 shown on the page all
come from there. The site does not read a local `release/` directory, does not copy
download assets, and maintains no fallback version values. This repository's obligation
follows from that: once a release is published it is public fact, so assets, checksums,
and signing status must be verified **before** publishing.

**`providers.lock.json` is the source of truth for commercial disclosure fields.** A
Provider's `relationship`, `disclosure`, and `referral_url` are maintained here, and they
must not influence Agent rank, compatibility conclusions, default selection, or connection
tests. That boundary belongs to this repository; the site only displays the result.

**Changing a lock file does not change the site.** The site vendors `agents.lock.json`
and `providers.lock.json` into its own `data/` directory, refreshed from release tags
rather than tracking this repository's `main`. This is deliberate: the site describes what
a published version supports, and following `main` would advertise an Agent that is merged
but not yet released. After adding an Agent or adjusting a disclosure field, refresh the
site repository per its `data/README.md`.

**The Stable gate is unchanged.** Per-platform signing, notarization, and the native
cleanroom gate remain requirements of the app release process. A GitHub Release does not
substitute for verifying the artifacts.

## Background

The design decision is recorded in
[ADR-009](decisions/ADR-009-public-site-and-generated-release-index.md). The part of that
ADR about maintaining `site/` in the same repository has been superseded by this split;
its conclusion about not adding marketing routes to the local Launcher still holds.
