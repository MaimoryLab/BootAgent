import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AGENT_ICON_IDS, AgentIcon, agentMarkKind, agentMarkRights, agentMarkSource, agentTagline } from "./agents";

const AUTO_AGENTS = ["codex", "claude-code", "opencode", "kilo-cli", "aider"];
// Shown on the first screen alongside them, so they need marks of their own even
// though OneAgent does not configure them.
const PROMINENT_GUIDE_AGENTS = ["cursor", "openclaw", "hermes"];
const ALL = [...AUTO_AGENTS, ...PROMINENT_GUIDE_AGENTS];

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

  it("uses generic Lucide marks for assets without redistribution evidence", () => {
    for (const id of ["claude-code", "cursor", "hermes", "kilo-cli", "aider"]) {
      const { container } = render(<AgentIcon agentId={id} />);
      expect(agentMarkKind(id)).toBe("generic");
      expect(container.querySelector("svg[data-mark-kind=generic]")).not.toBeNull();
      expect(container.querySelector("img")).toBeNull();
      expect(agentMarkRights(id)).toBeNull();
    }
  });

  it("records auditable rights for every redistributed image asset", () => {
    for (const id of ["codex", "opencode", "openclaw"]) {
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
    const img = container.querySelector("img");
    expect(img?.getAttribute("aria-hidden")).toBe("true");
    expect(img?.getAttribute("alt")).toBe("");
  });

  it("renders every mark in the same square box", () => {
    // Uniformity comes from the container, not from restyling the artwork:
    // that is what lets marks from eight different brands read as one set.
    for (const size of [18, 20]) {
      for (const id of ALL) {
        const { container } = render(<AgentIcon agentId={id} size={size} />);
        const mark = container.querySelector<HTMLElement>("img, svg");
        expect(mark?.getAttribute("width")).toBe(String(size));
        expect(mark?.getAttribute("height")).toBe(String(size));
        if (mark?.tagName === "IMG") {
          // contain, so a mark with tighter padding cannot overflow the box.
          expect((mark as HTMLImageElement).style.objectFit).toBe("contain");
        }
      }
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
