/**
 * Agent marks used to identify rows in OneAgent.
 *
 * Five image assets are shipped because their exact source, MIT license text,
 * copyright owner, and SHA-256 are tracked in asset-rights.json. An Agent with
 * no such basis uses a generic Lucide mark instead, which identifies a row
 * without copying a vendor favicon into the release.
 *
 * Trademark note: the labels identify which Agent a row refers to. The generic
 * marks are OneAgent UI symbols, not vendor artwork. Every mark is a
 * single-colour glyph painted with fill="currentColor", so it takes the
 * surrounding text colour rather than carrying a brand colour -- that is what
 * makes the set legible on both themes.
 *
 * Four of the five are the vendors' own artwork, redistributed with their
 * published geometry unchanged and not re-drawn. OpenClaw is the exception on
 * both counts: no official vector exists, so the mark is the lobster the
 * cc-switch project drew, and OneAgent recoloured it from a red gradient to a
 * single currentColor glyph to match the set. Its geometry is still unchanged.
 * asset-rights.json carries `modified: true` and the specifics, because MIT
 * requires a modified copy to travel with its licence and state the change.
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
type Mark = AssetMark | GenericMark;

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

/** Whether an Agent uses a licensed image asset, a generic mark, or fallback. */
export function agentMarkKind(agentId: string): "asset" | "generic" | "fallback" {
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
