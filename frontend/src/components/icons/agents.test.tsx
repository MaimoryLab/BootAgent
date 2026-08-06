import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AGENT_ICON_IDS, AgentIcon, agentMarkKind, agentMarkRights, agentMarkSource, agentTagline } from "./agents";

// Every Agent in agents.lock.json, all of which reach the first screen. Keep in
// step with the catalog: an Agent with no mark falls back to a shared symbol and
// stops being identifiable without reading its label.
const ALL = ["codex", "claude-code", "opencode", "kilo-cli", "aider", "openclaw", "hermes"];

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
    // Every id whose mark is redistributed artwork, vector or bitmap. Filtering
    // on "asset" alone would let a raster mark ship with no recorded rights,
    // which is the same defect this test exists to catch.
    const licensedIds = AGENT_ICON_IDS.filter((id) => agentMarkKind(id) === "asset" || agentMarkKind(id) === "bitmap");
    // chatgpt-desktop is a desktop Agent rather than a CLI, and it reuses the
    // OpenAI mark because it is OpenAI's own product sharing Codex's config.
    expect(licensedIds.sort()).toEqual(["chatgpt-desktop", "claude-code", "codex", "hermes", "kilo-cli", "openclaw", "opencode"]);
    for (const id of licensedIds) {
      const rights = agentMarkRights(id);
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
        const kind = agentMarkKind(id);
        if (kind === "asset" || kind === "bitmap") {
          // The licensed assets declare their own size in em, and the bitmap has
          // intrinsic pixels, so in both cases the wrapper carries the box and
          // CSS fits the artwork to it.
          const mark = container.querySelector<HTMLElement>(`[data-mark-kind="${kind}"]`);
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
    // The compliance note in agents.tsx states the marks are not re-drawn. The
    // viewBox is the check: inlining must not rescale or crop the artwork.
    //
    // Each mark is checked against its own source coordinate system rather than
    // one shared value. The four vendor marks come from lobe-icons at 24x24;
    // OpenClaw's is the cc-switch drawing at 120x120, and normalising it to 24
    // would be the re-drawing this test exists to prevent. The recolouring
    // recorded in asset-rights.json does not touch geometry.
    const PUBLISHED_VIEWBOX: Record<string, string> = {
      codex: "0 0 24 24",
      "chatgpt-desktop": "0 0 24 24",
      opencode: "0 0 24 24",
      "claude-code": "0 0 24 24",
      "kilo-cli": "0 0 24 24",
      openclaw: "0 0 120 120",
    };
    const assetIds = AGENT_ICON_IDS.filter((value) => agentMarkKind(value) === "asset");
    // Guards the map itself: a new asset with no entry would otherwise be skipped
    // rather than reported.
    expect(Object.keys(PUBLISHED_VIEWBOX).sort()).toEqual([...assetIds].sort());
    for (const id of assetIds) {
      const { container } = render(<AgentIcon agentId={id} />);
      const svg = container.querySelector('[data-mark-kind="asset"] svg');
      expect(svg!.getAttribute("viewBox"), id).toBe(PUBLISHED_VIEWBOX[id]);
    }
  });

  it("records every modified asset as modified, and no others", () => {
    // MIT lets a copy be changed, but the change has to be stated. Shipping a
    // changed mark as if it were untouched vendor artwork is the defect this
    // catches -- in both directions, since claiming an unmodified mark was
    // modified is also wrong.
    const MODIFIED: Record<string, RegExp> = {
      // Recoloured from a red gradient to a single currentColor glyph.
      openclaw: /currentColor/,
      // Downscaled from the published 1772x1799 to a 64px square.
      hermes: /[Dd]ownscaled/,
    };
    for (const id of AGENT_ICON_IDS.filter((value) => agentMarkKind(value) === "asset" || agentMarkKind(value) === "bitmap")) {
      const rights = agentMarkRights(id)!;
      const expectedNote = MODIFIED[id];
      if (expectedNote) {
        expect(rights.modified, id).toBe(true);
        expect(rights.modificationNote, id).toMatch(expectedNote);
      } else {
        expect("modified" in rights, `${id} should not claim a modification`).toBe(false);
      }
    }
    // Not the vendor's own artwork, so the owner must not read as OpenClaw's.
    expect(agentMarkRights("openclaw")!.copyrightOwner).toMatch(/cc-switch/);
    // Hermes' mark IS the vendor's own, taken from their repository under their
    // own MIT rather than from a downstream copy with no recorded provenance.
    expect(agentMarkRights("hermes")!.copyrightOwner).toBe("Nous Research");
    expect(agentMarkRights("hermes")!.source).toMatch(/NousResearch\/hermes-agent/);
    // The note must cite the upstream hash, since the shipped file's own hash
    // cannot prove what it was derived from.
    expect(agentMarkRights("hermes")!.modificationNote).toMatch(/2eaff911b9da9b1f1fcc81adb02f4992bb9ea6b781f4dd048cd79349927ddb7a/);
  });

  it("plates the bitmap mark, since it cannot adapt to the theme", () => {
    // The other marks paint with currentColor and sit on a transparent box. This
    // one is ~97% opaque and roughly half near-black, half near-white, so on a
    // transparent box its light half glows on the dark theme and its dark half
    // does the same on the light one. The plate is the fix, and it must not be
    // themed: inverting it would alter the artwork's apparent colours, which is a
    // modification asset-rights.json does not claim.
    const { container } = render(<AgentIcon agentId="hermes" size={20} />);
    const mark = container.querySelector<HTMLElement>('[data-mark-kind="bitmap"]');
    expect(mark).not.toBeNull();
    expect(mark!.classList.contains("agent-mark-plated")).toBe(true);
    expect(mark!.style.width).toBe("20px");
    expect(mark!.style.height).toBe("20px");
    const image = mark!.querySelector("img");
    expect(image).not.toBeNull();
    // Decorative: the Agent name sits beside it, so it must not be announced.
    expect(mark!.getAttribute("aria-hidden")).toBe("true");
    expect(image!.getAttribute("alt")).toBe("");
    // No vector mark may be plated -- they would lose their transparency.
    for (const id of AGENT_ICON_IDS.filter((value) => agentMarkKind(value) === "asset")) {
      const { container: vector } = render(<AgentIcon agentId={id} />);
      expect(vector.querySelector(".agent-mark-plated"), id).toBeNull();
    }
  });

  it("gives a desktop Agent its own mark rather than another vendor's", () => {
    // The desktop card used to pass a literal agentId="codex" for every desktop
    // Agent, so WorkBuddy -- a Tencent product -- rendered OpenAI's mark. Reusing
    // one vendor's artwork for another vendor's product is a trademark problem,
    // not a cosmetic one, so each case is asserted separately.
    //
    // ChatGPT Desktop is the one legitimate reuse: it is OpenAI's own app and
    // shares Codex's configuration, so it renders the same OpenAI mark.
    const { container: chatgpt } = render(<AgentIcon agentId="chatgpt-desktop" />);
    const { container: codex } = render(<AgentIcon agentId="codex" />);
    expect(chatgpt.innerHTML).toBe(codex.innerHTML);

    // WorkBuddy has no licensed mark, so it must fall back to the generic symbol
    // and must not borrow one that belongs to somebody else.
    const { container: workbuddy } = render(<AgentIcon agentId="workbuddy" />);
    expect(agentMarkKind("workbuddy")).toBe("fallback");
    expect(workbuddy.querySelector('[data-mark-kind="fallback"]')).not.toBeNull();
    expect(workbuddy.innerHTML).not.toBe(codex.innerHTML);
    for (const id of AGENT_ICON_IDS.filter((value) => agentMarkKind(value) === "asset")) {
      const { container } = render(<AgentIcon agentId={id} />);
      expect(workbuddy.innerHTML, `workbuddy must not reuse the ${id} mark`).not.toBe(container.innerHTML);
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
