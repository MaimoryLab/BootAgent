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
    expect(svg?.getAttribute("fill")).toBe("currentColor");
    expect(svg?.querySelectorAll("path").length).toBeGreaterThan(0);
  });

  it("does not hardcode the brand blue, so it follows the theme", () => {
    const { container } = render(<BrandMark />);
    const markup = container.innerHTML.toLowerCase();
    // #007AFF equals --blue on the light theme but not the dark one (#0a84ff),
    // so a literal here would be visibly wrong in dark mode.
    expect(markup).not.toContain("007aff");
    expect(markup).not.toContain("0a84ff");
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
