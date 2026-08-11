# Contributing to OneAgent

Thanks for considering a contribution. This file covers what you need to send a
change; `AGENTS.md` holds the fuller development conventions and is worth reading
before a larger one. Participation is governed by `CODE_OF_CONDUCT.md`.

## Before you start

For a bug fix or a small change, open a pull request directly.

For a new feature, a new Agent, or a new built-in Provider, open an issue first.
OneAgent has a deliberate scope — `docs/product-boundary-baseline.md` records what it
does and does not do — and an issue is a cheaper place to find out that something
falls outside it than a finished branch.

## Prerequisites

- Go — the version in `go.mod`
- Node.js and pnpm, for the frontend
- Python 3 is not needed to build or test. `scripts/check-docs.py` uses it, and
  installing Aider needs Python 3.12 at runtime, which uv resolves on its own.

## Local verification

Run the relevant checks before opening a pull request. `.github/workflows/ci.yml`
runs four jobs — Go, Frontend, Docs, and Release compliance — and the commands
below cover their repository-local checks.

```bash
go test ./...
go test -race ./...
go vet ./...
```

```bash
cd frontend
pnpm install --frozen-lockfile
pnpm run test
pnpm run build
pnpm run test:e2e
```

Run the documentation and release-compliance checks as well:

```bash
python3 scripts/check-docs.py
python3 -m unittest scripts/test_generate_third_party_licenses.py
python3 scripts/generate_third_party_licenses.py --check
```

CI also runs `staticcheck` for Go. Report what you actually ran. If something
fails or you could not run it, say so in the pull request — that is more useful
than a green summary that does not hold.

## What a change should include

**A test for behaviour.** A bug fix needs a test that fails before it and passes
after. Verify that by running it against the unfixed code: a test that passes either
way is documentation, not a guard.

**Comments that say why, not what.** Explain a constraint the code cannot show. The
next reader can see what the line does.

**Matching style.** Read the surrounding code first and follow it rather than
introducing a new pattern.

Keep the change focused. A bug fix does not need the surrounding code tidied up;
unrelated cleanup is easier to review as its own pull request.

## Things that are easy to get wrong

- **`frontend/bindings/` is generated.** Do not edit it by hand. Regenerate it with
  the invocation in `build/Taskfile.yml`, including its build-tag flag — without the
  flag the generator reports zero services and empties the directory.
- **Secrets stay out of everything user-visible.** No API key in logs, error
  messages, ordinary configuration files, status payloads, URLs, global frontend
  state, browser storage, or test fixtures. Only Provider editing and configuration
  forms read a key, on demand, through a local binding.
- **UI copy is Chinese-source.** Add the Chinese string to `frontend/src/i18n.tsx`
  first; the translation key is a closed union, so English-only copy will not
  compile.
- **A new bundled dependency or third-party mark means a `NOTICE` update.**
  `docs/distribution-compliance-policy.md` treats this as a release prerequisite.
- **User-visible changes mean a README update**, in both `README.md` and
  `README_ZH.md`, in the same change.

## Documentation

`docs/` is organised by audience, and `AGENTS.md` says which directory takes what.
The rule that catches people out is language: `README.md` and the specifications in
the `docs/` root are English-only, while `AGENTS.md` and `docs/internal/` are
Chinese, because their audience is maintainers. `scripts/check-docs.py` enforces
this.

## Commits and pull requests

Write commit subjects as `<type>: <imperative summary>` — `fix:`, `feat:`, `chore:`,
`docs:`, `test:`. Keep one logical change per commit.

In the pull request, say what was wrong and why the change is the right fix, not
only what you edited. If you decided against an alternative approach, saying so
saves the reviewer from proposing it.

## Reporting bugs

Include the OneAgent version, your platform, what you did, what you expected, and
what happened. If an Agent install or a Provider connection failed, the message shown
in the UI is the useful part.

**Never paste an API key**, in an issue or anywhere else. Redact it. If you already
have, revoke it at the Provider.

## Security

Do not report vulnerabilities through issues. `SECURITY.md` has the private
reporting path.

## License

Contributions are licensed under Apache-2.0, the same as the project.
