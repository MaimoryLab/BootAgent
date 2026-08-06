/**
 * Agent marks used to identify rows in OneAgent.
 *
 * Image assets are shipped only where their exact source, MIT license text,
 * copyright owner, and SHA-256 are tracked in asset-rights.json. An Agent with
 * no such basis uses a generic Lucide mark instead, which identifies a row
 * without copying a vendor favicon into the release.
 *
 * Trademark note: the labels identify which Agent a row refers to. The generic
 * marks are OneAgent UI symbols, not vendor artwork.
 *
 * Most marks are single-colour glyphs painted with fill="currentColor", so they
 * take the surrounding text colour rather than carrying a brand colour -- that is
 * what makes the set legible on both themes. Hermes is the one exception: no
 * vector is published for it, so its mark is a bitmap and cannot be recoloured.
 * It renders on a fixed plate instead, which is why `bitmap` is a distinct mark
 * kind rather than a variant of `asset`.
 *
 * Two marks are not untouched vendor vectors, and asset-rights.json carries
 * `modified: true` plus the specifics for both, because MIT requires a modified
 * copy to travel with its licence and state the change:
 *
 * - OpenClaw: no official vector exists, so the mark is the lobster the
 *   cc-switch project drew, recoloured from a red gradient to a single
 *   currentColor glyph. Geometry unchanged.
 * - Hermes: Nous Research's own PNG, downscaled to a 64px square. Pixels
 *   otherwise untouched -- no recolour, no crop.
 */
import {
  Bot,
  GitBranch,
  type LucideIcon
} from "lucide-react";

import { sourceTranslate, type Translate, type TranslationKey } from "../../i18n";

import assetRightsManifest from "./asset-rights.json";
import claudeCodeMark from "./assets/claude-code.svg?raw";
import codexMark from "./assets/codex.svg?raw";
import hermesMark from "./assets/hermes.png";
import kiloCliMark from "./assets/kilo-cli.svg?raw";
import openclawMark from "./assets/openclaw.svg?raw";
import opencodeMark from "./assets/opencode.svg?raw";

/**
 * The union of every manifest entry, widened so the two fields that exist only
 * on a modified asset are readable without narrowing at each use. They stay
 * optional: `modified` present and true is the claim, and its absence is the
 * claim that the artwork is untouched -- a distinction agents.test.tsx asserts
 * in both directions.
 */
type AssetRights = (typeof assetRightsManifest.assets)[keyof typeof assetRightsManifest.assets] & {
  modified?: boolean;
  modificationNote?: string;
};
type AssetMark = { kind: "asset"; markup: string; source: string; rights: AssetRights };
type GenericMark = { kind: "generic"; Icon: LucideIcon; source: string };
/**
 * A raster mark. Distinct from AssetMark because the two cannot be styled the
 * same way: an inlined SVG paints with currentColor and adapts to the theme,
 * while a bitmap carries its own pixels and cannot be recoloured. This one is
 * 97% opaque and roughly half dark, half light, so it needs an opaque plate to
 * sit on rather than inheriting the panel behind it.
 *
 * Kept a separate kind rather than widened into AssetMark so the compliance
 * tests can tell "vector, theme-adaptive" from "bitmap, plated" without
 * inspecting the markup.
 */
type BitmapMark = { kind: "bitmap"; src: string; source: string; rights: AssetRights };
type Mark = AssetMark | GenericMark | BitmapMark;

const GENERIC_SOURCE = "lucide-react@1.25.0 (ISC)";

const MARKS: Record<string, Mark> = {
  codex: {
    kind: "asset",
    markup: codexMark,
    source: assetRightsManifest.assets.codex.source,
    rights: assetRightsManifest.assets.codex,
  },
  opencode: {
    kind: "asset",
    markup: opencodeMark,
    source: assetRightsManifest.assets.opencode.source,
    rights: assetRightsManifest.assets.opencode,
  },
  "claude-code": {
    kind: "asset",
    markup: claudeCodeMark,
    source: assetRightsManifest.assets["claude-code"].source,
    rights: assetRightsManifest.assets["claude-code"],
  },
  "kilo-cli": {
    kind: "asset",
    markup: kiloCliMark,
    source: assetRightsManifest.assets["kilo-cli"].source,
    rights: assetRightsManifest.assets["kilo-cli"],
  },
  // ChatGPT Desktop is OpenAI's own product and shares Codex's configuration, so
  // it reuses the same OpenAI mark rather than registering a second copy of one
  // asset. Keyed by desktop Agent id because the desktop card looks itself up by
  // id; passing a literal here is what put this mark on WorkBuddy.
  "chatgpt-desktop": {
    kind: "asset",
    markup: codexMark,
    source: assetRightsManifest.assets.codex.source,
    rights: assetRightsManifest.assets.codex,
  },
  // The only raster mark in the set, because no vector exists: logo.svg is 404 at
  // both the vendor site and the repo, and no icon collection carries Hermes. It
  // is Nous Research's own artwork under their own MIT, which is a cleaner basis
  // than the undocumented copy in cc-switch's extracted/ directory.
  //
  // Being a bitmap it cannot adapt to the theme, so AgentIcon plates it. The
  // downscale from the published 1772x1799 is recorded in asset-rights.json.
  hermes: {
    kind: "bitmap",
    src: hermesMark,
    source: assetRightsManifest.assets.hermes.source,
    rights: assetRightsManifest.assets.hermes,
  },
  // Aider has no mark in lobe-icons, so it keeps a generic symbol rather than a
  // vendor favicon copied in without an auditable redistribution basis.
  aider: { kind: "generic", Icon: GitBranch, source: GENERIC_SOURCE },
  // Unlike the four above, this mark is not the vendor's own artwork: it is the
  // lobster the cc-switch project drew, MIT, and OneAgent recoloured it to a
  // single currentColor glyph so it adapts to the theme like the rest of the set.
  // asset-rights.json records both facts, because MIT requires a modified copy to
  // carry its licence and state the change. lobe-icons has no OpenClaw mark, and
  // no official vector exists to prefer instead.
  openclaw: {
    kind: "asset",
    markup: openclawMark,
    source: assetRightsManifest.assets.openclaw.source,
    rights: assetRightsManifest.assets.openclaw,
  },
};

