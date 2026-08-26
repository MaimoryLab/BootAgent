import { describe, expect, it } from "vitest";

import { sourceTranslate, translate } from "../i18n";
import { converterProfileName } from "./conversion";

describe("converterProfileName", () => {
  it("names every generated adapter Profile by the Agent that uses it", () => {
    expect(converterProfileName("bootagent-converter-anthropic", "stored", sourceTranslate)).toBe("协议适配（Claude Code）");
    expect(converterProfileName("bootagent-converter-responses", "stored", sourceTranslate)).toBe("协议适配（Codex）");
    expect(converterProfileName("bootagent-converter-chat", "stored", sourceTranslate)).toBe("协议适配（OpenCode / Aider）");
  });

  it("supports legacy IDs and preserves unknown names", () => {
    expect(converterProfileName("bootagent_converter_responses", "stored", sourceTranslate)).toBe("协议适配（Codex）");
    expect(converterProfileName("team-profile", "团队配置", sourceTranslate)).toBe("团队配置");
  });

  it("uses the matching English display names without changing stored labels", () => {
    const english = (key: Parameters<typeof translate>[1]) => translate("en", key);
    expect(converterProfileName("bootagent-converter-anthropic", "stored", english)).toBe("Protocol adapter (Claude Code)");
    expect(converterProfileName("bootagent-converter-responses", "stored", english)).toBe("Protocol adapter (Codex)");
  });
});
