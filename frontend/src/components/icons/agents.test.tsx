import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AGENT_ICON_IDS, AgentIcon, agentMarkKind, agentMarkRights, agentMarkSource, agentTagline } from "./agents";

// Shown on the first screen alongside them, so they need marks of their own even
// though OneAgent does not configure them.
const ALL = ["codex", "claude-code", "opencode", "kilo-cli", "aider"];

describe("AgentIcon", () => {
  it("has a distinct mark for every Agent on the first screen", () => {
    // The catalog group used to decide the icon, so all five one-click Agents
    // rendered the same glyph and none was identifiable without its label.
    for (const id of ALL) {
      expect(AGENT_ICON_IDS).toContain(id);
    }
    const rendered = ALL.map((id) => {
      const { container } = render(<AgentIcon agentId={id} />);
      return container.innerHTML;
    });
    expect(new Set(rendered).size).toBe(ALL.length);
    expect(rendered.every(Boolean)).toBe(true);
  });

  it("records auditable rights for every redistributed image asset", () => {
    // Every id whose mark is a real image, not a generic symbol. Asserting the
    // set rather than one example is what makes an unregistered mark fail here:
    // shipping artwork without a source, licence and hash is the defect.
    const assetIds = AGENT_ICON_IDS.filter((id) => agentMarkKind(id) === "asset");
    expect(assetIds.sort()).toEqual(["claude-code", "codex", "cursor", "kilo-cli", "opencode"]);
    for (const id of assetIds) {
      const rights = agentMarkRights(id);
      expect(agentMarkKind(id)).toBe("asset");
      expect(rights?.license).toBe("MIT");
      expect(rights?.source).toMatch(/^https:\/\//);
      expect(rights?.sha256).toMatch(/^[a-f0-9]{64}$/);
      expect(rights?.licenseSource).toMatch(/^licenses\//);
      expect(agentMarkSource(id)).toBe(rights?.source);
    }
    expect(agentMarkSource("brand-new-agent")).toBe("");
  });

  it("falls back rather than rendering nothing for an unknown Agent", () => {
    const { container } = render(<AgentIcon agentId="brand-new-agent" />);
    const svg = container.querySelector("svg");
    expect(svg).not.toBeNull();
    expect(container.querySelector("img")).toBeNull();
  });

  it("keeps the mark out of the accessibility tree", () => {
    // The Agent name sits right next to it, so announcing the mark duplicates.
    const { container } = render(<AgentIcon agentId="codex" />);
    const mark = container.querySelector('[data-mark-kind="asset"]');
    expect(mark?.getAttribute("aria-hidden")).toBe("true");
    // The inlined svg must not be announced separately from its wrapper.
    expect(container.querySelector("svg")?.getAttribute("role")).toBeNull();
  });

  it("renders every mark in the same square box", () => {
    // Uniformity comes from the container, not from restyling the artwork:
    // that is what lets marks from five different brands read as one set.
    for (const size of [18, 20]) {
      for (const id of ALL) {
        const { container } = render(<AgentIcon agentId={id} size={size} />);
        if (agentMarkKind(id) === "asset") {
          // The licensed assets declare their own size in em, so the wrapper
          // carries the pixel box and CSS stretches the glyph to fill it.
          const mark = container.querySelector<HTMLElement>('[data-mark-kind="asset"]');
          expect(mark?.style.width).toBe(`${size}px`);
          expect(mark?.style.height).toBe(`${size}px`);
        } else {
          const mark = container.querySelector<SVGElement>("svg");
          expect(mark?.getAttribute("width")).toBe(String(size));
          expect(mark?.getAttribute("height")).toBe(String(size));
        }
      }
    }
  });

  it("inlines licensed marks so currentColor resolves against the page", () => {
    // Loaded through <img src> an SVG is an isolated document: fill="currentColor"
    // cannot see this page's colour and resolved to black, leaving the marks at
    // roughly 1.2:1 against the dark theme's #2c2c2e panels. Inlining is what
    // lets them inherit the colour the Lucide marks beside them already use.
    const darkIconFg = "rgb(209, 209, 214)";
    for (const id of AGENT_ICON_IDS.filter((value) => agentMarkKind(value) === "asset")) {
      const { container } = render(
        <div style={{ color: darkIconFg }}>
          <AgentIcon agentId={id} />
        </div>,
      );
      const mark = container.querySelector('[data-mark-kind="asset"]')!;
      expect(mark.tagName, id).not.toBe("IMG");
      const svg = mark.querySelector("svg");
      expect(svg, `${id} should inline its svg`).toBeTruthy();
      expect(svg!.getAttribute("fill"), id).toBe("currentColor");
      expect(getComputedStyle(svg!).color, id).toBe(darkIconFg);
    }
  });

  it("keeps the published geometry of every licensed mark", () => {
    // The compliance note in agents.tsx states the marks are not re-drawn.
    // The viewBox is the check: inlining must not rescale or crop the artwork.
    for (const id of AGENT_ICON_IDS.filter((value) => agentMarkKind(value) === "asset")) {
      const { container } = render(<AgentIcon agentId={id} />);
      const svg = container.querySelector('[data-mark-kind="asset"] svg');
      expect(svg!.getAttribute("viewBox"), id).toBe("0 0 24 24");
    }
  });

  it("offers a tagline for hover, distinct from the name", () => {
    for (const id of ALL) {
      const tagline = agentTagline(id);
      expect(tagline.length).toBeGreaterThan(0);
      expect(tagline).not.toBe(id);
    }
    expect(agentTagline("brand-new-agent")).toBe("");
  });
});
