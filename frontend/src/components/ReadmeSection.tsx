import DOMPurify from "dompurify";
import { marked } from "marked";
import { useEffect, useState } from "react";

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
export function ReadmeSection({ readmeUrl }: { readmeUrl: string }) {
  const { t } = useI18n();
  const [state, setState] = useState<FetchState>("idle");
  const [html, setHtml] = useState("");

  useEffect(() => {
    let cancelled = false;
    setState("loading");
    setHtml("");

    fetch(readmeUrl)
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.text();
      })
      .then((md) => {
        if (cancelled) return;

        // Rewrite relative image src to absolute so GitHub images render.
        // raw.githubusercontent.com URLs have the form:
        // https://raw.githubusercontent.com/{owner}/{repo}/refs/heads/{branch}/path/README.md
        // We strip the filename to get the base directory.
        const base = readmeUrl.replace(/\/[^/]+$/, "/");
        const withAbsImages = md.replace(
          /!\[([^\]]*)\]\((?!https?:\/\/)([^)]+)\)/g,
          (_, alt: string, src: string) => `![${alt}](${base}${src})`,
        );

        const rawHtml = marked.parse(withAbsImages, { async: false }) as string;
        const clean = DOMPurify.sanitize(rawHtml, {
          USE_PROFILES: { html: true },
          FORBID_TAGS: ["script", "style", "iframe", "form", "input", "button"],
          FORBID_ATTR: ["onerror", "onload", "onclick", "onmouseover"],
        });
        setHtml(clean);
        setState("success");
      })
      .catch(() => {
        if (!cancelled) setState("error");
      });

    return () => { cancelled = true; };
  }, [readmeUrl]);

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
      // Content is sanitised by DOMPurify before insertion.
      // eslint-disable-next-line react/no-danger
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}