/** One-line positioning shown on hover; never a restatement of the name. */
const TAGLINES: Record<string, TranslationKey> = {
  codex: "OpenAI 的终端编码代理",
  "claude-code": "Anthropic 的终端编码代理",
  opencode: "开源终端编码代理",
  "kilo-cli": "多模型编排的命令行代理",
  aider: "结对编程式的仓库编辑代理",
  openclaw: "把聊天工具接到编码代理的自建网关",
  hermes: "从使用经验里自建技能的终端代理",
};

export const AGENT_ICON_IDS = Object.keys(MARKS);

export function agentTagline(agentId: string, t: Translate = sourceTranslate): string {
  const tagline = TAGLINES[agentId];
  return tagline ? t(tagline) : "";
}

/** The source URL or package provenance displayed in reference notes/tests. */
export function agentMarkSource(agentId: string): string {
  return MARKS[agentId]?.source ?? "";
}

/** Whether an Agent uses a licensed vector, a bitmap, a generic mark, or fallback. */
export function agentMarkKind(agentId: string): "asset" | "bitmap" | "generic" | "fallback" {
  return MARKS[agentId]?.kind ?? "fallback";
}

/**
 * Auditable rights for a redistributed image, vector or bitmap. Generic marks
 * return null: a Lucide glyph travels with lucide-react's own licence and is not
 * vendor artwork, so it has nothing to record here.
 */
export function agentMarkRights(agentId: string): AssetRights | null {
  const mark = MARKS[agentId];
  return mark?.kind === "asset" || mark?.kind === "bitmap" ? mark.rights : null;
}

export function AgentIcon({ agentId, size = 18 }: { agentId: string; size?: number }) {
  const mark = MARKS[agentId];
  if (!mark) {
    // An Agent added to agents.lock.json before its mark is assigned still
    // renders rather than leaving a blank square.
    return (
      <Bot
        width={size}
        height={size}
        strokeWidth={1.8}
        data-mark-kind="fallback"
        aria-hidden="true"
      />
    );
  }
  if (mark.kind === "generic") {
    const Icon = mark.Icon;
    return (
      <Icon
        width={size}
        height={size}
        strokeWidth={1.8}
        data-mark-kind="generic"
        aria-hidden="true"
      />
    );
  }
  if (mark.kind === "bitmap") {
    // A plate, not decoration. The artwork is ~97% opaque and split about evenly
    // between near-black and near-white, so on the dark theme its light half
    // reads as a glowing tile and on the light theme its dark half does the
    // same. A fixed white plate gives it one consistent background on both
    // themes -- the mark then looks like a product tile rather than a mark that
    // renders differently per theme.
    //
    // The plate is deliberately not themed: inverting it would change the
    // artwork's apparent colours, which is the modification we are not making.
    return (
      <span
        className="agent-mark agent-mark-plated"
        style={{ width: size, height: size }}
        aria-hidden="true"
        data-mark-kind="bitmap"
      >
        <img src={mark.src} alt="" width={size} height={size} />
      </span>
    );
  }
  // Inlined rather than loaded through <img src>. Each asset paints with
  // fill="currentColor", and an SVG fetched by <img> is an isolated document
  // where currentColor cannot see this page's colour -- it resolved to black, so
  // the marks were near-invisible on the dark theme's #2c2c2e panels. Inline,
  // they inherit the same colour the Lucide marks beside them already use.
  //
  // The markup is bundled at build time from files in this repository, so it is
  // not runtime input. It is still constrained: only these five imports reach
  // here, and the width/height below override the assets' own 1em sizing.
  return (
    <span
      className="agent-mark"
      style={{ width: size, height: size }}
      aria-hidden="true"
      data-mark-kind="asset"
      dangerouslySetInnerHTML={{ __html: mark.markup }}
    />
  );
}
