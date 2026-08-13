# BootAgent brand sources

The mark is a stroked ring with a round-capped stem set beside it: the b that
boots. It is defined as **geometry, not artwork**, and every raster in this
repository and in `MaimoryLab/BootAgent-site` is rendered from that definition
rather than traced from a bitmap.

## The geometry contract

On the viewBox `24 -1 84 108`:

| Part | Definition |
| --- | --- |
| Ring | `circle` at `cx 66`, `cy 65`, `r 33`, stroke only, no fill |
| Stem | `line` from `33,8` to `33,98`, `stroke-linecap="round"` |
| Stroke | `12` units for both, except where noted under *Small sizes* |

Anything that reproduces the mark should reproduce those numbers. Redrawing it by
eye, or scaling a PNG, is how the two surfaces drift apart.

## Colours

| Token | Light | Dark | Role |
| --- | --- | --- | --- |
| `--brand` | `#D96E49` | `#E4855C` | The stem. Decorative only. |
| `--text-primary` / `--ink` | `#211F1B` | `#F4EFE6` | The ring, via `currentColor`. |

**The brand terracotta cannot carry text or a control's only boundary.**
`#D96E49` is 2.79:1 on the light page background and 3.35:1 under white — both
below WCAG AA, and the second below even the 3:1 that non-text UI needs. It
exists for the mark and for large brand shapes that carry no information.
Anything actionable uses `--accent` (`#A94623` light, `#E4855C` dark), which
clears AA both as text on our grounds and as a fill under `--on-accent`.

## Files

| File | What it is |
| --- | --- |
| `bootagent-mark.svg` | Ink ring, on transparency. The primary mark. |
| `bootagent-mark-reversed.svg` | Cream ring, for dark grounds. |
| `bootagent-favicon.svg` | Stroke `14`, for small sizes. Shipped as the site's SVG favicon. |
| `bootagent-app-icon.svg` | `#211F1B` plate, `rx 28` on a 128 viewBox, reversed mark inset `0.78`. |
| `*.png` | Rasters of the above, kept so a reviewer can see them without a renderer. |
| `lockup-{light,dark}.{html,png}` | The wordmark lockup: `boot` in ink, `Agent` in terracotta, set in Jura Medium. The HTML is the source the PNG was rendered from; the font is not vendored, so install Jura Medium to re-render it exactly. |
| `bootagent-logo-source.jpeg` | The original raster the brand set was delivered alongside, kept for provenance. Superseded by the SVGs — do not render from it. |

## In-product use

Neither surface references these files at runtime. Both inline the geometry so it
can be themed:

- `frontend/src/components/icons/assets/bootagent-mark.svg` — ring on
  `currentColor`, stem on `var(--brand)`, inlined by `BrandMark.tsx`.
- `BootAgent-site/src/components/BrandMark.astro` — the same two strokes, inlined
  for the same reason.

Inlining is load-bearing. Through `<img src>` the SVG is an isolated document:
`currentColor` falls back to black, and a custom property does not cross into it
at all, so the stem would not paint. One file therefore covers both themes, and
`bootagent-mark-reversed.svg` is only needed where inlining is impossible.

## Regenerating the rasters

`qlmanage` is the only SVG rasteriser on a stock macOS box and it composites onto
white, which destroys the alpha the mark needs. The rasters here and the icon sets
in `build/` were produced by rendering the two shapes' exact distance fields
instead. See `build/Taskfile.yml` for the per-size stroke rules the Windows icon
needs — below about 32px a uniformly scaled 12-unit stroke closes the ring into a
disc, so the 16×16 entry widens it to 18.
