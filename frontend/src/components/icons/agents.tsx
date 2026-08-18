/**
 * Agent marks used to identify rows in BootAgent.
 *
 * Five image assets are shipped because their exact source, MIT license text,
 * copyright owner, and SHA-256 are tracked in asset-rights.json. An Agent with
 * no such basis uses a generic Lucide mark instead, which identifies a row
 * without copying a vendor favicon into the release.
 *
 * Trademark note: the labels identify which Agent a row refers to. The generic
 * marks are BootAgent UI symbols, not vendor artwork. Every mark is a
 * single-colour glyph painted with fill="currentColor", so it takes the
 * surrounding text colour rather than carrying a brand colour -- that is what
 * makes the set legible on both themes.
 *
 * Four of the five are the vendors' own artwork, redistributed with their
 * published geometry unchanged and not re-drawn. OpenClaw is the exception on
 * both counts: no official vector exists, so the mark is the lobster the
 * cc-switch project drew, and BootAgent recoloured it from a red gradient to a
 * single currentColor glyph to match the set. Its geometry is still unchanged.
 * asset-rights.json carries `modified: true` and the specifics, because MIT
 * requires a modified copy to travel with its licence and state the change.
 */
import {
  Bot,
  Braces,
  Briefcase,
  GitBranch,
  type LucideIcon
} from "lucide-react";

import { sourceTranslate, type Translate, type TranslationKey } from "../../i18n";

import assetRightsManifest from "./asset-rights.json";
import claudeCodeMark from "./assets/claude-code.svg?raw";
import codexMark from "./assets/codex.svg?raw";
import deepseekMark from "./assets/deepseek.svg?raw";
import hermesMark from "./assets/hermes.svg?raw";
import kiloCliMark from "./assets/kilo-cli.svg?raw";
import kimiCodeMark from "./assets/kimi-code.svg?raw";
import openclawMark from "./assets/openclaw.svg?raw";
import opencodeMark from "./assets/opencode.svg?raw";
import piMark from "./assets/pi.svg?raw";
// Raster, so imported as a URL rather than inlined. Neither vendor publishes a
// vector, and their icons draw the logo in colour on an opaque rounded plate --
// the alpha channel is just that plate, so a CSS mask would render both as
// identical blank squares. Colour is therefore kept.
import workbuddyRaster from "./assets/workbuddy.png";
import zcodeRaster from "./assets/zcode.png";

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
 * A vendor icon shipped as a bitmap. Separate from AssetMark because it carries
 * no rights record and cannot take the theme colour: it renders in the vendor's
 * own colours, which is the point of using it.
 */
type RasterMark = { kind: "raster"; src: string; source: string };
type Mark = AssetMark | GenericMark | RasterMark;

