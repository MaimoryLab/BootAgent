/**
 * BootAgent's own mark.
 *
 * Deliberately not part of the MARKS table in agents.tsx. That table exists to
 * track third-party vendor artwork -- every entry carries a source, licence, and
 * SHA-256 in asset-rights.json, and agents.test.tsx asserts each one has a
 * recorded basis. This mark is first-party work under the repository's own
 * licence, so recording it there would claim a third-party provenance it does
 * not have.
 *
 * The geometry is a construction rather than a trace: a stroked circle for the
 * bowl and a round-capped line for the stem, the b that boots. brand/README.md
 * records the exact numbers, and every other copy of the mark -- the site's, the
 * favicon, the platform icon sets -- is rendered from them rather than scaled
 * off this file.
 *
 * The two strokes are coloured differently on purpose, and neither is a literal.
 * The ring takes currentColor, which .brand-mark-glyph sets to --text-primary,
 * so it is ink on the light theme and cream on the dark one -- the same flip the
 * brand set ships as a separate reversed file. The stem takes var(--brand), the
 * one place the logo's undarkened terracotta is allowed: at 2.79:1 on the page
 * it could not have carried text or a control's boundary, so --accent is a
 * deeper step and the raw value stays here, on the mark it came from.
 *
 * Both depend on the SVG being inlined -- see below.
 */
import brandMark from "./assets/bootagent-mark.svg?raw";

export function BrandMark({ size = 22 }: { size?: number }) {
  // Inlined rather than <img src>, for the same reason the Agent marks are: an
  // SVG fetched by <img> is an isolated document where currentColor resolves to
  // black instead of the surrounding text colour. var(--brand) is the stricter
  // constraint of the two -- a custom property does not cross into that document
  // at all, so through <img> the stem would not paint.
  return (
    <span
      className="brand-mark-glyph"
      style={{ width: size, height: size }}
      aria-hidden="true"
      data-mark-kind="brand"
      dangerouslySetInnerHTML={{ __html: brandMark }}
    />
  );
}
