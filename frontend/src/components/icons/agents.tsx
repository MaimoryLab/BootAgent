/**
 * Agent marks used to identify rows in OneAgent.
 *
 * Three image assets remain because their exact source, MIT license text,
 * copyright owner, and SHA-256 are tracked in asset-rights.json. Agents whose
 * published artwork does not have an auditable redistribution basis use generic
 * Lucide marks instead; those marks identify a row without copying a vendor
 * favicon into the release.
 *
 * Trademark note: the labels identify which Agent a row refers to. The generic
 * marks are OneAgent UI symbols, not vendor artwork. The three licensed image
 * marks are not recoloured or restyled.
 */
import {
  Blocks,
  Bot,
  Braces,
  GitBranch,
  MousePointer2,
  type LucideIcon
} from "lucide-react";

import { sourceTranslate, type Translate, type TranslationKey } from "../../i18n";

import assetRightsManifest from "./asset-rights.json";
import codexMark from "./assets/codex.svg";
import opencodeMark from "./assets/opencode.svg";

type AssetRights = (typeof assetRightsManifest.assets)[keyof typeof assetRightsManifest.assets];
type AssetMark = { kind: "asset"; src: string; source: string; rights: AssetRights };
type GenericMark = { kind: "generic"; Icon: LucideIcon; source: string };
type Mark = AssetMark | GenericMark;

const GENERIC_SOURCE = "lucide-react@1.25.0 (ISC)";

const MARKS: Record<string, Mark> = {
  codex: {
    kind: "asset",
    src: codexMark,
    source: assetRightsManifest.assets.codex.source,
    rights: assetRightsManifest.assets.codex,
  },
  opencode: {
    kind: "asset",
    src: opencodeMark,
    source: assetRightsManifest.assets.opencode.source,
    rights: assetRightsManifest.assets.opencode,
  },
  "claude-code": { kind: "generic", Icon: Braces, source: GENERIC_SOURCE },
  cursor: { kind: "generic", Icon: MousePointer2, source: GENERIC_SOURCE },
  "kilo-cli": { kind: "generic", Icon: Blocks, source: GENERIC_SOURCE },
  aider: { kind: "generic", Icon: GitBranch, source: GENERIC_SOURCE },
};

/** One-line positioning shown on hover; never a restatement of the name. */
const TAGLINES: Record<string, TranslationKey> = {
  codex: "OpenAI 的终端编码代理",
  "claude-code": "Anthropic 的终端编码代理",
  opencode: "开源终端编码代理",
  "kilo-cli": "多模型编排的命令行代理",
  aider: "结对编程式的仓库编辑代理",
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
  return (
    <img
      className="agent-mark"
      src={mark.src}
      width={size}
      height={size}
      alt=""
      aria-hidden="true"
      data-mark-kind="asset"
      // contain keeps the three licensed assets inside the same square box.
      style={{ objectFit: "contain" }}
      draggable={false}
    />
  );
}
