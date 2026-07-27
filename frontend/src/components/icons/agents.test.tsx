import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AGENT_ICON_IDS, AgentIcon, agentTagline } from "./agents";

const AUTO_AGENTS = ["codex", "claude-code", "opencode", "kilo-cli", "aider"];

describe("AgentIcon", () => {
  it("has a distinct icon for every one-click Agent", () => {
    // The old UI keyed icons off the catalog group, so all five auto Agents
    // rendered the same glyph and none was recognisable at a glance.
    for (const id of AUTO_AGENTS) {
      expect(AGENT_ICON_IDS).toContain(id);
    }
    const rendered = AUTO_AGENTS.map((id) => {
      const { container } = render(<AgentIcon agentId={id} />);
      return container.querySelector("svg")?.innerHTML ?? "";
    });
    expect(new Set(rendered).size).toBe(AUTO_AGENTS.length);
  });

  it("falls back rather than rendering nothing for an unknown Agent", () => {
    // A new Agent in agents.lock.json must not leave a blank square.
    const { container } = render(<AgentIcon agentId="brand-new-agent" />);
    const svg = container.querySelector("svg");
    expect(svg).not.toBeNull();
    expect(svg?.innerHTML.length).toBeGreaterThan(0);
  });

  it("keeps the glyph out of the accessibility tree", () => {
    // The Agent name sits next to it, so announcing the icon would duplicate.
    const { container } = render(<AgentIcon agentId="codex" />);
    expect(container.querySelector("svg")?.getAttribute("aria-hidden")).toBe("true");
  });

  it("renders at one size and inherits colour", () => {
    // Uniform box and currentColor are what make icons from different sources
    // read as one set — and avoid recolouring third-party marks.
    const { container } = render(<AgentIcon agentId="claude-code" size={20} />);
    const svg = container.querySelector("svg");
    expect(svg?.getAttribute("viewBox")).toBe("0 0 24 24");
    expect(svg?.getAttribute("width")).toBe("20");
    expect(svg?.outerHTML).not.toMatch(/#[0-9a-f]{3,6}/i);
  });

  it("keeps the interior cutouts of a filled mark", () => {
    // The OpenAI mark is a single path whose knot relies on even-odd winding.
    // Without it the glyph fills into a solid blob and stops being recognisable.
    const { container } = render(<AgentIcon agentId="codex" />);
    expect(container.querySelector("svg")?.getAttribute("fill-rule")).toBe("evenodd");
  });

  it("offers a tagline for hover, distinct from the name", () => {
    for (const id of AUTO_AGENTS) {
      const tagline = agentTagline(id);
      expect(tagline.length).toBeGreaterThan(0);
      expect(tagline).not.toBe(id);
    }
    expect(agentTagline("brand-new-agent")).toBe("");
  });
});
