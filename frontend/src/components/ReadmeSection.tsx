import DOMPurify from "dompurify";
import { marked } from "marked";
import { type MouseEvent, useEffect, useState } from "react";

import { api } from "../backend/api";
import { useI18n } from "../i18n";

type FetchState = "idle" | "loading" | "success" | "error";

/**
 * Fetches a raw Markdown file and renders it as sanitised HTML.
 *
 * External content is treated as untrusted data: DOMPurify strips all
 * event handlers, javascript: hrefs, and dangerous tags before insertion.
 * Any text inside the README that looks like instructions is inert HTML,
 * not a prompt to this component.
 *
 * Relative image hrefs from GitHub READMEs are rewritten to absolute URLs
 * so they resolve when rendered outside their repo context.
 */
type ReadmeSectionProps =
  | { readmeUrl: string; skillhubSlug?: never }
  | { readmeUrl?: never; skillhubSlug: string };

function stripMarkdownFrontMatter(markdown: string): string {
  const content = markdown.startsWith("\uFEFF") ? markdown.slice(1) : markdown;
  const firstLineEnd = content.indexOf("\n");
  if (firstLineEnd < 0 || content.slice(0, firstLineEnd).replace(/\r$/, "").trimEnd() !== "---") {
    return content;
  }

  let lineStart = firstLineEnd + 1;
  while (lineStart <= content.length) {
    const lineEnd = content.indexOf("\n", lineStart);
    const end = lineEnd < 0 ? content.length : lineEnd;
    const line = content.slice(lineStart, end).replace(/\r$/, "").trimEnd();
    if (line === "---") return lineEnd < 0 ? "" : content.slice(lineEnd + 1);
    if (lineEnd < 0) break;
    lineStart = lineEnd + 1;
  }

  return content;
}

export function ReadmeSection({ readmeUrl, skillhubSlug }: ReadmeSectionProps) {
  const { t } = useI18n();
  const [state, setState] = useState<FetchState>("idle");
  const [html, setHtml] = useState("");

  useEffect(() => {
    let cancelled = false;
    setState("loading");
    setHtml("");

    const loadSkillhubFile = async (slug: string) => {
      try {
        return await api.marketplaceSkillFile(slug);
      } catch {
        const endpoint = `https://api.skillhub.cn/api/v1/skills/${encodeURIComponent(slug)}/file?path=SKILL.md`;
        const response = await fetch(endpoint);
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        return response.text();
      }
    };

    const load = skillhubSlug
      ? loadSkillhubFile(skillhubSlug)
      : readmeUrl
        ? fetch(readmeUrl).then((res) => {
          if (!res.ok) throw new Error(`HTTP ${res.status}`);
          return res.text();
          })
        : Promise.reject(new Error("README source is missing"));

    load
      .then((md) => {
        if (cancelled) return;

        const markdown = skillhubSlug ? stripMarkdownFrontMatter(md) : md;
        const rawHtml = marked.parse(markdown, { async: false }) as string;
        const clean = DOMPurify.sanitize(rawHtml, {
          USE_PROFILES: { html: true },
          FORBID_TAGS: ["script", "style", "iframe", "form", "input", "button"],
          FORBID_ATTR: ["onerror", "onload", "onclick", "onmouseover"],
        });
        const document = new DOMParser().parseFromString(clean, "text/html");
        const resolve = (value: string) => {
          if (/^(?:https?:|#)/i.test(value)) return value;
          if (skillhubSlug) {
            const file = value.replace(/^\.\//, "");
            return `https://api.skillhub.cn/api/v1/skills/${encodeURIComponent(skillhubSlug)}/file?path=${encodeURIComponent(file)}`;
          }
          return new URL(value, readmeUrl).href;
        };
        document.querySelectorAll<HTMLAnchorElement>("a[href]").forEach((anchor) => {
          anchor.href = resolve(anchor.getAttribute("href") ?? "");
        });
        document.querySelectorAll<HTMLImageElement>("img[src]").forEach((image) => {
          image.src = resolve(image.getAttribute("src") ?? "");
        });
        setHtml(document.body.innerHTML);
        setState("success");
      })
      .catch(() => {
        if (!cancelled) setState("error");
      });

    return () => { cancelled = true; };
  }, [readmeUrl, skillhubSlug]);

  const openLink = (event: MouseEvent<HTMLDivElement>) => {
    const target = event.target instanceof Element ? event.target.closest<HTMLAnchorElement>("a[href]") : null;
    if (!target || target.getAttribute("href")?.startsWith("#")) return;
    event.preventDefault();
    void api.openMarketplaceExternal(target.href).catch(() => undefined);
  };

  if (state === "idle" || state === "loading") {
    return (
      <div className="readme-loading" role="status" aria-live="polite">
        <span className="spinner" aria-hidden="true" />
        {t("正在加载 README")}
      </div>
    );
  }

  if (state === "error") {
    return (
      <p className="readme-error" role="alert">
        {t("README 加载失败，请检查网络连接。")}
      </p>
    );
  }

  return (
    <div
      className="readme-body"
      onClick={openLink}
      // Content is sanitised by DOMPurify before insertion.
      // eslint-disable-next-line react/no-danger
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
