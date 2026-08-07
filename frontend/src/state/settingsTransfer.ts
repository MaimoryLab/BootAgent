import type { ProfileSummary, ProviderEntry } from "../types/api";

type TransferFile = {
  version: 1;
  profiles?: ProfileSummary[];
  providers?: ProviderEntry[];
  encrypted?: { salt: string; iv: string; data: string };
};

const encode = (value: Uint8Array) => btoa(String.fromCharCode(...value));
const decode = (value: string) => Uint8Array.from(atob(value), (char) => char.charCodeAt(0));

async function keyFrom(password: string, salt: Uint8Array) {
  const material = await crypto.subtle.importKey("raw", new TextEncoder().encode(password), "PBKDF2", false, ["deriveKey"]);
  return crypto.subtle.deriveKey({ name: "PBKDF2", salt: salt as BufferSource, iterations: 100000, hash: "SHA-256" }, material, { name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
}

export async function makeTransfer(profiles: ProfileSummary[], providers: ProviderEntry[], encrypt: boolean, password = ""): Promise<TransferFile> {
  const file: TransferFile = { version: 1, profiles, providers: encrypt ? undefined : providers };
  if (encrypt) {
    const salt = crypto.getRandomValues(new Uint8Array(16));
    const iv = crypto.getRandomValues(new Uint8Array(12));
    const data = await crypto.subtle.encrypt({ name: "AES-GCM", iv: iv as BufferSource }, await keyFrom(password, salt), new TextEncoder().encode(JSON.stringify(providers)));
    file.encrypted = { salt: encode(salt), iv: encode(iv), data: encode(new Uint8Array(data)) };
  }
  return file;
}

export async function parseTransfer(text: string, password = ""): Promise<TransferFile> {
  const file = JSON.parse(text) as TransferFile;
  if (file.version !== 1 || (!file.profiles && !file.providers && !file.encrypted)) throw new Error("文件格式无效");
  if (file.encrypted) {
    const { salt, iv, data } = file.encrypted;
    const plain = await crypto.subtle.decrypt({ name: "AES-GCM", iv: decode(iv) as BufferSource }, await keyFrom(password, decode(salt)), decode(data) as BufferSource);
    file.providers = JSON.parse(new TextDecoder().decode(plain)) as ProviderEntry[];
  }
  return file;
}

export const stringifyTransfer = (file: TransferFile) => JSON.stringify(file, null, 2);
