export function optionalHttpsUrl(value: string | undefined, label: string): string | null {
  const normalized = value?.trim();
  if (!normalized) return null;
  let parsed: URL;
  try {
    parsed = new URL(normalized);
  } catch {
    throw new Error(`${label} must be a valid HTTPS URL`);
  }
  if (parsed.protocol !== "https:") throw new Error(`${label} must use HTTPS`);
  return parsed.toString();
}

export function optionalEmail(value: string | undefined, label: string): string | null {
  const normalized = value?.trim();
  if (!normalized) return null;
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(normalized)) throw new Error(`${label} must be a valid email address`);
  return normalized;
}
