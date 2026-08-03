import { act, renderHook } from "@testing-library/react";
import type { PropsWithChildren } from "react";
import { describe, expect, it } from "vitest";

import { I18nProvider, LOCALE_STORAGE_KEY, translate, useI18n } from "./i18n";

describe("i18n", () => {
  it("translates placeholders and persists language changes", () => {
    expect(translate("en", "更多 Agent（{count}）", { count: 2 })).toBe("More agents (2)");
    localStorage.setItem(LOCALE_STORAGE_KEY, "en");
    const wrapper = ({ children }: PropsWithChildren) => <I18nProvider>{children}</I18nProvider>;
    const { result } = renderHook(() => useI18n(), { wrapper });

    expect(result.current.t("返回")).toBe("Back");
    expect(document.documentElement.lang).toBe("en");
    act(() => result.current.setLocale("zh-CN"));
    expect(result.current.t("返回")).toBe("返回");
    expect(document.documentElement.lang).toBe("zh-CN");
    expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe("zh-CN");
  });
});
