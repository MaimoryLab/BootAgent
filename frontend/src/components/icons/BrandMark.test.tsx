import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AGENT_ICON_IDS, agentMarkKind } from "./agents";
import { BrandMark } from "./BrandMark";

describe("BrandMark", () => {
  it("renders an inline svg so currentColor resolves against this document", () => {
    const { container } = render(<BrandMark />);
    const svg = container.querySelector("svg");
    expect(svg).toBeTruthy();
    // An <img src> would make the SVG a separate document where currentColor
    // falls back to black; the Agent marks are inlined for the same reason.
    expect(container.querySelector("img")).toBeNull();
    // var(--brand) on the stem is the stricter of the two reasons to inline:
    // a custom property does not cross into an <img>'s document at all, so the
    // stem would not paint rather than merely paint the wrong colour.
    expect(svg?.querySelector("line")?.getAttribute("stroke")).toBe("var(--brand)");
    expect(svg?.querySelector("circle")?.getAttribute("stroke")).toBe("currentColor");
  });

  it("draws the mark with strokes, not fills", () => {
    const { container } = render(<BrandMark />);
    const svg = container.querySelector("svg");
    // The ring is a stroked circle with an open centre. A fill would close it.
    expect(svg?.getAttribute("fill")).toBe("none");
    expect(svg?.querySelector("circle")).toBeTruthy();
    expect(svg?.querySelector("line")).toBeTruthy();
  });

  it("hardcodes no colour at all, so both strokes follow the theme", () => {
    const { container } = render(<BrandMark />);
    const markup = container.innerHTML.toLowerCase();
    // Any hex at all, which subsumes the two literal blues this used to name:
    // the brand's own #d96e49 is banned here too, because the dark theme lifts
    // it to #e4855c and the stem has to reach it through var(--brand).
    expect(markup).not.toMatch(/#[0-9a-f]{3,8}/);
  });

  it("is hidden from assistive technology, since the wordmark carries the name", () => {
    const { container } = render(<BrandMark />);
    expect(container.firstElementChild?.getAttribute("aria-hidden")).toBe("true");
  });

  it("honours the requested size", () => {
    const { container } = render(<BrandMark size={40} />);
    const host = container.firstElementChild as HTMLElement;
    expect(host.style.width).toBe("40px");
    expect(host.style.height).toBe("40px");
  });

  it("stays out of the third-party Agent mark inventory", () => {
    // The MARKS table exists to track vendor artwork with a recorded licence and
    // SHA-256. This mark is first-party, so registering it there would assert a
    // third-party provenance it does not have.
    expect(AGENT_ICON_IDS).not.toContain("bootagent");
    expect(agentMarkKind("bootagent")).toBe("fallback");
  });
});
