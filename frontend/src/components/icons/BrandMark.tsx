/**
 * OneAgent's own mark.
 *
 * Deliberately not part of the MARKS table in agents.tsx. That table exists to
 * track third-party vendor artwork -- every entry carries a source, licence, and
 * SHA-256 in asset-rights.json, and agents.test.tsx asserts each one has a
 * recorded basis. This mark is first-party work under the repository's own
 * licence, so recording it there would claim a third-party provenance it does
 * not have.
 *
 * The path is traced from the master raster in the site repository
 * (public/images/brand/oneagent-logo.png) rather than redrawn, so the geometry
 * stays the designer's. It paints with fill="currentColor" instead of the
 * brand's #007AFF: that value matches --blue on the light theme exactly, but the
 * dark theme sets --blue to #0a84ff, so a hardcoded fill would be visibly off
 * against every other blue in the window. Inheriting the colour keeps it in step
 * with both themes.
 */
import brandMark from "./assets/oneagent-mark.svg?raw";

export function BrandMark({ size = 22 }: { size?: number }) {
  // Inlined rather than <img src>, for the same reason the Agent marks are: an
  // SVG fetched by <img> is an isolated document where currentColor resolves to
  // black instead of the surrounding text colour.
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
