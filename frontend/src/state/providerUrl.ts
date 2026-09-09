export function anthropicClientBaseURL(value: string): string {
  const base = value.trim().replace(/\/+$/, "");
  for (const suffix of ["/v1/messages", "/messages", "/v1"]) {
    if (base.endsWith(suffix)) return base.slice(0, -suffix.length).replace(/\/+$/, "");
  }
  return base;
}
