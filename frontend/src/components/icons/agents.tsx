/**
 * Agent marks used to identify rows in OneAgent.
 *
 * Five image assets are shipped because their exact source, MIT license text,
 * copyright owner, and SHA-256 are tracked in asset-rights.json. Agents whose
 * published artwork does not have an auditable redistribution basis use generic
 * Lucide marks instead; those marks identify a row without copying a vendor
 * favicon into the release.
 *
 * Trademark note: the labels identify which Agent a row refers to. The generic
 * marks are OneAgent UI symbols, not vendor artwork. The licensed marks keep
 * their published geometry unchanged; none is restyled or re-drawn. Each is
 * distributed as a single-colour glyph painted with fill="currentColor", so it
 * takes the surrounding text colour by design rather than carrying a brand
 * colour we would be altering -- that is what makes them legible on both
 * themes.
 */
import {
  Bot,
  GitBranch,
  Waypoints,
  type LucideIcon
} from "lucide-react";

import { sourceTranslate, type Translate, type TranslationKey } from "../../i18n";

import assetRightsManifest from "./asset-rights.json";
import claudeCodeMark from "./assets/claude-code.svg?raw";
import codexMark from "./assets/codex.svg?raw";
import kiloCliMark from "./assets/kilo-cli.svg?raw";
import opencodeMark from "./assets/opencode.svg?raw";

type AssetRights = (typeof assetRightsManifest.assets)[keyof typeof assetRightsManifest.assets];
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
  // OpenClaw's real logo is a lobster, and the only SVG version available is one
  // CC Switch drew itself (see docs/internal/cc-switch-reference-notes.md) -- a
  // third party's redrawing is not the vendor's artwork and gives OneAgent no
  // right to redistribute it. lobe-icons has no OpenClaw mark either, so it takes
  // a generic symbol until an official asset with a licence appears.
  openclaw: { kind: "generic", Icon: Waypoints, source: GENERIC_SOURCE },
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
