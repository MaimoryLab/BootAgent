# ADR-010: Visual restyle direction — drafting-sheet Swiss with a blueprint signature

## Status

Proposed (comparison comps attached; awaiting review)

## Date

2026-08-17

## Context

The current visual language (warm cream ground `#efeae0`, terracotta accent
`#d96e49`) has two problems that grew over time rather than being chosen:

1. The palette now matches the most common AI-generated interface look — warm
   cream plus a terracotta accent that sits within a few RGB points of the
   Claude product accent. For a tool that must be recognisable next to
   competitors, this reads as "everyone" instead of "us".
2. The action accent (`--accent: #a94623`) and the danger colour
   (`--red: #a32b1d`) are near neighbours in hue. Primary and destructive
   actions in a tool that rewrites user configuration files should never share
   a colour family.

What must not be lost in a restyle: the token-first architecture
(`frontend/src/styles/tokens.css` feeding `base.css` and `app.css`), the
documented WCAG contrast maths on every colour token, the enforced light/dark
palette parity, and the CSS invariant tests in CI. The restyle replaces token
values and the component skin, not this architecture.

Product constraints that filter any style choice: dense configuration UI needs
quiet surfaces and strong hierarchy; both light and dark palettes are
mandatory; the Linux build renders through WebKitGTK, so blur-heavy materials
are out; the UI ships bilingual (zh/en), so every typeface decision needs a
CJK answer; fonts must be bundled locally (no runtime font fetching), so
licences must permit redistribution.

## Decision

Adopt a Swiss-typographic base with an engineering-drawing (blueprint)
signature layer, specified in full — palette hex values with computed contrast
ratios, type roles, and signature element scope — in the
[design brief](../ui-restyle-brief.md).

Summary of the direction:

- Ground: drafting-paper white with a faint drawing grid; dark mode inverts to
  a cyanotype-inspired deep blue-black. Surfaces are flat; 1px rules replace
  soft shadows.
- Accent: drafting blue (`#20618e` light / `#6cb0dc` dark). Blue is far from
  the red error family, resolving the accent/danger collision by construction.
- The existing terracotta becomes a brand-mark colour only (application icon,
  splash, about page). Assets under `brand/` are not redrawn.
- Type: one grotesque family carries all Latin hierarchy through weight and
  size (IBM Plex Sans), one mono for annotations and data (IBM Plex Mono),
  Noto Sans SC for CJK. All three are SIL OFL licensed, compatible with local
  bundling and the NOTICE generation flow.
- The signature layer — leader-line wiring, a title block, dimension-style
  annotations — appears in exactly three places (overview, wizard review step,
  empty states) and nowhere else.

A neo-brutalist direction was built as a full alternative comp and rejected
for the reasons recorded below, with the comp kept for reference.

Comparison comps (self-contained HTML, open in any browser):

- [Tile A — Swiss + Blueprint (recommended)](../assets/restyle/tile-a-swiss-blueprint.html)
- [Tile B — Neo-brutalist (alternative)](../assets/restyle/tile-b-neobrutalist.html)

Both tiles render the real overview screen content (same strings the
application ships) so the comparison is between styles, not between contents.

## Consequences

- `tokens.css` token names stay stable; values change wholesale. `base.css`
  and component CSS migrate screen by screen against screenshot baselines, as
  planned in the restyle phases of
  [the optimization plan](../ux-and-agent-capability-optimization-plan.md).
- The contrast comments in `tokens.css` become an executable CI check; the
  ratios in the brief were computed, not estimated, and the check must
  reproduce them.
- Bundling three font families adds roughly 1–2 MB (Latin) plus a subset CJK
  payload to the frontend bundle; the build gains a subsetting step for
  Noto Sans SC. Licence texts enter NOTICE through the existing generator.
- The blueprint signature is scoped to three surfaces by decision; extending
  it elsewhere requires revisiting this ADR, which is the intended brake
  against theming creep.
- Rejecting neo-brutalism is a decision about the product's register: thick
  strokes and hard offset shadows carry a playful, poster-like voice that
  fights the quiet confidence a configuration tool needs, its hard shadows do
  not survive dark mode honestly (they vanish against near-black and must be
  faked with light borders), and its visual density budget is spent on the
  frame rather than the content.
