/**
 * Language-neutral catalogue labels: these are proper nouns, identical in every
 * locale. Translated labels live in src/i18n/catalog.ts.
 */
export const protocolLabels: Record<string, string> = {
  openai: "OpenAI Chat Completions",
  anthropic: "Anthropic Messages",
  responses: "OpenAI Responses",
};

export const platformLabels: Record<string, string> = {
  macos: "macOS",
  windows: "Windows",
  linux: "Linux",
};