const GENERIC_SOURCE = "lucide-react@1.25.0 (ISC)";
// Provenance for the two raster marks, recorded here rather than in
// asset-rights.json: that manifest is the licence-bearing set, and these are
// vendor trademarks with no licence to point at.
const WORKBUDDY_MARK_SOURCE = "WorkBuddy.app/Contents/Resources/icon.icns (Tencent, trademark)";
const ZCODE_MARK_SOURCE = "ZCode.app/Contents/Resources/icon.png (Z.ai, trademark)";

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
  "claude-desktop": {
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
  // The same drawing lobe-icons publishes as nousresearch.svg -- byte-identical
  // but for the <title> -- which is right: Hermes Agent is Nous Research's
  // product, so the company mark identifies it.
  hermes: {
    kind: "asset",
    markup: hermesMark,
    source: assetRightsManifest.assets.hermes.source,
    rights: assetRightsManifest.assets.hermes,
  },
  // Aider has no mark in lobe-icons and no vector to extract, so it keeps a
  // generic symbol.
  aider: { kind: "generic", Icon: GitBranch, source: GENERIC_SOURCE },
  // DeepSeek's whale, the company mark rather than a Harness-specific one: the
  // project ships no mark of its own, and the whale is what identifies whose
  // product this is. Same lobe-icons basis as the six above, so it carries a
  // licence and a hash rather than being a favicon copied off a website.
  dsh: {
    kind: "asset",
    markup: deepseekMark,
    source: assetRightsManifest.assets.dsh.source,
    rights: assetRightsManifest.assets.dsh,
  },
  // DSH Desktop reuses the CLI Harness mark: both drive DeepSeek, and the whale
  // is what identifies whose model is behind them. Keyed by desktop Agent id
  // because the desktop card looks itself up by id, as with chatgpt-desktop
  // above -- without this entry the card fell back to the generic Bot glyph.
  //
  // Unlike chatgpt-desktop, this is not the vendor's own app: anywhere-labs
  // publishes it. The mark still says DeepSeek because that is the model it
  // talks to, so the Definition carries Unofficial and the UI states the
  // publisher rather than letting the mark imply it.
  "dsh-desktop": {
    kind: "asset",
    markup: deepseekMark,
    source: assetRightsManifest.assets.dsh.source,
    rights: assetRightsManifest.assets.dsh,
  },
  // Pi's own mark. lobe-icons also carries inflection.svg for Inflection's
  // Pi.ai, a different product with a different drawing -- this is the blocked
  // "pi" wordmark, checked against the logo Pi's own README links to.
  pi: {
    kind: "asset",
    markup: piMark,
    source: assetRightsManifest.assets.pi.source,
    rights: assetRightsManifest.assets.pi,
  },
  // WorkBuddy and ZCode use the vendors' own icons, taken from the installed
  // application bundles. Both ship raster only, so they are the two marks in this
  // set that keep their brand colours instead of taking the theme's. A generic
  // glyph read as "not configured yet" next to the real marks around it, which is
  // the reason for the swap.
  //
  // These two carry no asset-rights entry: the icons are vendor trademarks used
  // to identify which product a row refers to, not redistributable artwork with a
  // licence to record. generate_third_party_licenses.py skips them for the same
  // reason, so NOTICE makes no claim about them either.
  workbuddy: { kind: "raster", src: workbuddyRaster, source: WORKBUDDY_MARK_SOURCE },
  // The international build is the same product, so it carries the same mark; the
  // EditionTag next to the name is what tells the two rows apart.
  "workbuddy-intl": { kind: "raster", src: workbuddyRaster, source: WORKBUDDY_MARK_SOURCE },
  zcode: { kind: "raster", src: zcodeRaster, source: ZCODE_MARK_SOURCE },
  // Unlike the four above, this mark is not the vendor's own artwork: it is the
  // lobster the cc-switch project drew, MIT, and BootAgent recoloured it to a
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
  // lobe-icons publishes this as kimi.svg, the Kimi product mark rather than a
  // Moonshot company mark -- which is the right one here, since the Agent is
  // Kimi Code. Unmodified, so no `modified` claim is recorded.
  "kimi-code": {
    kind: "asset",
    markup: kimiCodeMark,
    source: assetRightsManifest.assets["kimi-code"].source,
    rights: assetRightsManifest.assets["kimi-code"],
  },
};

/** One-line positioning shown on hover; never a restatement of the name. */
const TAGLINES: Record<string, TranslationKey> = {
  codex: "OpenAI 的终端编码代理",
  "claude-code": "Anthropic 的终端编码代理",
  opencode: "开源终端编码代理",
  "kilo-cli": "多模型编排的命令行代理",
  aider: "结对编程式的仓库编辑代理",
  hermes: "可扩展的多渠道智能代理",
  openclaw: "把聊天工具接到编码代理的自建网关",
  "kimi-code": "月之暗面的终端编码代理",
  dsh: "DeepSeek 的插件式本地 Web 代理",
  pi: "可扩展的终端编码代理",
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

/**
 * Whether an Agent uses a licensed vector asset, a vendor raster, a generic
 * mark, or the fallback.
 */
export function agentMarkKind(agentId: string): "asset" | "raster" | "generic" | "fallback" {
  return MARKS[agentId]?.kind ?? "fallback";
}

/** Auditable rights for an image asset; generic marks intentionally return null. */
export function agentMarkRights(agentId: string): AssetRights | null {
  const mark = MARKS[agentId];
  return mark?.kind === "asset" ? mark.rights : null;
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
  if (mark.kind === "raster") {
    // A vendor icon in its own colours. The rounded plate the vendors draw is
    // part of the artwork, so it is clipped to a matching radius rather than
    // sitting as a square on the transparent icon box.
    return (
      <img
        src={mark.src}
        width={size}
        height={size}
        className="agent-mark-raster"
        data-mark-kind="raster"
        alt=""
        aria-hidden="true"
        draggable={false}
      />
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
