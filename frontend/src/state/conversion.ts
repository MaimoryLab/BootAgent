import type { Translate } from "../i18n";

export function isConverterID(id: string): boolean {
  return id.startsWith("bootagent-converter-") || id.startsWith("bootagent_converter_");
}

export function converterProfileName(id: string, fallback: string, t: Translate): string {
  const kind = id.startsWith("bootagent-converter-")
    ? id.slice("bootagent-converter-".length)
    : id.startsWith("bootagent_converter_")
      ? id.slice("bootagent_converter_".length)
      : "";
  switch (kind) {
    case "anthropic": return t("协议适配（Claude Code）");
    case "responses": return t("协议适配（Codex）");
    case "chat": return t("协议适配（OpenCode / Aider）");
    default: return fallback;
  }
}
