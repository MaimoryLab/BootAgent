const keyPairs = [
  ["oneagent.locale", "bootagent.locale"],
  ["oneagent.theme", "bootagent.theme"],
] as const;

export function migrateStorageKeys(): void {
  try {
    for (const [oldKey, newKey] of keyPairs) {
      const value = localStorage.getItem(oldKey);
      if (value !== null && localStorage.getItem(newKey) === null) localStorage.setItem(newKey, value);
      if (value !== null) localStorage.removeItem(oldKey);
    }
    const oldKeys: string[] = [];
    for (let i = 0; i < localStorage.length; i += 1) {
      const oldKey = localStorage.key(i);
      if (!oldKey?.startsWith("oneagent:launch-directory:")) continue;
      oldKeys.push(oldKey);
    }
    for (const oldKey of oldKeys) {
      const newKey = oldKey.replace("oneagent:", "bootagent:");
      const value = localStorage.getItem(oldKey);
      if (value !== null && localStorage.getItem(newKey) === null) localStorage.setItem(newKey, value);
      localStorage.removeItem(oldKey);
    }
  } catch {
    // Webview storage can be unavailable; the app remains usable in-memory.
  }
}
