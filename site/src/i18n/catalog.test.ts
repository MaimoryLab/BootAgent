import { describe, expect, it } from "vitest";

import { catalog } from "../lib/catalog";
import { locales, switchesLanguage, translatedRoutes, type Locale } from "./index";
import { useCatalogLabels } from "./catalog";


// Every enum the catalog can emit needs a label in every locale. A missing entry
// falls back to the raw id, which reads as the wrong language rather than as a
// bug — the Chinese `gateway` label shipped as the English "Gateway Agent" that
// way, and nothing failed.
describe("catalogue labels cover every locale", () => {
  const cjk = /[一-鿿]/;

  for (const locale of locales) {
    describe(locale, () => {
      const labels = useCatalogLabels(locale);

      it("labels every group the catalog uses", () => {
        const used = [...new Set(catalog.agents.map((agent) => agent.group))].sort();
        for (const group of used) {
          expect(labels.groupLabels[group], `${locale} is missing group "${group}"`).toBeTruthy();
        }
      });

      it("labels every protocol status the providers publish", () => {
        const used = [...new Set(catalog.providers.flatMap((p) => p.protocols.map((entry) => entry.status)))].sort();
        for (const status of used) {
          expect(labels.protocolStatusLabels[status], `${locale} is missing status "${status}"`).toBeTruthy();
        }
      });

      it("writes group labels in the locale's own language", () => {
        for (const [group, label] of Object.entries(labels.groupLabels)) {
          // Proper nouns are allowed to stay Latin, so the check is directional:
          // a Chinese label must contain some Chinese, and an English one none.
          if (locale === "zh-CN") {
            expect(cjk.test(label), `zh-CN group "${group}" has no Chinese: ${label}`).toBe(true);
          } else {
            expect(cjk.test(label), `en group "${group}" contains Chinese: ${label}`).toBe(false);
          }
        }
      });
    });
  }
});

describe("cross-language link detection", () => {
  it("flags only the routes with no translation", () => {
    for (const route of translatedRoutes) {
      expect(switchesLanguage("en", route), `${route} is translated`).toBe(false);
    }
    for (const route of ["enterprise/", "support/", "agents/", "providers/", "changelog/"]) {
      expect(switchesLanguage("en", route), `${route} has no English page`).toBe(true);
    }
  });

  it("never flags a link for a Chinese reader, since Chinese is the fallback", () => {
    for (const route of [...translatedRoutes, "enterprise/", "agents/"]) {
      expect(switchesLanguage("zh-CN" as Locale, route)).toBe(false);
    }
  });
});
